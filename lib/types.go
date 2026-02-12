package lib

import (
	"net"
	"os"

	"golang.org/x/sys/unix"
)

type ContainerSpec struct {
	ID           string	
	
	FS           FSConfig
	Capabilities CapsConfig
	Cgroups      CGroupsConfig // nil = disabled
	Seccomp      Profile
	Workload     WorkloadSpec	

	Namespaces NamespaceConfig // what this container wants to CREATE
	Shares     []ShareSpec     // what this container wants to JOIN
	
}

type IPManager interface {
	Allocate() (net.IP, error)
	Release(net.IP)
}

type SupervisorCtx struct {
	ParentNS 	*NamespaceState	
	Handles 	map[string]map[NamespaceType]*NamespaceHandle // containerID -> nsType -> handle
	IPAlloc   IPManager
	ChildPID 	uintptr       // init pid
	WorkPID  	uintptr       // workload pid	
	Subnet  	*net.IPNet
}



// namespaces

type NamespaceType int

const (
	NSUser NamespaceType = iota
	NSNet
	NSMnt
	NSPID
	NSIPC
	NSUTS
	NSCgroup
)

type ShareSpec struct {
	Type          NamespaceType
	FromContainer string // container ID
}

type NamespaceHandle struct {
	Type NamespaceType
	FD   int // O_PATH fd received or captured
	Ref  int // supervisor-owned refcount
}


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
		Args: []string{"ip", "addr", "show"},
		Env:  os.Environ(),
	},
}

func DefaultShellSpec() *ContainerSpec {
	spec := &ContainerSpec{}
	spec.Namespaces = NamespaceConfig{
		USER:   true, //almost everything needs this enabled
		MOUNT:  true,
		CGROUP: false, //needs root cause /sys/fs/cgroup
		PID:    false,
		UTS:    false,
		NET:    true, // set to true for container 1, container 2 will join this netns
		IPC:    false,
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
		AllowCaps: []Capability{CAP_SYS_ADMIN},
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
