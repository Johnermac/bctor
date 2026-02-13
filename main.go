package main

import (
	"fmt"
	"runtime"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/lib/ntw"
	"github.com/Johnermac/bctor/sup"
	"golang.org/x/sys/unix"
)

func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	const N = 3 //numbers of caontainers to start

	containers := make(map[string]*sup.Container)
	scx := lib.SupervisorCtx{
		Handles: make(map[string]map[lib.NamespaceType]*lib.NamespaceHandle),
	}

	// REAPER SETUP
	events := make(chan sup.Event, 32)
	//lib.LogInfo("Supervisor: Starting Reaper and Signal Handler")
	sup.StartReaper(events)
	sup.StartSignalHandler(events)

	// LOG SETUP
  logChan := make(chan lib.LogMsg, 200)
	lib.GlobalLogChan = logChan // Connect the pipes
	lib.StartGlobalLogger(logChan)

	// NETWORK SETUP
	//lib.LogInfo("Supervisor: Container networking setup")
	ntw.EnableIPForwarding()
	iface, _ := ntw.DefaultRouteInterface()
	ntw.AddNATRule("10.0.0.0/24", iface)

	if err := ntw.EnsureBridge("bctor0", "10.0.0.1/24"); err != nil { return }
	alloc, _ := ntw.NewIPAlloc("10.0.0.0/24")
	scx.IPAlloc = alloc
	scx.Subnet = alloc.Subnet

	go sup.OnContainerExit(containers, &scx, events, iface)

	var rootContainer *sup.Container
	//lib.LogInfo("Supervisor: Starting %d containers", N)

	// MAIN LOOP

	for i := 1; i <= N; i++ {
		ipc, err := lib.NewIPC()
		if err != nil {
			lib.LogError("Failed to create IPC for container %d: %v", i, err)
			continue
		}

		lib.LogInfo("-> Supervisor: container %d Flow started", i)
		spec := lib.DefaultShellSpec()
		spec.ID = fmt.Sprintf("bctor-c%d", i)

		if i == 1 {
			// 1st is the creator
			spec.IsNetRoot = true			
			lib.LogInfo("Container %s=Creator", spec.ID)
		} else {
			// rest are joiners
			spec.Namespaces.USER = false
			spec.Namespaces.MOUNT = false
			spec.Namespaces.NET = false
			spec.Namespaces.PID = true
			spec.IsNetRoot = false
			spec.Shares = []lib.ShareSpec{
				{Type: lib.NSUser, FromContainer: rootContainer.Spec.ID},
				{Type: lib.NSNet, FromContainer: rootContainer.Spec.ID},
				{Type: lib.NSMnt, FromContainer: rootContainer.Spec.ID},
			}
			lib.LogInfo("Container %s=Joiner of: %s", spec.ID, rootContainer.Spec.ID)
		}

		c, err := sup.StartContainer(spec, &scx, containers, ipc)
		if err != nil {
			lib.LogError("Failed to start container %d: %v", i, err)
			continue
		}
		
		// --- LOGGING INTEGRATION ---
    // 3. Supervisor logic
		unix.Close(ipc.Log2Sup[0]) 	
		go lib.CaptureLogs(c.Spec.ID, ipc.Log2Sup[1], logChan)		

		containers[c.Spec.ID] = c
		if i == 1 {
			rootContainer = c
		}
	}

	select {}
	//sup lives forever
}
