package sup

import (
	"fmt"
	"os"
	"runtime"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/lib/ntw"
	"golang.org/x/sys/unix"
)

var spec lib.ContainerSpec

type ContainerState int

const (
	ContainerCreated ContainerState = iota
	ContainerRunning
	ContainerStopped
	ContainerExited
)

type Container struct {
	Spec        *lib.ContainerSpec
	InitPID     int
	WorkloadPID int
	State       ContainerState
	Namespaces  map[lib.NamespaceType]*lib.NamespaceHandle
	Net         *ntw.NetResources
}

func StartContainer(
	spec *lib.ContainerSpec,
	scx *lib.SupervisorCtx,
	containers map[string]*Container,
	ipc *lib.IPC,
) (*Container, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	//unix.Close(ipc.UserNSPipe[0])
	scx.ParentNS, _ = lib.ReadNamespaces(os.Getpid())

	var err error
	scx.ChildPID, err = lib.NewFork()
	if err != nil {
		return nil, err
	}

	if scx.ChildPID == 0 {
		RunContainerInit(scx, spec, ipc)
	} else {
		fmt.Printf("[!] Supervisor: Fork() Supervisor -> Container-init\n")

		// userns handshake only if USER is created (not joined)
		if spec.Namespaces.USER && !specJoins(spec, lib.NSUser) {
			// Wait until child REALLY entered userns
			lib.WaitFd(ipc.UserNSReady[0])

			if err := lib.SetupUserNSAndContinue(int(scx.ChildPID), ipc); err != nil {
				fmt.Fprintf(os.Stderr, "[?] Supervisor: SetupUserNSAndContinue failed: %v\n", err)
				return nil, err
			}
			fmt.Printf("[>] Supervisor: Successfully wrote maps and signaled child\n")
		} else {
			// For joiner: just signal the child to continue, no maps to set
			lib.FreeFd(ipc.UserNSPipe[1])

			fmt.Printf("[>] Supervisor: Signaled joiner to continue\n")
		}
		unix.Close(ipc.UserNSPipe[1])

		// send shared namespace FDs to joiner
		if len(spec.Shares) > 0 {
			from := spec.Shares[0].FromContainer
			lib.SendNamespaceFDs(
				ipc,
				scx.Handles[from],
				spec.Shares,
			)
		}
	}

	unix.Close(ipc.Sup2Init[1])

	containers[spec.ID] = &Container{
		Spec:    spec,
		InitPID: int(scx.ChildPID),
		State:   ContainerCreated,
	}

	return FinalizeContainer(scx, spec, containers, ipc), nil
}

func FinalizeContainer(
	scx *lib.SupervisorCtx,
	spec *lib.ContainerSpec,
	containers map[string]*Container,
	ipc *lib.IPC,
) *Container {

	workloadPID := lib.RecvWorkloadPID(ipc)
	fmt.Printf("[>] Supervisor: received workload PID=%d from container-init\n", workloadPID)

	var created map[lib.NamespaceType]int
	if createsAnyNamespace(spec) {
		fds, err := lib.RecvCreatedNamespaceFDs(ipc)
		if err == nil {

			lib.RegisterNamespaceHandles(scx, spec.ID, fds)
			created = fds
		}
	}

	var netres *ntw.NetResources

	if spec.IsNetRoot {
		if fd, ok := created[lib.NSNet]; ok && fd != 0 {
			netres = ntw.NetworkConfig(fd, scx, spec, created)
			if netres == nil {
				lib.LogError("Supervisor: Network setup failed for %s.", spec.ID)
			}
		}

		lib.LogInfo("NETWORK CONFIGURED!")
		lib.FreeFd(ipc.NetReady[1])
	}

	fmt.Printf("[>] Supervisor: Container %s created workload PID=%d\n", spec.ID, workloadPID)

	handles := make(map[lib.NamespaceType]*lib.NamespaceHandle)
	for ns, fd := range created {
		handles[ns] = &lib.NamespaceHandle{
			Type: ns,
			FD:   fd,
			Ref:  1,
		}
	}

	c := &Container{
		Spec:        spec,
		InitPID:     int(scx.ChildPID),
		WorkloadPID: workloadPID,
		State:       ContainerRunning,
		Namespaces:  handles,
		Net:         netres,
	}

	unix.Close(ipc.Init2Sup[0])
	unix.Close(ipc.Sup2Init[0])

	lib.LogInfo("Container %s finalized (PID: %d)", spec.ID, workloadPID)
	return c
}

func specJoins(spec *lib.ContainerSpec, ns lib.NamespaceType) bool {
	for _, s := range spec.Shares {
		if s.Type == ns {
			return true
		}
	}
	return false
}

func createsAnyNamespace(spec *lib.ContainerSpec) bool {
	n := spec.Namespaces
	return n.USER || n.NET || n.MOUNT || n.PID || n.IPC || n.UTS || n.CGROUP
}
