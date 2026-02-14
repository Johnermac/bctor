package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/lib/ntw"
	"github.com/Johnermac/bctor/sup"
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
	loggerDone := make(chan bool)
	logChan := make(chan lib.LogMsg, 200)
	lib.GlobalLogChan = logChan // Connect the pipes
	go lib.StartGlobalLogger(logChan, loggerDone)

	// NETWORK SETUP
	//lib.LogInfo("Supervisor: Container networking setup")
	ntw.EnableIPForwarding()
	iface, _ := ntw.DefaultRouteInterface()
	ntw.AddNATRule("10.0.0.0/24", iface)

	if err := ntw.EnsureBridge("bctor0", "10.0.0.1/24"); err != nil {
		return
	}
	alloc, _ := ntw.NewIPAlloc("10.0.0.0/24")
	scx.IPAlloc = alloc
	scx.Subnet = alloc.Subnet	
	
	rootReady := make(chan *sup.Container, 1)
	var wg sync.WaitGroup	

	wg.Add(1) // close OnContainerExit
	go sup.OnContainerExit(containers, &scx, events, iface, &wg, N)
	scx.ParentNS, _ = lib.ReadNamespaces(os.Getpid())

	wg.Add(1) // close after this main loop
	for i := 1; i <= N; i++ {    
    ipc, _ := lib.NewIPC()
    wg.Add(1) // close on CaptureLogs
		time.Sleep(20 * time.Millisecond)
    
    if i == 1 {
        // --- CREATOR ---
        spec := lib.DefaultShellSpec()
        spec.ID = fmt.Sprintf("bctor-c%d", i)
        spec.IsNetRoot = true

        
        lib.LogInfo("Container %s=Creator", spec.ID)
        c, err := sup.StartContainer(spec, logChan, &scx, containers, ipc, &wg)
        if err != nil {
            lib.LogError("Start Root failed: %v", err)           
            return 
        }
        c.IPC = ipc
        rootReady <- c        
    } else {
        // --- JOINERS ---
        root := <-rootReady
        rootReady <- root        
        
        go func(index int, jipc *lib.IPC) {            
            
            jspec := lib.DefaultShellSpec()
            jspec.ID = fmt.Sprintf("bctor-c%d", index)
            jspec.Namespaces.USER = false
            jspec.Namespaces.MOUNT = true
            jspec.Namespaces.NET = true
            jspec.Namespaces.PID = false
            jspec.IsNetRoot = false
            jspec.Shares = []lib.ShareSpec{
                {Type: lib.NSUser, FromContainer: root.Spec.ID},
                {Type: lib.NSNet, FromContainer: root.Spec.ID},
            }

            lib.LogInfo("Container %s=Joiner of: %s", jspec.ID, root.Spec.ID)
            cj, err := sup.StartContainer(jspec, logChan, &scx, containers, jipc, &wg)
            if err != nil {
                lib.LogError("Start Joiner %s failed: %v", jspec.ID, err)
                return
            }           
            cj.IPC = jipc
            lib.FreeFd(cj.IPC.KeepAlive[1])
        }(i, ipc)
    }
	}
	wg.Done()

	lib.LogInfo("Supervisor: Waiting for containers to produce output...")
  
	wg.Wait()
	close(logChan)
	<-loggerDone
	
}
