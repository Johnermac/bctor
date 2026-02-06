package sup

import (
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
)

type EventType int

const (
	EventChildExit EventType = iota
	EventSignal
)

type Event struct {
	Type   EventType
	PID    int
	Status unix.WaitStatus
	Signal unix.Signal
}

func StartSignalHandler(events chan<- Event) chan<- os.Signal {
	sigCh := make(chan os.Signal, 16)

	signal.Notify(sigCh,
		unix.SIGINT,
		unix.SIGTERM,
		unix.SIGQUIT,
		unix.SIGHUP,
	)

	go func() {
		for sig := range sigCh {
			s, ok := sig.(unix.Signal)
			if !ok {
				continue
			}
			events <- Event{
				Type:   EventSignal,
				Signal: s,
			}
		}
	}()

	return sigCh
}
