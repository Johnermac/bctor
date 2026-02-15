package lib

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// Collect created namespace FDs from init to send to supervisor
func CollectCreatedNamespaceFDs(spec *ContainerSpec) map[NamespaceType]int {
	fds := make(map[NamespaceType]int)

	joined := make(map[NamespaceType]bool)
	for _, s := range spec.Shares {
		joined[s.Type] = true
	}

	for _, ns := range []NamespaceType{
		NSUser, NSNet, NSMnt, NSPID, NSIPC, NSUTS, NSCgroup,
	} {
		if !specWantsNamespace(spec, ns) {
			continue
		}
		if joined[ns] {
			continue
		}

		path := nsTypeToProcPath(ns)
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		fds[ns] = fd
	}

	return fds
}

// Send created namespace FDs from init to supervisor
func SendCreatedNamespaceFDs(ipc *IPC, fds map[NamespaceType]int) error {
	count := len(fds)

	buf := make([]byte, 1+count)
	buf[0] = byte(count)

	if count == 0 {
		err := unix.Sendmsg(ipc.Init2Sup[1], buf, nil, nil, 0)
		return err
	}

	oobfds := make([]int, 0, count)

	i := 0
	for ns, fd := range fds {
		buf[1+i] = byte(ns)
		oobfds = append(oobfds, fd)
		i++
	}

	oob := unix.UnixRights(oobfds...)
	return unix.Sendmsg(ipc.Init2Sup[1], buf, oob, nil, 0)
}

// Supervisor receives created namespace FDs from init
func RecvCreatedNamespaceFDs(ipc *IPC) (map[NamespaceType]int, error) {
	buf := make([]byte, 256)
	oob := make([]byte, unix.CmsgSpace(8*8))

	n, oobn, _, _, err := unix.Recvmsg(ipc.Init2Sup[0], buf, oob, 0)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("EOF: child process exited before sending FDs")
	}

	count := int(buf[0])
	if count == 0 {
		return make(map[NamespaceType]int), nil
	}

	// --- SAFETY CHECK ---
	if oobn == 0 {
		return nil, fmt.Errorf("received %d bytes but 0 file descriptors. Child likely crashed.", n)
	}

	cmsgs, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, err
	}

	// --- PREVENT PANIC HERE ---
	if len(cmsgs) == 0 {
		return nil, fmt.Errorf("no unix control messages found in OOB data")
	}

	fds, err := unix.ParseUnixRights(&cmsgs[0])
	if err != nil {
		return nil, err
	}
	// ---------------------

	if len(fds) != count {
		return nil, fmt.Errorf("fd count mismatch: expected %d, got %d", count, len(fds))
	}

	out := make(map[NamespaceType]int)
	for i := 0; i < count; i++ {
		ns := NamespaceType(buf[1+i])
		out[ns] = fds[i]
	}

	return out, nil
}

// Joining container → Supervisor: send workload PID only
func SendWorkloadPID(ipc *IPC, pid int) error {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(pid))
	return unix.Sendmsg(ipc.Init2Sup[1], buf, nil, nil, 0) // write to Init2Sup[1] (init → sup)
}

// Supervisor receives workload PID from init
func RecvWorkloadPID(ipc *IPC) int {
	buf := make([]byte, 4)
	//LogInfo("RecvWorkloadPID")

	n, _, _, _, err := unix.Recvmsg(ipc.Init2Sup[0], buf, nil, 0)
	if err != nil {
		panic(err)
	}
	if n != 4 {
		panic("invalid workload PID message")
	}

	return int(binary.LittleEndian.Uint32(buf))
}

// NEW > Supervisor → joiner init: send N FDs + types
func SendNamespaceFDs(
	ipc *IPC,
	scx *SupervisorCtx,
	handles map[NamespaceType]*NamespaceHandle,
	shares []ShareSpec,
) error {

	count := len(shares)
	if count == 0 {
		return nil
	}
	scx.Mu.Lock() // LOCK: Protect the read from scx.Handles
	from := shares[0].FromContainer
	handles, ok := scx.Handles[from]
	if !ok {
		scx.Mu.Unlock()
		return fmt.Errorf("missing namespace handle for container: %s", from)
	}

	// ---- in-band payload ----
	buf := make([]byte, 1+count)
	buf[0] = byte(count)
	fds := make([]int, 0, count)

	for i, s := range shares {
		h, ok := handles[s.Type]
		if !ok {
			scx.Mu.Unlock()
			return fmt.Errorf("missing namespace handle: %v", s.Type)
		}
		buf[1+i] = byte(s.Type)
		fds = append(fds, h.FD)
		h.Ref++
	}
	scx.Mu.Unlock()

	// ---- out-of-band ----
	oob := unix.UnixRights(fds...)
	return unix.Sendmsg(ipc.Sup2Init[1], buf, oob, nil, 0)
}

// NEW > Joiner init ← Supervisor: receive N FDs + types
func RecvNamespaceFDs(ipc *IPC) map[NamespaceType]int {
	// max: count(1) + N types
	buf := make([]byte, 32)
	//LogInfo("RecvNamespaceFDs")
	oob := make([]byte, unix.CmsgSpace(8*8)) // up to 8 FDs
	_, oobn, _, _, err := unix.Recvmsg(ipc.Sup2Init[0], buf, oob, 0)
	if err != nil {
		panic(err)
	}

	count := int(buf[0])
	types := buf[1 : 1+count]

	cmsgs, _ := unix.ParseSocketControlMessage(oob[:oobn])
	fds, _ := unix.ParseUnixRights(&cmsgs[0])

	if len(fds) != count {
		panic("fd/type count mismatch")
	}

	out := make(map[NamespaceType]int, count)
	for i := 0; i < count; i++ {
		out[NamespaceType(types[i])] = fds[i]
	}

	return out
}

// Supervisor registers namespace handles for a container (for future sharing)
func RegisterNamespaceHandles(
	scx *SupervisorCtx,
	containerID string,
	fds map[NamespaceType]int,
) {
	scx.Mu.Lock()
	defer scx.Mu.Unlock()

	if scx.Handles[containerID] == nil {
		scx.Handles[containerID] = make(map[NamespaceType]*NamespaceHandle)
	}

	for ns, fd := range fds {
		scx.Handles[containerID][ns] = &NamespaceHandle{
			FD:  fd,
			Ref: 1,
		}
	}
}
