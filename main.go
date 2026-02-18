package main

import (
	"runtime"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/sup"
)

func main() {

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	state, err := sup.Setup()
	if err != nil {
		lib.LogError("setup failed: %v", err)
		return
	}

	//state.Wg.Done() //main loop
	state.Wg.Wait() // CaptureLogs, Reaper
	close(state.LogChan)
	<-state.LoggerDone

}
