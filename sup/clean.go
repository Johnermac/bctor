package sup

import (
	"fmt"
	"sync"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/lib/ntw"
	"golang.org/x/sys/unix"
)

func OnContainerExit(
	containers map[string]*Container,
	scx *lib.SupervisorCtx,
	events <-chan Event,
	iface string,
	wg *sync.WaitGroup,
	containerCount int,
) {
	defer wg.Done()
	processedExits := 0	
	lib.LogInfo("Bctor Supervisor is ready. Type 'new' to create a Pod.")
	fmt.Printf("\r\x1b[Kbctor ❯ ")

	for {
		ev, ok := <-events
		if !ok {
			return
		}

		switch ev.Type {
		case EventSignal:
			scx.Mu.Lock()
			for _, c := range containers {
				if c.InitPID > 0 {
					_ = unix.Kill(c.InitPID, ev.Signal)
				}
			}
			scx.Mu.Unlock()

		case EventChildExit:
			scx.Mu.Lock()
			c := findContainerByPID(containers, ev.PID)
			if c == nil {
				scx.Mu.Unlock()
				continue
			}

			// cleanup workload
			if ev.PID == c.WorkloadPID {
        lib.LogInfo("Reaper: Workload for %s exited. Killing Init...", c.Spec.ID)
        
        // Break the KeepAlive pipe to let Init die naturally
        if len(c.IPC.KeepAlive) > 1 {
             lib.FreeFd(c.IPC.KeepAlive[1]) 
        }
			}

			// cleanup init
			if ev.PID == c.InitPID {
				processedExits++
				c.State = ContainerExited
				lib.LogWarn("Reaper: Container %s (PID %d) exited", c.Spec.ID, ev.PID)

				if c.Net != nil {
					ntw.CleanupContainerNetworking(scx, c.Net)
				}

				// Cleanup handles
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

				// Release the Root if it's the only one left and it's just waiting
				if len(containers) == 1 && processedExits >= (containerCount-1) {
					for _, res := range containers {
						if res.Spec.IsNetRoot {
							lib.LogInfo("Reaper: Releasing root %s", res.Spec.ID)
							lib.FreeFd(res.IPC.KeepAlive[1])							
							break
						}
					}
				}				
			}
			scx.Mu.Unlock()			
		}
	}
}
