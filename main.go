package main

import (
	"fmt"
	"runtime"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/sup"
)

var scx lib.SupervisorCtx

func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	containers := make(map[string]*sup.Container)
	events := make(chan sup.Event, 32)
	fmt.Printf("[*] Supervisor: Starting Reaper and Signal Handler\n")
	sup.StartReaper(events)
	sup.StartSignalHandler(events)
	ipc1, _ := lib.NewIPC()
	ipc2, _ := lib.NewIPC()

	fmt.Printf("[!] Supervisor: Starting containers\n")

	fmt.Printf("[>] Supervisor: container 1 Flow started\n")
	spec1 := lib.DefaultShellSpec()
	spec1.Namespaces.NET = true
	c1, _ := sup.StartContainer(spec1, scx, containers, ipc1)
	containers[c1.Spec.ID] = c1

	fmt.Printf("[>] Supervisor: container 2 Flow started\n")
	// container 2 JOINS container 1 netns
	spec2 := lib.DefaultShellSpec()
	spec2.Namespaces.NET = false
	spec2.Namespaces.USER = false
	spec2.ShareUserNS = c1.UserNS
	spec2.ShareNetNS = c1.NetNS
	c1.UserNS.Ref++
	c1.NetNS.Ref++

	c2, _ := sup.StartContainer(spec2, scx, containers, ipc2)
	containers[c2.Spec.ID] = c2

	fmt.Printf("[!] Supervisor: All containers started and running\n")
	sup.SupervisorLoop(containers, events)
	//program ended

}
