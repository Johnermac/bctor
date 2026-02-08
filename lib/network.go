package lib

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

type NetNamespace struct {
    FD  	int    // open netns fd (O_PATH)
    Ref  	int		
}

func joinNetNS(fd int) error {
	return unix.Setns(fd, unix.CLONE_NEWNET)
}

// For creator containers
func SendWorkloadPIDAndNetNS(ctx SupervisorCtx, pid int, netnsFD int) error {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(pid))

	oob := unix.UnixRights(netnsFD)

	return unix.Sendmsg(
		ctx.Init2sup[1],
		buf,
		oob,
		nil,
		0,
	)
}

func RecvWorkloadPIDAndNetNS(ctx SupervisorCtx) (int, int) {
	buf := make([]byte, 4)
	oob := make([]byte, unix.CmsgSpace(4))

	n, oobn, _, _, err := unix.Recvmsg(
		ctx.Init2sup[0],
		buf,
		oob,
		0,
	)
	if err != nil {
		panic(err)
	}
	if n != 4 || oobn == 0 {
		panic("invalid creator container message")
	}

	pid := int(binary.LittleEndian.Uint32(buf))

	cmsgs, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil || len(cmsgs) != 1 {
		panic("invalid SCM_RIGHTS message")
	}

	fds, err := unix.ParseUnixRights(&cmsgs[0])
	if err != nil || len(fds) != 1 {
		panic("invalid netns fd")
	}

	return pid, fds[0]
}

// For joining containers (PID only, still sendmsg/recvmsg)
func SendWorkloadPID(ctx SupervisorCtx, pid int) error {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(pid))
	return unix.Sendmsg(ctx.Init2sup[1], buf, nil, nil,	0)
}

func RecvWorkloadPID(ctx SupervisorCtx) int {

	buf := make([]byte, 4)

	n, _, _, _, err := unix.Recvmsg(
		ctx.Init2sup[0],
		buf,
		nil,
		0,
	)
	if err != nil {
		panic(err)
	}
	if n != 4 {
		panic("invalid workload PID message")
	}

	return int(binary.LittleEndian.Uint32(buf))
}

// supervisor → joiner init
func SendNetNSFD(ctx SupervisorCtx, fd int) error {
    oob := unix.UnixRights(fd)
    return unix.Sendmsg(ctx.P2C[1], nil, oob, nil, 0)
}

// joiner init ← supervisor
func RecvNetNSFD(ctx SupervisorCtx) int {
    oob := make([]byte, unix.CmsgSpace(4))
    _, oobn, _, _, err := unix.Recvmsg(ctx.P2C[0], nil, oob, 0)
    if err != nil {
        panic(err)
    }
    cmsgs, _ := unix.ParseSocketControlMessage(oob[:oobn])
    fds, _ := unix.ParseUnixRights(&cmsgs[0])
    return fds[0]
}




