package main

import (
	"fmt"
	"log"
	"runtime"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/sup"
)

var ctx lib.SupervisorCtx

func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ctx := sup.RunSupervisor(ctx)

	containers := make(map[string]*sup.Container)
	events := make(chan sup.Event, 32)
	fmt.Printf("[*] Starting Reaper and Signal Handler\n")
	sup.StartReaper(events)
	sup.StartSignalHandler(events)

	fmt.Printf("[*] Starting Container with default spec\n")

	c, err := sup.StartContainer(lib.DefaultShellSpec(), ctx, containers)
	if err != nil {
		log.Fatal(err)
	}

	containers[c.Spec.ID] = c
	fmt.Printf("[*] container added to map: %s\n", c.Spec.ID)

	fmt.Printf("[!] initPID: %d\n", c.InitPID)
	fmt.Printf("[!] WorkloadPID: %d\n", c.WorkloadPID)

	fmt.Printf("[*] Supervisor loop started\n")
	sup.SupervisorLoop(containers, events)

}
