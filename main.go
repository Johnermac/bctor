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
	"golang.org/x/sys/unix"
)

func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	const N = 1 // number of containers to start
	runMode := lib.ModeInteractive
	runProfile := lib.ProfileDebugShell
	if runMode == lib.ModeBatch {
		runProfile = lib.ProfileIpLink
	}

	// ============================================================
	// Core Runtime State
	// ============================================================

	containers := make(map[string]*sup.Container)

	scx := lib.SupervisorCtx{
		Handles: make(map[string]map[lib.NamespaceType]*lib.NamespaceHandle),
	}

	// ============================================================
	// Process Lifecycle (Reaper + Signals)
	// ============================================================

	events := make(chan sup.Event, 32)

	sup.StartReaper(events)
	sup.StartSignalHandler(events)

	// ============================================================
	// Multiplex Implementation
	// ============================================================

	mtx := lib.NewMultiplexer()
	go mtx.RunLoop()

	// ============================================================
	// Logging Subsystem
	// ============================================================

	loggerDone := make(chan bool)
	logChan := make(chan lib.LogMsg, 200)

	lib.GlobalLogChan = logChan
	go lib.StartGlobalLogger(logChan, loggerDone, mtx)

	// ============================================================
	// Networking Bootstrap
	// ============================================================

	ntw.EnableIPForwarding()

	iface, _ := ntw.DefaultRouteInterface()
	ntw.AddNATRule("10.0.0.0/24", iface)

	if err := ntw.EnsureBridge("bctor0", "10.0.0.1/24"); err != nil {
		return
	}

	alloc, _ := ntw.NewIPAlloc("10.0.0.0/24")
	scx.IPAlloc = alloc
	scx.Subnet = alloc.Subnet

	// ============================================================
	// Container Coordination
	// ============================================================

	rootReady := make(chan *sup.Container, 1)
	var wg sync.WaitGroup

	wg.Add(1) // close OnContainerExit
	go sup.OnContainerExit(containers, &scx, events, iface, &wg, N)
	scx.ParentNS, _ = lib.ReadNamespaces(os.Getpid())

	wg.Add(1) // close after this main loop

	// ============================================================
	// Main Loop
	// ============================================================
	for i := 1; i <= N; i++ {
		ipc, _ := lib.NewIPC()
		time.Sleep(20 * time.Millisecond)

		if i == 1 {
			// --- CREATOR ---
			spec := lib.DefaultSpec()
			spec.ID = fmt.Sprintf("bctor-c%d", i)
			spec.IsNetRoot = true
			spec.Workload.Mode = runMode
			spec.Seccomp = runProfile

			lib.LogInfo("Container %s=Creator", spec.ID)

			c, err := sup.StartContainer(spec, logChan, &scx, containers, ipc, &wg)
			if err != nil {
				lib.LogError("Start Root failed: %v", err)
				return
			}
			c.IPC = ipc

			var readFd int
			if spec.Workload.Mode == lib.ModeBatch {
				readFd = ipc.Log2Sup[0]

				wg.Add(1)
				go lib.CaptureLogs(spec.ID, readFd, ipc.Log2Sup[1], spec.Workload.Mode, logChan, &wg)
			} else {
				unix.Close(ipc.Log2Sup[1])
			}

			ipc.PtySlave.Close()
			mtx.Register(spec.ID, ipc.PtyMaster, c.WorkloadPID)

			rootReady <- c
		} else {
			// --- JOINERS ---
			root := <-rootReady
			rootReady <- root

			go func(index int, jipc *lib.IPC) {
				jspec := lib.DefaultSpec()
				jspec.Workload.Mode = runMode
				jspec.ID = fmt.Sprintf("bctor-c%d", index)
				jspec.Seccomp = runProfile

				jspec.Namespaces.USER = false
				jspec.Namespaces.MOUNT = true
				jspec.Namespaces.NET = true
				jspec.Namespaces.PID = false
				jspec.IsNetRoot = false

				jspec.Shares = []lib.ShareSpec{
					{Type: lib.NSUser, FromContainer: root.Spec.ID},
					{Type: lib.NSNet, FromContainer: root.Spec.ID},
					{Type: lib.NSMnt, FromContainer: root.Spec.ID},
				}

				lib.LogInfo("Container %s=Joiner of: %s", jspec.ID, root.Spec.ID)

				cj, err := sup.StartContainer(jspec, logChan, &scx, containers, jipc, &wg)
				if err != nil {
					lib.LogError("Start Joiner %s failed: %v", jspec.ID, err)
					return
				}
				cj.IPC = jipc

				if jipc.PtyMaster == nil {
					lib.LogError("FATAL: PtyMaster for %s is nil", jspec.ID)
					return
				}

				if jspec.Workload.Mode == lib.ModeInteractive {
					unix.Close(jipc.Log2Sup[1])
				} else {
					readFd := jipc.Log2Sup[0]

					wg.Add(1)
					go lib.CaptureLogs(jspec.ID, readFd, jipc.Log2Sup[1], jspec.Workload.Mode, logChan, &wg)
				}

				mtx.Register(jspec.ID, jipc.PtyMaster, cj.WorkloadPID)

				jipc.PtySlave.Close()
				lib.FreeFd(cj.IPC.KeepAlive[1])
			}(i, ipc)
		}
	}

	wg.Done() //for loop
	wg.Wait() // CaptureLogs,  Reaper
	close(logChan)
	<-loggerDone

}
