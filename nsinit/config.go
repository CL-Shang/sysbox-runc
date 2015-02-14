package main

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"syscall"

	"github.com/codegangsta/cli"
	"github.com/docker/libcontainer/configs"
)

const defaultMountFlags = syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV

var createFlags = []cli.Flag{
	cli.IntFlag{Name: "parent-death-signal", Usage: "set the signal that will be delivered to the process in case the parent dies"},
	cli.BoolFlag{Name: "read-only", Usage: "set the container's rootfs as read-only"},
	cli.StringSliceFlag{Name: "bind", Value: &cli.StringSlice{}, Usage: "add bind mounts to the container"},
	cli.StringSliceFlag{Name: "tmpfs", Value: &cli.StringSlice{}, Usage: "add tmpfs mounts to the container"},
	cli.IntFlag{Name: "cpushares", Usage: "set the cpushares for the container"},
	cli.IntFlag{Name: "memory-limit", Usage: "set the memory limit for the container"},
	cli.IntFlag{Name: "memory-swap", Usage: "set the memory swap limit for the container"},
	cli.StringFlag{Name: "cpuset-cpus", Usage: "set the cpuset cpus"},
	cli.StringFlag{Name: "cpuset-mems", Usage: "set the cpuset mems"},
	cli.StringFlag{Name: "apparmor-profile", Usage: "set the apparmor profile"},
	cli.StringFlag{Name: "process-label", Usage: "set the process label"},
	cli.StringFlag{Name: "mount-label", Usage: "set the mount label"},
	cli.IntFlag{Name: "userns-root-uid", Usage: "set the user namespace root uid"},
}

var configCommand = cli.Command{
	Name:  "config",
	Usage: "generate a standard configuration file for a container",
	Flags: append([]cli.Flag{
		cli.StringFlag{Name: "file,f", Value: "stdout", Usage: "write the configuration to the specified file"},
	}, createFlags...),
	Action: func(context *cli.Context) {
		template := getTemplate()
		modify(template, context)
		data, err := json.MarshalIndent(template, "", "\t")
		if err != nil {
			fatal(err)
		}
		var f *os.File
		filePath := context.String("file")
		switch filePath {
		case "stdout", "":
			f = os.Stdout
		default:
			if f, err = os.Create(filePath); err != nil {
				fatal(err)
			}
			defer f.Close()
		}
		if _, err := io.Copy(f, bytes.NewBuffer(data)); err != nil {
			fatal(err)
		}
	},
}

func modify(config *configs.Config, context *cli.Context) {
	config.ParentDeathSignal = context.Int("parent-death-signal")
	config.Readonlyfs = context.Bool("read-only")
	config.Cgroups.CpusetCpus = context.String("cpuset-cpus")
	config.Cgroups.CpusetMems = context.String("cpuset-mems")
	config.Cgroups.CpuShares = int64(context.Int("cpushares"))
	config.Cgroups.Memory = int64(context.Int("memory-limit"))
	config.Cgroups.MemorySwap = int64(context.Int("memory-swap"))
	config.AppArmorProfile = context.String("apparmor-profile")
	config.ProcessLabel = context.String("process-label")
	config.MountLabel = context.String("mount-label")

	userns_uid := context.Int("userns-root-uid")
	if userns_uid != 0 {
		config.Namespaces = append(config.Namespaces, configs.Namespace{Type: configs.NEWUSER})
		config.UidMappings = []configs.IDMap{
			{ContainerID: 0, HostID: userns_uid, Size: 1},
			{ContainerID: 1, HostID: 1, Size: userns_uid - 1},
			{ContainerID: userns_uid + 1, HostID: userns_uid + 1, Size: math.MaxInt32 - userns_uid},
		}
		config.GidMappings = []configs.IDMap{
			{ContainerID: 0, HostID: userns_uid, Size: 1},
			{ContainerID: 1, HostID: 1, Size: userns_uid - 1},
			{ContainerID: userns_uid + 1, HostID: userns_uid + 1, Size: math.MaxInt32 - userns_uid},
		}
	}
}

func getTemplate() *configs.Config {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return &configs.Config{
		Rootfs:            cwd,
		ParentDeathSignal: int(syscall.SIGKILL),
		Capabilities: []string{
			"CHOWN",
			"DAC_OVERRIDE",
			"FSETID",
			"FOWNER",
			"MKNOD",
			"NET_RAW",
			"SETGID",
			"SETUID",
			"SETFCAP",
			"SETPCAP",
			"NET_BIND_SERVICE",
			"SYS_CHROOT",
			"KILL",
			"AUDIT_WRITE",
		},
		Namespaces: configs.Namespaces([]configs.Namespace{
			{Type: configs.NEWNS},
			{Type: configs.NEWUTS},
			{Type: configs.NEWIPC},
			{Type: configs.NEWPID},
			{Type: configs.NEWNET},
		}),
		Cgroups: &configs.Cgroup{
			Name:            filepath.Base(cwd),
			Parent:          "nsinit",
			AllowAllDevices: false,
			AllowedDevices:  configs.DefaultAllowedDevices,
		},
		Devices:  configs.DefaultAutoCreatedDevices,
		Hostname: "nsinit",
		MaskPaths: []string{
			"/proc/kcore",
		},
		ReadonlyPaths: []string{
			"/proc/sys", "/proc/sysrq-trigger", "/proc/irq", "/proc/bus",
		},
		Mounts: []*configs.Mount{
			{
				Device:      "tmpfs",
				Source:      "shm",
				Destination: "/dev/shm",
				Data:        "mode=1777,size=65536k",
				Flags:       defaultMountFlags,
			},
			{
				Source:      "mqueue",
				Destination: "/dev/mqueue",
				Device:      "mqueue",
				Flags:       defaultMountFlags,
			},
			{
				Source:      "sysfs",
				Destination: "/sys",
				Device:      "sysfs",
				Flags:       defaultMountFlags | syscall.MS_RDONLY,
			},
		},
		Networks: []*configs.Network{
			{
				Type:    "loopback",
				Address: "127.0.0.1/0",
				Gateway: "localhost",
			},
		},
		Rlimits: []configs.Rlimit{
			{
				Type: syscall.RLIMIT_NOFILE,
				Hard: 1024,
				Soft: 1024,
			},
		},
	}

}
