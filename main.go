package main

import (
	"fmt"
	"runtime"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/sup"
)

var ctx lib.SupervisorCtx

func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	//ctx := sup.RunSupervisor(ctx)

	containers := make(map[string]*sup.Container)
	events := make(chan sup.Event, 32)
	fmt.Printf("[*] Supervisor: Starting Reaper and Signal Handler\n")
	sup.StartReaper(events)
	sup.StartSignalHandler(events)

		
	fmt.Printf("[!] Supervisor: Starting containers\n")
	

	fmt.Printf("[>] Supervisor: container 1 Flow started\n")
	spec1 := lib.DefaultShellSpec()
	spec1.Namespaces.NET = true
	c1, _ := sup.StartContainer(spec1, sup.NewCtx(), containers)
	containers[c1.Spec.ID] = c1


	fmt.Printf("[>] Supervisor: container 2 Flow started\n")
	// container 2 JOINS container 1 netns
	spec2 := lib.DefaultShellSpec()
	spec2.Namespaces.NET = false
	spec2.ShareNetNS = c1.NetNS	
	c1.NetNS.Ref++

	c2, _ := sup.StartContainer(spec2, sup.NewCtx(), containers)
	containers[c2.Spec.ID] = c2	
		

	fmt.Printf("[!] Supervisor: All containers started and running\n")
	sup.SupervisorLoop(containers, events)
	//program ended

}
