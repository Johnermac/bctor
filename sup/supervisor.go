package sup

import (
	"encoding/binary"
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
}

func SupervisorLoop(containers map[string]*Container, events <-chan Event) {
	fmt.Printf("[*] len: %d\n", len(containers))
	for len(containers) > 0 {
		ev, ok := <-events
		if !ok {
			fmt.Printf("[*] Events channel closed\n")
			return
		}

		fmt.Printf("[*] event type: %d\n", ev.Type)
		fmt.Printf("[*] InitPID: %d\n", ev.PID)
		switch ev.Type {

		case EventSignal:
			for _, c := range containers {
				fmt.Printf("[*] InitPID to signal: %d\n", c.InitPID)
				if c.InitPID > 0 {
					_ = unix.Kill(c.InitPID, ev.Signal)
				}
			}

		case EventChildExit:
			fmt.Printf("[DBG] supervisor got exit: PID=%d\n", ev.PID)

			c := findContainerByPID(containers, ev.PID)
			if c == nil {
				fmt.Println("[DBG] supervisor: unknown PID")
				continue
			}

			fmt.Printf(
				"[DBG] container match: InitPID=%d WorkloadPID=%d\n",
				c.InitPID, c.WorkloadPID,
			)

			if ev.PID == c.InitPID {
				fmt.Println("[DBG] container fully exited")
				c.State = ContainerExited
				delete(containers, c.Spec.ID)
			} else if ev.PID == c.WorkloadPID {
				fmt.Println("[DBG] container workload exited, killing init")
				c.State = ContainerStopped
				if c.InitPID > 0 {
					_ = unix.Kill(c.InitPID, unix.SIGKILL)
				}
			}
		}
	}
}

func RunSupervisor(ctx lib.SupervisorCtx) lib.SupervisorCtx {

	// fd[0] is the read end, fd[1] is the write end
	err := unix.Pipe2(ctx.P2C[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

	err = unix.Pipe2(ctx.C2P[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

	err = unix.Pipe2(ctx.Init2sup[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

	ctx.ParentNS, _ = lib.ReadNamespaces(os.Getpid())

	return ctx

}

func StartContainer(spec lib.ContainerSpec, ctx lib.SupervisorCtx, containers map[string]*Container) (*Container, error) {
	var err error
	ctx.ChildPID, err = lib.NewFork()
	if err != nil {
		return nil, err
	}

	//containers := map[string]*Container{}
	containers[spec.ID] = &Container{
		Spec:    &spec,
		InitPID: int(ctx.ChildPID),
		State:   ContainerCreated,
	}

	if ctx.ChildPID == 0 {
		RunContainerInit(ctx)
	} else {
		PipeHandshake(ctx)
	}

	buf := make([]byte, 8)
	unix.Read(ctx.Init2sup[0], buf)
	workloadPID := int(binary.LittleEndian.Uint64(buf))

	c := &Container{
		Spec:        &spec,
		InitPID:     int(ctx.ChildPID),
		WorkloadPID: workloadPID,
		State:       ContainerRunning,
	}

	return c, nil
}
