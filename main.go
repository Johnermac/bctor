package main

import (
	"fmt"
	"runtime"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/sup"
)


func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	
	containers := make(map[string]*sup.Container)
	scx := lib.SupervisorCtx{
        Handles: make(map[string]map[lib.NamespaceType]*lib.NamespaceHandle),
    }

	events := make(chan sup.Event, 32)
	fmt.Printf("[*] Supervisor: Starting Reaper and Signal Handler\n")
	sup.StartReaper(events)
	sup.StartSignalHandler(events)
	go sup.OnContainerExit(containers, &scx, events)

	ipc1, _ := lib.NewIPC()
	ipc2, _ := lib.NewIPC()

	fmt.Printf("[!] Supervisor: Starting containers\n")
	

	fmt.Printf("[>] Supervisor: container 1 Flow started\n")
	spec1 := lib.DefaultShellSpec()
	spec1.Namespaces.NET = true
	c1, _ := sup.StartContainer(spec1, &scx, containers, ipc1)
	

	fmt.Printf("[>] Supervisor: container 2 Flow started\n")
	// container 2 JOINS container 1 netns
	spec2 := lib.DefaultShellSpec()	
	spec2.Namespaces.USER = false
	spec2.Shares = []lib.ShareSpec{
    { Type: lib.NSUser, FromContainer: c1.Spec.ID },
    { Type: lib.NSNet,  FromContainer: c1.Spec.ID },
	}	

	c2, _ := sup.StartContainer(spec2, &scx, containers, ipc2)
	containers[c2.Spec.ID] = c2

	fmt.Printf("[!] Supervisor: All containers started and running\n")
	
	select {}
	//sup lives forever
}
