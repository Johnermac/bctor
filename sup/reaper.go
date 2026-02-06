package sup

import (
	"fmt"

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
				fmt.Printf("[!] reaper fatal error: %v\n", err)
				continue
			}

			fmt.Printf(
				"[DBG] reaper: pid=%d exited=%v signaled=%v\n",
				pid,
				status.Exited(),
				status.Signaled(),
			)

			events <- Event{
				Type:   EventChildExit,
				PID:    pid,
				Status: status,
			}
		}
	}()
}
