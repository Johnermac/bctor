package lib

import (
	"os"

	"golang.org/x/sys/unix"
)

type ContainerSpec struct {
	ID           string
	Namespaces   NamespaceConfig
	FS           FSConfig
	Capabilities CapsConfig
	Cgroups      CGroupsConfig // nil = disabled
	Seccomp      Profile
	Workload     WorkloadSpec
	ShareNetNS 	*NetNamespace
}

type SupervisorCtx struct {
	ParentNS *NamespaceState
	NetNS 	 *NetNamespace
	P2C      [2]int
	C2P      [2]int
	Init2sup [2]int
	ChildPID uintptr	
	NetNSFD int
}

/*
type SupervisorCtx struct {
	ParentNS *NamespaceState
	NetNS    *NetNamespace

	ChildPID uintptr   // init pid
	WorkPID  uintptr   // workload pid (important)
	NetNSFD  int       // fd to netns
}
*/

type WorkloadSpec struct {
	Path string // absolute inside container (/bin/sh, /bin/nc, etc)
	Args []string
	Env  []string
}

var WorkloadRegistry = map[Profile]WorkloadSpec{
	ProfileDebugShell: {
		Path: "/bin/sh",
		Args: []string{"sh"},
		Env:  []string{"PATH=/bin"},
	},
	ProfileWorkload: {
		Path: "/bin/nc",
		Args: []string{"nc", "-lp", "80"},
		Env:  os.Environ(),
	},
	ProfileIpLink: {
		Path: "/bin/ip",
		Args: []string{"ip", "link", "show"},
		Env:  os.Environ(),
	},
}

func DefaultShellSpec() *ContainerSpec {
	spec := &ContainerSpec{}
	spec.Namespaces = NamespaceConfig{
		USER:  true, //almost everything needs this enabled
		MOUNT: true,
		CGROUP: false, //needs root cause /sys/fs/cgroup
		PID: false,
		UTS: false,
		NET: false, // set to true for container 1, container 2 will join this netns 
		IPC: false,
	}

	spec.FS = FSConfig{
		Rootfs:   "/dev/shm/bctor-root/",
		ReadOnly: false, // no permission, debug later
		Proc:     true,
		Sys:      true,
		Dev:      true,
		UseTmpfs: true,
	}

	spec.Cgroups = CGroupsConfig{
		Path:      "/sys/fs/cgroup/bctor",
		CPUMax:    "50000 100000", // 50% CPU
		MemoryMax: "12M",
		PIDsMax:   "5",
	}

	spec.Capabilities = CapsConfig{
		AllowCaps: []Capability{CAP_NET_BIND_SERVICE},
	}

	return spec
}

func WriteSync(fd int) {
	var b [1]byte
	unix.Write(fd, b[:])
}

func ReadSync(fd int) {
	var b [1]byte
	unix.Read(fd, b[:])
}