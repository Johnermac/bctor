package sup

import (
	"encoding/binary"
	"fmt"
	"os"
	"strconv"

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
}

func StartContainer(spec *lib.ContainerSpec, ctx lib.SupervisorCtx, containers map[string]*Container) (*Container, error) {
	
	ctx.ParentNS, _ = lib.ReadNamespaces(os.Getpid())

	var err error
	ctx.ChildPID, err = lib.NewFork()
	if err != nil {
		return nil, err
	}		
	fmt.Printf("[!] Supervisor: First fork done\n")

	if ctx.ChildPID == 0 {
		RunContainerInit(ctx, spec)
		//return nil, nil
	} else {
		PipeHandshake(ctx)
	}		

	fmt.Printf("[>] Supervisor: Sending NetNS FD to container-init\n")
	if spec.ShareNetNS != nil { lib.SendNetNSFD(ctx, spec.ShareNetNS.FD) }


	containers[spec.ID] = &Container{
		Spec:    spec,
		InitPID: int(ctx.ChildPID),
		State:   ContainerCreated,
	}	
	
	fmt.Printf("[>] Supervisor: After handshake, waiting for workload PID from container-init\n")
	
	c := FinalizeContainer(ctx, spec, containers, containers[spec.ID].InitPID)	
	//fmt.Printf("[>] Supervisor: container added to map: %s\n", c.Spec.ID)

	return c, nil			
}

func PipeHandshake(ctx lib.SupervisorCtx) {
	unix.Close(ctx.P2C[0]) // parent writes only
	unix.Close(ctx.C2P[1]) // parent reads only
	

	// 1. wait for container-init to signal "alive"
	buf := make([]byte, 1)
	if _, err := unix.Read(ctx.C2P[0], buf); err != nil {
		panic("[?] Supervisor: Handshake: failed to read child ready signal")
	}

	fmt.Println("[2] Supervisor: container-init is alive, setting up user namespace")

	// 2. setup user namespace for container-init
	pidStr := strconv.Itoa(int(ctx.ChildPID))
	if err := lib.SetupUserNamespace(pidStr); err != nil {
		fmt.Println("[?] Supervisor: Error SetupUserNamespace:", err)
		unix.Exit(1)
	}

	fmt.Println("[3] Supervisor: parent set up user namespace and allowed continuation")
	
	// 3. allow child to continue
	if _, err := unix.Write(ctx.P2C[1], []byte{1}); err != nil {
		panic("[?] Supervisor: Handshake failed to signal continue")
	}

	// 4. close write end → child observes EOF (sync point)
	unix.Close(ctx.P2C[1])

	// optional: drain + close
	unix.Read(ctx.C2P[0], buf)
	unix.Close(ctx.C2P[0])
}

func GetPipeContent(ctx lib.SupervisorCtx) int {
	buf := make([]byte, 8)
	unix.Read(ctx.Init2sup[0], buf)
	PID := int(binary.LittleEndian.Uint64(buf))
	fmt.Printf("[*] Supervisor: received PID=%d\n", PID)
	return PID
}

func FinalizeContainer(
	ctx lib.SupervisorCtx,
	spec *lib.ContainerSpec,
	containers map[string]*Container,
	initPID int,
) *Container {

	var c *Container

	if spec.Namespaces.NET {
		// creator container
		workloadPID, netnsFD := lib.RecvWorkloadPIDAndNetNS(ctx)
		fmt.Printf("[>] Supervisor: received workload PID=%d and netns FD=%d from container-init\n",
			workloadPID, netnsFD,
		)		

		ctx.NetNS = &lib.NetNamespace{
			FD:  netnsFD,
			Ref: 1,
		}

		c = &Container{
			Spec:        spec,
			InitPID:     int(ctx.ChildPID),
			WorkloadPID: workloadPID,
			State:       ContainerRunning,
			NetNS:       ctx.NetNS,
		}
	} else {
		// joining container
		workloadPID := lib.RecvWorkloadPID(ctx)
		fmt.Printf("[>] Supervisor: received workload PID=%d from container-init\n", workloadPID)

		c = &Container{
			Spec:        spec,
			InitPID:     int(ctx.ChildPID),
			WorkloadPID: workloadPID,
			State:       ContainerRunning,
			NetNS:       spec.ShareNetNS,
		}
	}

	containers[spec.ID] = c
	return c
}
