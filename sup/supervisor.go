package sup

import (
	"runtime"
	"sync"

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
	IPC         *lib.IPC
}

func StartContainer(
	spec *lib.ContainerSpec,
	logChan chan<- lib.LogMsg,
	scx *lib.SupervisorCtx,
	containers map[string]*Container,
	ipc *lib.IPC,
	wg *sync.WaitGroup,
) (*Container, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var err error
	ChildPID, err := lib.NewFork()
	if err != nil {
		return nil, err
	}

	if ChildPID == 0 {
		RunContainerInit(scx, spec, ipc)
	} else {
		//fmt.Printf("[!] Supervisor: Fork() Supervisor -> Container-init\n")

		// userns handshake only if USER is created (not joined)
		if spec.Namespaces.USER && !specJoins(spec, lib.NSUser) {
			// Wait until child REALLY entered userns
			lib.WaitFd(ipc.UserNSReady[0])

			if err := lib.SetupUserNSAndContinue(int(ChildPID), ipc); err != nil {
				lib.LogError("Supervisor: SetupUserNSAndContinue failed: %v\n", err)
				return nil, err
			}
			//fmt.Printf("[>] Supervisor: Successfully wrote maps and signaled child\n")
		} else {
			// For joiner: just signal the child to continue, no maps to set
			lib.FreeFd(ipc.UserNSPipe[1])

			//	fmt.Printf("[>] Supervisor: Signaled joiner to continue\n")
		}

		// send shared namespace FDs to joiner
		if len(spec.Shares) > 0 {
			from := spec.Shares[0].FromContainer

			scx.Mu.Lock()
			handles := scx.Handles[from]
			scx.Mu.Unlock()

			lib.SendNamespaceFDs(ipc, scx, handles, spec.Shares)
		}
	}

	unix.Close(ipc.Sup2Init[1])

	scx.Mu.Lock()
	containers[spec.ID] = &Container{
		Spec:    spec,
		InitPID: int(ChildPID),
		State:   ContainerCreated,
	}
	scx.Mu.Unlock()

	return FinalizeContainer(scx, logChan, ChildPID, spec, containers, ipc, wg), nil
}

func FinalizeContainer(
	scx *lib.SupervisorCtx,
	logChan chan<- lib.LogMsg,
	ChildPID uintptr,
	spec *lib.ContainerSpec,
	containers map[string]*Container,
	ipc *lib.IPC,
	wg *sync.WaitGroup,
) *Container {

	// LOG SETUP
	//go lib.CaptureLogs(spec.ID, ipc.Log2Sup[0],ipc.Log2Sup[1], spec.Workload.Mode,  logChan, wg)

	workloadPID := lib.RecvWorkloadPID(ipc)
	//fmt.Printf("[>] Supervisor: received workload PID=%d from container-init\n", workloadPID)

	var created map[lib.NamespaceType]int
	if createsAnyNamespace(spec) || len(spec.Shares) > 0 {
		//lib.LogInfo("Supervisor: Waiting for CreatedNamespaceFDs from %s", spec.ID)
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

	//fmt.Printf("[>] Supervisor: Container %s created workload PID=%d\n", spec.ID, workloadPID)

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
		InitPID:     int(ChildPID),
		WorkloadPID: workloadPID,
		State:       ContainerRunning,
		Namespaces:  handles,
		Net:         netres,
	}

	scx.Mu.Lock()
	containers[spec.ID] = c
	scx.Mu.Unlock()

	unix.Close(ipc.Init2Sup[0])
	unix.Close(ipc.Sup2Init[0])

	//lib.LogWarn("Container %s finalized (PID: %d)", spec.ID, workloadPID)
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
