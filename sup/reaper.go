package sup

import (
	"github.com/Johnermac/bctor/lib"
	"golang.org/x/sys/unix"
)

func StartReaper(events chan<- Event) {
	go func() {
		for {
			pid, status, err := waitForAnyChild()
			if err != nil {
				if err == unix.EINTR || err == unix.ECHILD {
					continue
				}
				lib.LogError("Supervisor: Reaper fatal error: %v\n", err)
				continue
			}
			/*
				fmt.Printf(
					"[DBG] Supervisor: Reaper: pid=%d exited=%v signaled=%v\n",
					pid,
					status.Exited(),
					status.Signaled(),
				)*/

			events <- Event{
				Type:   EventChildExit,
				PID:    pid,
				Status: status,
			}
		}
	}()
}
