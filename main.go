package main

import (
	"fmt"
	"runtime"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/lib/ntw"
	"github.com/Johnermac/bctor/sup"
)



func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	
	const N = 1 //numbers of caontainers to start

	containers := make(map[string]*sup.Container)
	scx := lib.SupervisorCtx{
        Handles: make(map[string]map[lib.NamespaceType]*lib.NamespaceHandle),
    }

	events := make(chan sup.Event, 32)
	//lib.LogInfo("Supervisor: Starting Reaper and Signal Handler")
	sup.StartReaper(events)
	sup.StartSignalHandler(events)

	lib.LogInfo("Supervisor: Container networking setup")
	ntw.EnableIPForwarding()
	iface, _ := ntw.DefaultRouteInterface()
	ntw.AddNATRule("10.0.0.0/24", iface)

	if err := ntw.EnsureBridge("bctor0", "10.0.0.1/24"); err != nil { return }
	alloc, _ := ntw.NewIPAlloc("10.0.0.0/24")
	scx.IPAlloc = alloc
	scx.Subnet = alloc.Subnet 

	go sup.OnContainerExit(containers, &scx, events, iface)
	
	var rootContainer *sup.Container
  lib.LogInfo("Supervisor: Starting %d containers", N)	

	for i := 1; i <= N; i++ {
		ipc, err := lib.NewIPC()
		if err != nil {
				lib.LogError("Failed to create IPC for container %d: %v", i, err)
				continue
		}

		lib.LogInfo("Supervisor: container %d Flow started\n", i)
		spec := lib.DefaultShellSpec()
		spec.ID = fmt.Sprintf("bctor-c%d", i)

		if i == 1 {
				// 1st is the creator
				spec.Namespaces.NET = true
				lib.LogInfo("Container %s will be the Namespace Root", spec.ID)
		} else {
				// rest are joiners				
				spec.Namespaces.USER = false
				spec.Shares = []lib.ShareSpec{
						{Type: lib.NSUser, FromContainer: rootContainer.Spec.ID},
						{Type: lib.NSNet, FromContainer: rootContainer.Spec.ID},
				}
				lib.LogInfo("Container %s joining namespaces of %s", spec.ID, rootContainer.Spec.ID)
		}	

		c, err := sup.StartContainer(spec, &scx, containers, ipc)
		if err != nil {
				lib.LogError("Failed to start container %d: %v", i, err)
				continue
		}
		containers[c.Spec.ID] = c
		if i == 1 {
			rootContainer = c
		}
  }	
	
	select {}
	//sup lives forever
}
