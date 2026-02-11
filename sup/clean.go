package sup

import (
	"fmt"

	"github.com/Johnermac/bctor/lib"
	"golang.org/x/sys/unix"
)

func OnContainerExit(
	containers map[string]*Container,
	scx *lib.SupervisorCtx,
	events <-chan Event,
) {
	fmt.Println("[!] Supervisor running")

	for {
		ev, ok := <-events
		if !ok {
			fmt.Println("[*] Supervisor: Events channel closed")
			return
		}

		switch ev.Type {

		case EventSignal:
			for _, c := range containers {
				if c.InitPID > 0 {
					_ = unix.Kill(c.InitPID, ev.Signal)
				}
			}

		case EventChildExit:
			c := findContainerByPID(containers, ev.PID)
			if c == nil {
				continue
			}

			// workload exit → ignore (init owns lifecycle)
			if ev.PID == c.WorkloadPID {
				continue
			}

			// init exit → teardown
			if ev.PID == c.InitPID {
				c.State = ContainerExited

				// cleanup ONLY namespaces owned by this container
				if owned, ok := scx.Handles[c.Spec.ID]; ok {
					for ns, h := range owned {
						h.Ref--
						if h.Ref == 0 {
							unix.Close(h.FD)
							delete(owned, ns)
						}
					}
					delete(scx.Handles, c.Spec.ID)
				}

				delete(containers, c.Spec.ID)
			}
		}
	}
}

