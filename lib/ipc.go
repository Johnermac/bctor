package lib

import (
	"os"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type IPC struct {
	UserNSPipe  [2]int   // pipe
	UserNSReady [2]int   // pipe
	NetReady    [2]int   // pipe
	Init2Sup    [2]int   // unix socket
	PtyMaster   *os.File // Master side for the Supervisor
	PtySlave    *os.File // Slave side for the Child

	Sup2Init [2]int // unix socket // ill only delete when the pty is already working
	Log2Sup  [2]int // stdout/stderr ill only delete when the pty is already working

	KeepAlive [2]int // pause the C1 to hold net ns
}

func NewIPC() (*IPC, error) {
	var c IPC
	var fds [2]int
	var err error

	master, slave, err := pty.Open()
	if err != nil {
		return nil, err
	}

	c.PtyMaster = master
	c.PtySlave = slave

	if err = unix.Pipe(c.UserNSPipe[:]); err != nil {
		return nil, err
	}

	if err = unix.Pipe(c.UserNSReady[:]); err != nil {
		return nil, err
	}

	if err = unix.Pipe(c.NetReady[:]); err != nil {
		return nil, err
	}

	if err = unix.Pipe(c.KeepAlive[:]); err != nil {
		return nil, err
	}

	fds, err = unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET, 0)
	if err != nil {
		return nil, err
	}
	c.Init2Sup = fds

	fds, err = unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET, 0)
	if err != nil {
		return nil, err
	}
	c.Sup2Init = fds

	//ill delete later
	fds, err = unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0) // Use SOCK_STREAM for logs
	if err != nil {
		return nil, err
	}
	c.Log2Sup = fds

	return &c, nil
}

func NewFork() (uintptr, error) {
	pid, _, err := unix.RawSyscall(unix.SYS_FORK, 0, 0, 0)
	if err != 0 {
		return 0, err
	}
	return pid, nil
}
