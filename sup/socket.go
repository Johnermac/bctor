package sup

import "golang.org/x/sys/unix"

type Ctx struct {
	P2C       [2]int // pipe
	C2P       [2]int // pipe
	Init2Sup  [2]int // unix socket
	Sup2Init  [2]int // unix socket  // NEW
}

func NewCtx() (*Ctx, error) {
	var c Ctx
	if err := unix.Pipe(c.P2C[:]); err != nil { return nil, err }
	if err := unix.Pipe(c.C2P[:]); err != nil { return nil, err }
	if err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET, 0, c.Init2Sup[:]); err != nil { return nil, err }
	if err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET, 0, c.Sup2Init[:]); err != nil { return nil, err } // NEW
	return &c, nil
}
