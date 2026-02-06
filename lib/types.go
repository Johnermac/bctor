package lib

type ContainerSpec struct {
	ID           string
	Namespaces   NamespaceConfig
	FS           FSConfig
	Capabilities CapsConfig
	Cgroups      CGroupsConfig // nil = disabled
	Seccomp      Profile
	Workload     WorkloadSpec
}

type SupervisorCtx struct {
	ParentNS *NamespaceState
	P2C      [2]int
	C2P      [2]int
	Init2sup [2]int
	ChildPID uintptr
}

func DefaultShellSpec() ContainerSpec {
	spec := ContainerSpec{}
	spec.Namespaces = NamespaceConfig{
		USER:  true, //almost everything needs this enabled
		MOUNT: true,
		//CGROUP: true, //needs root cause /sys/fs/cgroup
		//PID: true,
		//UTS: true,
		//NET: true,
		//IPC: true,
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
