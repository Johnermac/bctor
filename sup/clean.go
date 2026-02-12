package sup

import (
	"fmt"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/lib/ntw"
	"golang.org/x/sys/unix"
)

func OnContainerExit(
	containers map[string]*Container,
	scx *lib.SupervisorCtx,
	events <-chan Event,
	iface string,
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

				if c.Net != nil {
					ntw.CleanupContainerNetworking(scx, c.Net)
				}

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

				if len(containers) == 0 {
					ntw.RemoveNATRule("10.0.0.0/24", iface)
					ntw.DeleteBridge("bctor0")		
					lib.LogSuccess("All good!")			
				}
			}			
		}
	}
}

