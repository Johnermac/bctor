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

type appState struct {
	containers map[string]*sup.Container
	scx        lib.SupervisorCtx
	mtx        *lib.Multiplexer
	loggerDone chan bool
	logChan    chan lib.LogMsg
	iface      string
	rootReady  chan *sup.Container
	wg         sync.WaitGroup
}

func setup(containerCount int) (*appState, error) {
	state := &appState{
		containers: make(map[string]*sup.Container),
		scx: lib.SupervisorCtx{
			Handles: make(map[string]map[lib.NamespaceType]*lib.NamespaceHandle),
		},
		rootReady:  make(chan *sup.Container, 1),
		loggerDone: make(chan bool),
		logChan:    make(chan lib.LogMsg, 200),
	}

	events := make(chan sup.Event, 32)
	sup.StartReaper(events)
	sup.StartSignalHandler(events)

	state.mtx = lib.NewMultiplexer()
	go state.mtx.RunLoop()

	lib.GlobalLogChan = state.logChan
	go lib.StartGlobalLogger(state.logChan, state.loggerDone, state.mtx)

	ntw.EnableIPForwarding()
	iface, _ := ntw.DefaultRouteInterface()
	state.iface = iface
	ntw.AddNATRule("10.0.0.0/24", iface)

	if err := ntw.EnsureBridge("bctor0", "10.0.0.1/24"); err != nil {
		return nil, err
	}

	alloc, _ := ntw.NewIPAlloc("10.0.0.0/24")
	state.scx.IPAlloc = alloc
	state.scx.Subnet = alloc.Subnet
	state.scx.ParentNS, _ = lib.ReadNamespaces(os.Getpid())

	state.wg.Add(1)
	go sup.OnContainerExit(state.containers, &state.scx, events, state.iface, &state.wg, containerCount)
	state.wg.Add(1)

	return state, nil
}

func startCreator(index int, runMode lib.ExecutionMode, runProfile lib.Profile, state *appState, ipc *lib.IPC) (*sup.Container, error) {
	spec := lib.DefaultSpec()
	spec.ID = fmt.Sprintf("bctor-c%d", index)
	spec.IsNetRoot = true
	spec.Workload.Mode = runMode
	spec.Seccomp = runProfile

	lib.LogInfo("Container %s=Creator", spec.ID)

	c, err := sup.StartContainer(spec, state.logChan, &state.scx, state.containers, ipc, &state.wg)
	if err != nil {
		return nil, err
	}
	c.IPC = ipc

	if spec.Workload.Mode == lib.ModeBatch {
		readFd := ipc.Log2Sup[0]
		state.wg.Add(1)
		go lib.CaptureLogs(spec.ID, readFd, ipc.Log2Sup[1], spec.Workload.Mode, state.logChan, &state.wg)
	} else {
		unix.Close(ipc.Log2Sup[1])
	}

	ipc.PtySlave.Close()
	state.mtx.Register(spec.ID, ipc.PtyMaster, c.WorkloadPID)

	return c, nil
}

func startJoiner(index int, root *sup.Container, runMode lib.ExecutionMode, runProfile lib.Profile, state *appState, ipc *lib.IPC) {
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

	cj, err := sup.StartContainer(jspec, state.logChan, &state.scx, state.containers, ipc, &state.wg)
	if err != nil {
		lib.LogError("Start Joiner %s failed: %v", jspec.ID, err)
		return
	}
	cj.IPC = ipc

	if ipc.PtyMaster == nil {
		lib.LogError("FATAL: PtyMaster for %s is nil", jspec.ID)
		return
	}

	if jspec.Workload.Mode == lib.ModeInteractive {
		unix.Close(ipc.Log2Sup[1])
	} else {
		readFd := ipc.Log2Sup[0]
		state.wg.Add(1)
		go lib.CaptureLogs(jspec.ID, readFd, ipc.Log2Sup[1], jspec.Workload.Mode, state.logChan, &state.wg)
	}

	state.mtx.Register(jspec.ID, ipc.PtyMaster, cj.WorkloadPID)

	ipc.PtySlave.Close()
	lib.FreeFd(cj.IPC.KeepAlive[1])
}

func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	const N = 5 // number of containers to start
	runMode := lib.ModeInteractive
	runProfile := lib.ProfileDebugShell
	if runMode == lib.ModeBatch {
		runProfile = lib.ProfileIpLink
	}

	state, err := setup(N)
	if err != nil {
		lib.LogError("setup failed: %v", err)
		return
	}

	// ============================================================
	// Main Loop
	// ============================================================
	for i := 1; i <= N; i++ {
		ipc, _ := lib.NewIPC()
		time.Sleep(20 * time.Millisecond)

		if i == 1 {
			c, err := startCreator(i, runMode, runProfile, state, ipc)
			if err != nil {
				lib.LogError("Start Root failed: %v", err)
				return
			}
			state.rootReady <- c
		} else {
			root := <-state.rootReady
			state.rootReady <- root
			go startJoiner(i, root, runMode, runProfile, state, ipc)
		}
	}

	state.wg.Done() //for loop
	state.wg.Wait() // CaptureLogs, Reaper
	close(state.logChan)
	<-state.loggerDone

}
