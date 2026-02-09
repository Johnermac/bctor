package lib

import "golang.org/x/sys/unix"

type IPC struct {
	UserNSPipe 	[2]int // pipe	
	Init2Sup 		[2]int // unix socket
	Sup2Init 		[2]int // unix socket
}

func NewIPC() (*IPC, error) {
	var c IPC
	var fds [2]int
	var err error

	if err = unix.Pipe(c.UserNSPipe[:]); err != nil {
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

	return &c, nil
}

