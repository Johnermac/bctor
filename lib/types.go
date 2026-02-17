package lib

import (
	"net"
	"sync"

	"golang.org/x/sys/unix"
)

type ContainerSpec struct {
	ID string

	FS           FSConfig
	Capabilities CapsConfig
	Cgroups      CGroupsConfig // nil = disabled
	Seccomp      Profile
	Workload     WorkloadSpec

	Namespaces NamespaceConfig // what this container wants to CREATE
	Shares     []ShareSpec     // what this container wants to JOIN

	IsNetRoot bool
}

type IPManager interface {
	Allocate() (net.IP, error)
	Release(net.IP)
}

type SupervisorCtx struct {
	ParentNS *NamespaceState
	Handles  map[string]map[NamespaceType]*NamespaceHandle // containerID -> nsType -> handle

	IPAlloc IPManager
	Subnet  *net.IPNet
	Mu      sync.Mutex
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

type ExecutionMode int

const (
	ModeInteractive ExecutionMode = iota
	ModeBatch
)

type WorkloadSpec struct {
	Path string
	Args []string
	Env  []string
	Mode ExecutionMode
}

var WorkloadRegistry = map[Profile]WorkloadSpec{
	ProfileDebugShell: {
		Path: "/bin/sh",
		Args: []string{"sh", "-i"},
		Env: []string{
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"TERM=xterm-256color",
			"HOME=/root",
		},
		Mode: ModeInteractive,
	},
	ProfileWorkload: {
		Path: "/bin/nc",
		Args: []string{"nc", "-lp", "80"},
		Env:  []string{"PATH=/bin:/usr/bin"},
		Mode: ModeBatch,
	},
	ProfileIpLink: {
		Path: "/bin/ip",
		Args: []string{"ip", "addr", "show"},
		Env:  []string{"PATH=/bin:/sbin:/usr/bin"},
		Mode: ModeBatch,
	},
	ProfileLs: {
		Path: "/bin/ls",
		Args: []string{"ls", "/sys/class/net"},
		Env:  []string{"PATH=/bin:/usr/bin"},
		Mode: ModeBatch,
	},
}

func DefaultSpec() *ContainerSpec {
	spec := &ContainerSpec{}
	spec.Namespaces = NamespaceConfig{
		USER:   true, //almost everything needs this enabled
		MOUNT:  true,
		CGROUP: false, //needs root cause /sys/fs/cgroup
		PID:    true,
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

// helper to wait
func WaitFd(fd int) {
	buf := make([]byte, 1)
	unix.Read(fd, buf)
	unix.Close(fd)
}

func FreeFd(fd int) {
	unix.Write(fd, []byte{1})
	unix.Close(fd)
}
