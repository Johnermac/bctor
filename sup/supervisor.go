package sup

import (
	"fmt"
	"os"

	"github.com/Johnermac/bctor/lib"
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
	NetNS       *lib.NetNamespace
	UserNS      *lib.UserNamespace
}

func StartContainer(
	spec *lib.ContainerSpec, 
	scx lib.SupervisorCtx, 
	containers map[string]*Container,
	ipc *lib.IPC,
	) (*Container, error) {
	
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
		fmt.Printf("[!] Supervisor: First fork done\n")		
		
		// Only setup uid/gid maps for creator containers
		// Joiner containers join the creator's user NS which already has maps
		if spec.ShareUserNS == nil {
			if err := lib.SetupUserNSAndContinue(int(scx.ChildPID), ipc.UserNSPipe[1]); err != nil {
				fmt.Fprintf(os.Stderr, "[?] Supervisor: SetupUserNSAndContinue failed: %v\n", err)
				return nil, err
			}
		} else {
			// For joiner: just signal the child to continue, no maps to set
			unix.Write(ipc.UserNSPipe[1], []byte{1})
		}
		unix.Close(ipc.UserNSPipe[1])
		unix.Close(ipc.UserNSPipe[0])
		
		// Send namespaces only for joiner containers
		if spec.ShareNetNS != nil {
			fmt.Printf(
				"[>] Supervisor: Sending UserNS FD=%d and NetNS FD=%d to container-init\n",
				spec.ShareUserNS.FD,
				spec.ShareNetNS.FD,
			)

			lib.SendUserNetNSFD(
				ipc,
				spec.ShareUserNS.FD,
				spec.ShareNetNS.FD,
			)
		}
	}		

	unix.Close(ipc.Sup2Init[1])
	

	containers[spec.ID] = &Container{
		Spec:    spec,
		InitPID: int(scx.ChildPID),
		State:   ContainerCreated,
	}	
	
	fmt.Printf("[>] Supervisor: After handshake, waiting for workload PID from container-init\n")
	
	c := FinalizeContainer(scx, spec, containers, containers[spec.ID].InitPID, ipc)	
	return c, nil			
}


func FinalizeContainer(
	scx lib.SupervisorCtx,
	spec *lib.ContainerSpec,
	containers map[string]*Container,
	initPID int,
	ipc *lib.IPC,
) *Container {

	var c *Container

	if spec.Namespaces.NET {
		// creator container
		workloadPID, usernsFD, netnsFD := lib.RecvWorkPIDUserNetNS(ipc)		
		fmt.Printf("[>] Supervisor: received workload PID=%d netns FD=%d user FD=%d from container-init\n", workloadPID, netnsFD, usernsFD)

		scx.UserNS = &lib.UserNamespace{
			FD:  usernsFD,
			Ref: 1,
		}
		scx.NetNS = &lib.NetNamespace{
			FD:  netnsFD,
			Ref: 1,
		}

		c = &Container{
			Spec:        spec,
			InitPID:     int(scx.ChildPID),
			WorkloadPID: workloadPID,
			State:       ContainerRunning,
			NetNS:       scx.NetNS,		
			UserNS:      scx.UserNS,
		}
		
	} else {
		// joining container
		workloadPID := lib.RecvWorkloadPID(ipc)
		fmt.Printf("[>] Supervisor: received workload PID=%d from container-init\n", workloadPID)

		c = &Container{
			Spec:        spec,
			InitPID:     int(scx.ChildPID),
			WorkloadPID: workloadPID,
			State:       ContainerRunning,
			NetNS:       spec.ShareNetNS,
			UserNS:      spec.ShareUserNS,
		}		
	}		
	unix.Close(ipc.Init2Sup[0])
	unix.Close(ipc.Sup2Init[0])

	containers[spec.ID] = c
	return c
}
