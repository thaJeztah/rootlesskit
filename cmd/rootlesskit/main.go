package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"

	"github.com/rootless-containers/rootlesskit/pkg/child"
	"github.com/rootless-containers/rootlesskit/pkg/common"
	"github.com/rootless-containers/rootlesskit/pkg/network/slirp4netns"
	"github.com/rootless-containers/rootlesskit/pkg/network/vdeplugslirp"
	"github.com/rootless-containers/rootlesskit/pkg/network/vpnkit"
	"github.com/rootless-containers/rootlesskit/pkg/parent"
)

func main() {
	pipeFDEnvKey := "_ROOTLESSKIT_PIPEFD_UNDOCUMENTED"
	iAmChild := os.Getenv(pipeFDEnvKey) != ""
	debug := false
	app := cli.NewApp()
	app.Name = "rootlesskit"
	app.Usage = "the gate to the rootless world"
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:        "debug",
			Usage:       "debug mode",
			Destination: &debug,
		},
		cli.StringFlag{
			Name:  "state-dir",
			Usage: "state directory",
		},
		cli.StringFlag{
			Name:  "net",
			Usage: "host, slirp4netns, vpnkit, vdeplug_slirp",
			Value: "host",
		},
		cli.StringFlag{
			Name:  "slirp4netns-binary",
			Usage: "path of slirp4netns binary for --net=slirp4netns",
			Value: "slirp4netns",
		},
		cli.StringFlag{
			Name:  "vpnkit-binary",
			Usage: "path of VPNKit binary for --net=vpnkit",
			Value: "vpnkit",
		},
		cli.IntFlag{
			Name:  "mtu",
			Usage: "MTU for non-host network (default: 65520 for slirp4netns, 1500 for others)",
			Value: 0, // resolved into 65520 for slirp4netns, 1500 for others
		},
		cli.StringSliceFlag{
			Name:  "copy-up",
			Usage: "mount a filesystem and copy-up the contents. e.g. \"--copy-up=/etc\" (typically required for non-host network)",
		},
		cli.StringFlag{
			Name:  "copy-up-mode",
			Usage: "tmpfs+symlink",
			Value: "tmpfs+symlink",
		},
	}
	app.Before = func(context *cli.Context) error {
		if debug {
			logrus.SetLevel(logrus.DebugLevel)
		}
		return nil
	}
	app.Action = func(clicontext *cli.Context) error {
		if clicontext.NArg() < 1 {
			return errors.New("no command specified")
		}
		if iAmChild {
			return child.Child(pipeFDEnvKey, clicontext.Args())
		}
		parentOpt, err := createParentOpt(clicontext)
		if err != nil {
			return err
		}
		return parent.Parent(pipeFDEnvKey, parentOpt)
	}
	if err := app.Run(os.Args); err != nil {
		id := "parent"
		if iAmChild {
			id = "child " // padded to len("parent")
		}
		if debug {
			fmt.Fprintf(os.Stderr, "[rootlesskit:%s] error: %+v\n", id, err)
		} else {
			fmt.Fprintf(os.Stderr, "[rootlesskit:%s] error: %v\n", id, err)
		}
		// propagate the exit code
		code, ok := common.GetExecExitStatus(err)
		if !ok {
			code = 1
		}
		os.Exit(code)
	}
}

func parseCopyUpMode(s string) (common.CopyUpMode, error) {
	switch s {
	case "tmpfs+symlink":
		return common.TmpfsWithSymlinkCopyUp, nil
	default:
		return -1, errors.Errorf("unknown tmpfs copy-up mode: %s", s)
	}
}

func createParentOpt(clicontext *cli.Context) (*parent.Opt, error) {
	opt := &parent.Opt{}
	var err error
	opt.StateDir = clicontext.String("state-dir")
	mtu := clicontext.Int("mtu")
	if mtu < 0 || mtu > 65521 {
		// 0 is ok (stands for the driver's default)
		return nil, errors.Errorf("mtu must be <= 65521, got %d", mtu)
	}
	switch s := clicontext.String("net"); s {
	case "host":
		// NOP
		if mtu != 0 {
			logrus.Warnf("unsupported mtu for --net=host: %d", mtu)
		}
	case "slirp4netns":
		binary := clicontext.String("slirp4netns-binary")
		if _, err := exec.LookPath(binary); err != nil {
			return nil, err
		}
		opt.NetworkDriver = slirp4netns.NewParentDriver(binary, mtu)
	case "vpnkit":
		binary := clicontext.String("vpnkit-binary")
		if _, err := exec.LookPath(binary); err != nil {
			return nil, err
		}
		opt.NetworkDriver = vpnkit.NewParentDriver(binary, mtu)
	case "vdeplug_slirp":
		opt.NetworkDriver = vdeplugslirp.NewParentDriver(mtu)
	default:
		return nil, errors.Errorf("unknown network mode: %s", s)
	}
	opt.CopyUpMode, err = parseCopyUpMode(clicontext.String("copy-up-mode"))
	if err != nil {
		return nil, err
	}
	opt.CopyUpDirs = clicontext.StringSlice("copy-up")
	return opt, nil
}
