package sup

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func SupervisorLoop(containers map[string]*Container, events <-chan Event) {
	
	fmt.Println("[!] Supervisor running")
	fmt.Printf("[DBG] Supervisor: Len of containers: %d\n", len(containers))

	for {
		ev, ok := <-events
		if !ok {
			fmt.Printf("[*] Supervisor: Events channel closed\n")
			return
		}

		fmt.Printf("[DBG] Supervisor: event type: %d\n", ev.Type)

		switch ev.Type {

		case EventSignal:
			for _, c := range containers {
				if c.InitPID > 0 {
					_ = unix.Kill(c.InitPID, ev.Signal)
				}
			}

		case EventChildExit:
			fmt.Printf("[DBG] Supervisor: Got exit: PID=%d\n", ev.PID)

			c := findContainerByPID(containers, ev.PID)
			if c == nil {
				fmt.Println("[DBG] Supervisor: unknown PID")
				continue
			}

			fmt.Printf(
				"[DBG] Supervisor: container match: InitPID=%d WorkloadPID=%d\n",
				c.InitPID, c.WorkloadPID,
			)

			// workload exit: do nothing (init handles lifecycle)
			if ev.PID == c.WorkloadPID {
				continue
			}

			// init exit: container teardown
			if ev.PID == c.InitPID {
				fmt.Printf("[DBG] Supervisor: Container %s exited\n", c.Spec.ID)

				if c.NetNS != nil {
					c.NetNS.Ref--
					if c.NetNS.Ref == 0 {
						fmt.Printf("[DBG] Supervisor: Cleaning up netns fd %d\n", c.NetNS.FD)
						unix.Close(c.NetNS.FD)
					}
				}

				c.State = ContainerExited
				delete(containers, c.Spec.ID)
			}
		}
	}
}
