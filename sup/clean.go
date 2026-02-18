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
) {
	defer wg.Done()
	lib.LogInfo("Bctor Supervisor is ready. Type 'new' to create a Pod.")
	fmt.Printf("\r\x1b[Kbctor ❯ ")
	podNetResources := make(map[string]*ntw.NetResources)

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

			// --- 1. WORKLOAD CLEANUP ---
			if ev.PID == c.WorkloadPID {
				lib.LogInfo("Reaper: Workload for %s exited. Killing Init...", c.Spec.ID)

				// Only NetRoot init waits on KeepAlive; release it once.
				if c.Spec.IsNetRoot && c.IPC != nil && c.IPC.KeepAlive[1] >= 0 {
					lib.FreeFd(c.IPC.KeepAlive[1])
					c.IPC.KeepAlive[1] = -1
				}
			}

			// --- 2. INIT / NETWORK / FORWARD CLEANUP ---
			if ev.PID == c.InitPID {
				c.State = ContainerExited
				lib.LogWarn("Reaper: Container %s (PID %d) exited", c.Spec.ID, ev.PID)

				podName, _, isPodContainer := splitContainerID(c.Spec.ID)
				if c.Net != nil && isPodContainer {
					// Root may exit before joiners; save net resources until pod is empty.
					podNetResources[podName] = c.Net
				}

				// Cleanup Namespace Handles
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

				// Finally, remove from the global map
				delete(containers, c.Spec.ID)
				releaseLastNetRootKeepAlive(podName, containers)

				// check if pod is empty
				empty := isPodContainer && isPodEmpty(podName, containers)
				if empty {
					scx.Forwards.CleanupForward(podName)

					if netres := podNetResources[podName]; netres != nil {
						ntw.CleanupContainerNetworking(scx, netres)
						delete(podNetResources, podName)
						lib.LogWarn("Reaper: Network for %s cleaned up", podName)
					}

					lib.LogWarn("Reaper: Last container in %s exited. Closing forwards.", podName)
				}
			}

			scx.Mu.Unlock()
		}
	}
}

func isPodEmpty(podName string, containers map[string]*Container) bool {
	for id := range containers {
		if pID, _, ok := splitContainerID(id); ok && pID == podName {
			return false // Found another container belonging to this pod
		}
	}
	return true
}

func releaseLastNetRootKeepAlive(podName string, containers map[string]*Container) {
	var root *Container
	count := 0

	for id, c := range containers {
		pID, _, ok := splitContainerID(id)
		if !ok || pID != podName {
			continue
		}
		count++
		if c.Spec != nil && c.Spec.IsNetRoot {
			root = c
		}
	}

	if count == 1 && root != nil && root.IPC != nil && root.IPC.KeepAlive[1] >= 0 {
		lib.FreeFd(root.IPC.KeepAlive[1])
		root.IPC.KeepAlive[1] = -1
	}
}
