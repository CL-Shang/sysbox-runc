// +build linux

package syscont

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	mapset "github.com/deckarep/golang-set"
	"github.com/opencontainers/runc/libsysvisor/sysvisor"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

// UID & GID Mapping Constants
const (
	IdRangeMin uint32 = 65536
	defaultUid uint32 = 231072
	defaultGid uint32 = 231072
)

// sysvisorFsMounts is a list of system container mounts backed by sysvisor-fs
// (please keep in alphabetical order)

var SysvisorFsDir = "/var/lib/sysvisorfs"

var sysvisorFsMounts = []specs.Mount{
	specs.Mount{
		Destination: "/proc/cpuinfo",
		Source:      filepath.Join(SysvisorFsDir, "proc/cpuinfo"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},

	specs.Mount{
		Destination: "/proc/cgroups",
		Source:      filepath.Join(SysvisorFsDir, "proc/cgroups"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/devices",
		Source:      filepath.Join(SysvisorFsDir, "proc/devices"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/diskstats",
		Source:      filepath.Join(SysvisorFsDir, "proc/diskstats"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/loadavg",
		Source:      filepath.Join(SysvisorFsDir, "proc/loadavg"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/meminfo",
		Source:      filepath.Join(SysvisorFsDir, "proc/meminfo"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/pagetypeinfo",
		Source:      filepath.Join(SysvisorFsDir, "proc/pagetypeinfo"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/partitions",
		Source:      filepath.Join(SysvisorFsDir, "proc/partitions"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/stat",
		Source:      filepath.Join(SysvisorFsDir, "proc/stat"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/swaps",
		Source:      filepath.Join(SysvisorFsDir, "proc/swaps"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/sys",
		Source:      filepath.Join(SysvisorFsDir, "proc/sys"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/uptime",
		Source:      filepath.Join(SysvisorFsDir, "proc/uptime"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
}

// sysvisorRwPaths list the paths within the sys container's rootfs
// that must have read-write permission
var sysvisorRwPaths = []string{
	"/proc",
	"/proc/sys",
}

// sysvisorExposedPaths list the paths within the sys container's rootfs
// that must not be masked
var sysvisorExposedPaths = []string{
	"/proc",
	"/proc/sys",
}

// linuxCaps is the full list of Linux capabilities
var linuxCaps = []string{
	"CAP_CHOWN",
	"CAP_DAC_OVERRIDE",
	"CAP_FSETID",
	"CAP_FOWNER",
	"CAP_MKNOD",
	"CAP_NET_RAW",
	"CAP_SETGID",
	"CAP_SETUID",
	"CAP_SETFCAP",
	"CAP_SETPCAP",
	"CAP_NET_BIND_SERVICE",
	"CAP_SYS_CHROOT",
	"CAP_KILL",
	"CAP_AUDIT_WRITE",
	"CAP_DAC_READ_SEARCH",
	"CAP_LINUX_IMMUTABLE",
	"CAP_NET_BROADCAST",
	"CAP_NET_ADMIN",
	"CAP_IPC_LOCK",
	"CAP_IPC_OWNER",
	"CAP_SYS_MODULE",
	"CAP_SYS_RAWIO",
	"CAP_SYS_PTRACE",
	"CAP_SYS_PACCT",
	"CAP_SYS_ADMIN",
	"CAP_SYS_BOOT",
	"CAP_SYS_NICE",
	"CAP_SYS_RESOURCE",
	"CAP_SYS_TIME",
	"CAP_SYS_TTY_CONFIG",
	"CAP_LEASE",
	"CAP_AUDIT_CONTROL",
	"CAP_MAC_OVERRIDE",
	"CAP_MAC_ADMIN",
	"CAP_SYSLOG",
	"CAP_WAKE_ALARM",
	"CAP_BLOCK_SUSPEND",
	"CAP_AUDIT_READ",
}

// cfgNamespaces checks that the namespace config has the minimum set
// of namespaces required and adds any missing namespaces to it
func cfgNamespaces(spec *specs.Spec) error {

	// user-ns and cgroup-ns are not required; but we will add them to the spec.
	var allNs = []string{"pid", "ipc", "uts", "mount", "network", "user", "cgroup"}
	var reqNs = []string{"pid", "ipc", "uts", "mount", "network"}

	allNsSet := mapset.NewSet()
	for _, ns := range allNs {
		allNsSet.Add(ns)
	}

	reqNsSet := mapset.NewSet()
	for _, ns := range reqNs {
		reqNsSet.Add(ns)
	}

	specNsSet := mapset.NewSet()
	for _, ns := range spec.Linux.Namespaces {
		specNsSet.Add(string(ns.Type))
	}

	if !reqNsSet.IsSubset(specNsSet) {
		return fmt.Errorf("container spec missing namespaces %v", reqNsSet.Difference(specNsSet))
	}

	addNsSet := allNsSet.Difference(specNsSet)
	for ns := range addNsSet.Iter() {
		str := fmt.Sprintf("%v", ns)
		newns := specs.LinuxNamespace{
			Type: specs.LinuxNamespaceType(str),
			Path: "",
		}
		spec.Linux.Namespaces = append(spec.Linux.Namespaces, newns)
		logrus.Debugf("added namespace %s to spec", ns)
	}

	return nil
}

// allocIDMappings performs uid and gid allocation for the system container
func allocIDMappings(sysMgr *sysvisor.Mgr, spec *specs.Spec) error {
	var uid, gid uint32
	var err error

	if sysMgr.Enabled() {
		uid, gid, err = sysMgr.ReqSubid(IdRangeMin)
		if err != nil {
			return fmt.Errorf("subid allocation failed: %v", err)
		}
	} else {
		uid = defaultUid
		gid = defaultGid
	}

	uidMap := specs.LinuxIDMapping{
		ContainerID: 0,
		HostID:      uid,
		Size:        IdRangeMin,
	}

	gidMap := specs.LinuxIDMapping{
		ContainerID: 0,
		HostID:      gid,
		Size:        IdRangeMin,
	}

	spec.Linux.UIDMappings = append(spec.Linux.UIDMappings, uidMap)
	spec.Linux.GIDMappings = append(spec.Linux.GIDMappings, gidMap)

	return nil
}

// validateIDMappings checks if the spec's user namespace uid and gid mappings meet sysvisor-runc requirements
func validateIDMappings(spec *specs.Spec) error {

	if len(spec.Linux.UIDMappings) != 1 {
		return fmt.Errorf("sysvisor-runc requires user namespace uid mapping array have one element; found %v", spec.Linux.UIDMappings)
	}

	if len(spec.Linux.GIDMappings) != 1 {
		return fmt.Errorf("sysvisor-runc requires user namespace gid mapping array have one element; found %v", spec.Linux.GIDMappings)
	}

	uidMap := spec.Linux.UIDMappings[0]
	if uidMap.ContainerID != 0 || uidMap.Size < IdRangeMin {
		return fmt.Errorf("sysvisor-runc requires uid mapping specify a container with at least %d uids starting at uid 0; found %v", IdRangeMin, uidMap)
	}

	gidMap := spec.Linux.GIDMappings[0]
	if gidMap.ContainerID != 0 || gidMap.Size < IdRangeMin {
		return fmt.Errorf("sysvisor-runc requires gid mapping specify a container with at least %d gids starting at gid 0; found %v", IdRangeMin, gidMap)
	}

	return nil
}

// cfgIDMappings checks if the uid/gid mappings are present and valid; if they are not
// present, it allocates them. Note that we don't disallow mappings that map to the host
// root UID (i.e., we always honor the ID config); some runc tests use such mappings.
func cfgIDMappings(sysMgr *sysvisor.Mgr, spec *specs.Spec) error {
	if len(spec.Linux.UIDMappings) == 0 && len(spec.Linux.GIDMappings) == 0 {
		return allocIDMappings(sysMgr, spec)
	}
	return validateIDMappings(spec)
}

// cfgCapabilities sets the capabilities for the process in the system container
func cfgCapabilities(p *specs.Process) {
	caps := p.Capabilities
	uid := p.User.UID

	// In a sys container, the root process has all capabilities
	if uid == 0 {
		caps.Bounding = linuxCaps
		caps.Effective = linuxCaps
		caps.Inheritable = linuxCaps
		caps.Permitted = linuxCaps
		caps.Ambient = linuxCaps
		logrus.Debugf("enabled all capabilities in the process spec")
	}
}

// cfgMaskedPaths removes from the container's config any masked paths for which
// sysvisor-fs will handle accesses.
func cfgMaskedPaths(spec *specs.Spec) {
	specPaths := spec.Linux.MaskedPaths
	for i := 0; i < len(specPaths); i++ {
		for _, path := range sysvisorExposedPaths {
			if specPaths[i] == path {
				specPaths = append(specPaths[:i], specPaths[i+1:]...)
				i--
				logrus.Debugf("removed masked path %s from spec", path)
				break
			}
		}
	}
	spec.Linux.MaskedPaths = specPaths
}

// cfgReadonlyPaths removes from the container's config any read-only paths
// that must be read-write in the system container
func cfgReadonlyPaths(spec *specs.Spec) {
	specPaths := spec.Linux.ReadonlyPaths
	for i := 0; i < len(specPaths); i++ {
		for _, path := range sysvisorRwPaths {
			if specPaths[i] == path {
				specPaths = append(specPaths[:i], specPaths[i+1:]...)
				i--
				logrus.Debugf("removed read-only path %s from spec", path)
				break
			}
		}
	}
	spec.Linux.ReadonlyPaths = specPaths
}

// cfgSysvisorFsMounts adds the sysvisor-fs mounts to the containers config.
func cfgSysvisorFsMounts(spec *specs.Spec) {

	// disallow all mounts over /proc/* or /sys/* (except for /sys/fs/cgroup);
	// only sysvisor-fs mounts are allowed there.
	for i := 0; i < len(spec.Mounts); i++ {
		m := spec.Mounts[i]
		if strings.HasPrefix(m.Destination, "/proc/") ||
			(strings.HasPrefix(m.Destination, "/sys/") && (m.Destination != "/sys/fs/cgroup")) {
			spec.Mounts = append(spec.Mounts[:i], spec.Mounts[i+1:]...)
			i--
			logrus.Debugf("removed mount %s from spec (not compatible with sysvisor-runc)", m.Destination)
		}
	}

	// add sysvisor-fs mounts to the config
	for _, mount := range sysvisorFsMounts {
		spec.Mounts = append(spec.Mounts, mount)
		logrus.Debugf("added sysvisor-fs mount %s to spec", mount.Destination)
	}
}

// cfgCgroups configures the system container's cgroup settings.
func cfgCgroups(spec *specs.Spec) error {

	// remove the read-only attribute from the cgroup mount; this is fine because the sys
	// container's cgroup root will be a child of the cgroup that controls the
	// sys container's resources; thus, root processes inside the sys container will be
	// able to allocate cgroup resources yet not modify the resources allocated to the sys
	// container itself.
	for i, mount := range spec.Mounts {
		if mount.Type == "cgroup" {
			for j := 0; j < len(mount.Options); j++ {
				if mount.Options[j] == "ro" {
					mount.Options = append(mount.Options[:j], mount.Options[j+1:]...)
					j--
					logrus.Debugf("removed read-only attr for cgroup mount %s", mount.Destination)
				}
			}
			spec.Mounts[i].Options = mount.Options
		}
	}

	return nil
}

// cfgSeccomp configures the system container's seccomp settings.
func cfgSeccomp(seccomp *specs.LinuxSeccomp) error {
	if seccomp == nil {
		return nil
	}

	supportedArch := false
	for _, arch := range seccomp.Architectures {
		if arch == specs.ArchX86_64 {
			supportedArch = true
		}
	}
	if !supportedArch {
		return nil
	}

	// we don't yet support specs with default trap & trace actions
	if seccomp.DefaultAction != specs.ActAllow &&
		seccomp.DefaultAction != specs.ActErrno &&
		seccomp.DefaultAction != specs.ActKill {
		return fmt.Errorf("spec seccomp default actions other than allow, errno, and kill are not supported")
	}

	// categorize syscalls per seccomp actions
	allowSet := mapset.NewSet()
	errnoSet := mapset.NewSet()
	killSet := mapset.NewSet()

	for _, syscall := range seccomp.Syscalls {
		for _, name := range syscall.Names {
			switch syscall.Action {
			case specs.ActAllow:
				allowSet.Add(name)
			case specs.ActErrno:
				errnoSet.Add(name)
			case specs.ActKill:
				killSet.Add(name)
			}
		}
	}

	// convert sys container syscall whitelist to a set
	syscontAllowSet := mapset.NewSet()
	for _, sc := range syscontSyscallWhitelist {
		syscontAllowSet.Add(sc)
	}

	// seccomp syscall lsit may be a whitelist or blacklist
	whitelist := (seccomp.DefaultAction == specs.ActErrno ||
		seccomp.DefaultAction == specs.ActKill)

	// diffset is the set of syscalls that needs adding (for whitelist) or removing (for blacklist)
	diffSet := mapset.NewSet()
	if whitelist {
		diffSet = syscontAllowSet.Difference(allowSet)
	} else {
		disallowSet := errnoSet.Union(killSet)
		diffSet = disallowSet.Difference(syscontAllowSet)
	}

	if whitelist {
		// add the diffset to the whitelist
		for syscallName := range diffSet.Iter() {
			str := fmt.Sprintf("%v", syscallName)
			sc := specs.LinuxSyscall{
				Names:  []string{str},
				Action: specs.ActAllow,
			}
			seccomp.Syscalls = append(seccomp.Syscalls, sc)
		}

		logrus.Debugf("added syscalls to seccomp profile: %v", diffSet)

	} else {
		// remove the diffset from the blacklist
		var newSyscalls []specs.LinuxSyscall
		for _, sc := range seccomp.Syscalls {
			for i, scName := range sc.Names {
				if diffSet.Contains(scName) {
					// Remove this syscall
					sc.Names = append(sc.Names[:i], sc.Names[i+1:]...)
				}
			}
			if sc.Names != nil {
				newSyscalls = append(newSyscalls, sc)
			}
		}
		seccomp.Syscalls = newSyscalls

		logrus.Debugf("removed syscalls from seccomp profile: %v", diffSet)
	}

	return nil
}

// cfgAppArmor sets up the apparmor config for sys containers
func cfgAppArmor(p *specs.Process) error {

	// The default docker profile is too restrictive for sys containers (e.g., preveting
	// mounts, write access to /proc/sys/*, etc). For now, we simply ignore any apparmor
	// profile in the container's config.
	//
	// TODO: In the near future, we should develop an apparmor profile for sys-containers,
	// and have sysvisor-mgr load it to the kernel (if apparmor is enabled on the system)
	// and then configure the container to use that profile here.

	p.ApparmorProfile = ""
	return nil
}

// cfgLibModMount sets up a read-only bind mount of the host's "/lib/modules/<kernel-release>"
// directory in the same path inside the system container; this allows system container
// processes to verify the presence of modules via modprobe. System apps such as Docker and
// K8s do this. Note that this does not imply module loading/unloading is supported in a
// system container (it's not). It merely lets processes check if a module is loaded.
func cfgLibModMount(spec *specs.Spec, doFhsCheck bool) error {

	if doFhsCheck {
		// only do the mount if the container's rootfs has a "/lib" dir
		rootfsLibPath := filepath.Join(spec.Root.Path, "lib")
		if _, err := os.Stat(rootfsLibPath); os.IsNotExist(err) {
			return nil
		}
	}

	kernelRel, err := sysvisor.GetKernelRelease()
	if err != nil {
		return err
	}

	path := filepath.Join("/lib/modules/", kernelRel)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		logrus.Warnf("could not setup bind mount for %s: %v", path, err)
		return nil
	}

	mount := specs.Mount{
		Destination: path,
		Source:      path,
		Type:        "bind",
		Options:     []string{"ro", "rbind", "rprivate"}, // must be read-only
	}

	// check if the container spec has a match or a conflict for the mount
	for _, m := range spec.Mounts {
		if (m.Source == mount.Source) &&
			(m.Destination == mount.Destination) &&
			(m.Type == mount.Type) &&
			stringSliceEqual(m.Options, mount.Options) {
			return nil
		}

		if m.Destination == mount.Destination {
			logrus.Debugf("honoring container spec override for mount of %s", m.Destination)
			return nil
		}
	}

	// perform the mount; note that the mount will appear inside the system
	// container as owned by nobody:nogroup; this is fine since the files
	// are not meant to be modified from within the system container.
	spec.Mounts = append(spec.Mounts, mount)
	logrus.Debugf("added bind mount for %s to container's spec", path)
	return nil
}

// checkSpec performs some basic checks on the system container's spec
func checkSpec(spec *specs.Spec) error {

	if spec.Root == nil || spec.Linux == nil {
		return fmt.Errorf("not a linux container spec")
	}

	if spec.Root.Readonly {
		return fmt.Errorf("root path must be read-write but it's set to read-only")
	}

	return nil
}

// needUidShiftOnRootfs checks if uid/gid shifting on the container's rootfs is required to
// run the system container.
func needUidShiftOnRootfs(spec *specs.Spec) (bool, error) {
	var hostUidMap, hostGidMap uint32

	// the uid map is assumed to be present
	for _, mapping := range spec.Linux.UIDMappings {
		if mapping.ContainerID == 0 {
			hostUidMap = mapping.HostID
		}
	}

	// the gid map is assumed to be present
	for _, mapping := range spec.Linux.GIDMappings {
		if mapping.ContainerID == 0 {
			hostGidMap = mapping.HostID
		}
	}

	// find the rootfs owner
	rootfs := spec.Root.Path

	fi, err := os.Stat(rootfs)
	if err != nil {
		return false, err
	}

	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("failed to convert to syscall.Stat_t")
	}

	rootfsUid := st.Uid
	rootfsGid := st.Gid

	// use shifting when the rootfs is owned by true root, the containers uid/gid root
	// mapping don't match the container's rootfs owner, and the host ID for the uid and
	// gid mappings is the same.
	if rootfsUid == 0 && rootfsGid == 0 &&
		hostUidMap != rootfsUid && hostGidMap != rootfsGid &&
		hostUidMap == hostGidMap {
		return true, nil
	}

	return false, nil
}

// getSupConfig obtains supplementary config from the sysvisor-mgr for the container with the given id
func getSupConfig(mgr *sysvisor.Mgr, spec *specs.Spec, shiftUids bool) error {
	uid := spec.Linux.UIDMappings[0].HostID
	gid := spec.Linux.GIDMappings[0].HostID

	mounts, err := mgr.ReqSupMounts(spec.Root.Path, uid, gid, shiftUids)
	if err != nil {
		return fmt.Errorf("failed to request supplementary mounts from sysvisor-mgr: %v", err)
	}
	spec.Mounts = append(spec.Mounts, mounts...)
	return nil
}

// Configure the container's process spec for system containers
func ConvertProcessSpec(p *specs.Process) error {
	cfgCapabilities(p)

	if err := cfgAppArmor(p); err != nil {
		return fmt.Errorf("failed to configure AppArmor profile: %v", err)
	}

	return nil
}

// ConvertSpec converts the given container spec to a system container spec.
func ConvertSpec(context *cli.Context, sysMgr *sysvisor.Mgr, sysFs *sysvisor.Fs, spec *specs.Spec) (bool, error) {

	if err := checkSpec(spec); err != nil {
		return false, fmt.Errorf("invalid or unsupported system container spec: %v", err)
	}

	if err := ConvertProcessSpec(spec.Process); err != nil {
		return false, fmt.Errorf("failed to configure process spec: %v", err)
	}

	if err := cfgNamespaces(spec); err != nil {
		return false, fmt.Errorf("invalid namespace config: %v", err)
	}

	if err := cfgIDMappings(sysMgr, spec); err != nil {
		return false, fmt.Errorf("invalid user/group ID config: %v", err)
	}

	if err := cfgCgroups(spec); err != nil {
		return false, fmt.Errorf("failed to configure cgroup mounts: %v", err)
	}

	if err := cfgLibModMount(spec, true); err != nil {
		return false, fmt.Errorf("failed to setup /lib/module/<kernel-version> mount: %v", err)
	}

	if sysFs.Enabled() {
		cfgMaskedPaths(spec)
		cfgReadonlyPaths(spec)
		cfgSysvisorFsMounts(spec)
	}

	if err := cfgSeccomp(spec.Linux.Seccomp); err != nil {
		return false, fmt.Errorf("failed to configure seccomp: %v", err)
	}

	// Must be done after cfgIDMappings()
	shiftUids, err := needUidShiftOnRootfs(spec)
	if err != nil {
		return false, fmt.Errorf("error while checking for uid-shifting need: %v", err)
	}

	// Must be done after needUidShiftOnRootfs()
	if sysMgr.Enabled() {
		if err := getSupConfig(sysMgr, spec, shiftUids); err != nil {
			return false, fmt.Errorf("failed to get supplementary config: %v", err)
		}
	}

	// TODO: ensure /proc and /sys are mounted (if not present in the container spec)

	// TODO: ensure /dev is mounted

	return shiftUids, nil
}