package lib

import (
	"encoding/binary"
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)



func joinNetNS(fd int) error {
	return unix.Setns(fd, unix.CLONE_NEWNET)
}

// Init (creator) → Supervisor: send workload PID + userns FD + netns FD
func SendWorkPIDUserNetNS(ipc *IPC, pid int, usernsFD int, netnsFD int) error {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(pid))

	// send BOTH FDs in one SCM_RIGHTS message
	oob := unix.UnixRights(usernsFD, netnsFD)

	return unix.Sendmsg(ipc.Init2Sup[1], buf, oob, nil, 0)
}


// Supervisor (creator) → receive workload PID + userns FD + netns FD
func RecvWorkPIDUserNetNS(ipc *IPC) (pid int, usernsFD int, netnsFD int) {
	buf := make([]byte, 4)
	oob := make([]byte, unix.CmsgSpace(2*4)) // space for 2 FDs

	n, oobn, _, _, err := unix.Recvmsg(ipc.Init2Sup[0], buf, oob, 0)
	if err != nil {
		panic(err)
	}
	if n != 4 {
		panic("invalid workload PID message size")
	}

	pid = int(binary.LittleEndian.Uint32(buf))

	cmsgs, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil || len(cmsgs) != 1 {
		panic("invalid SCM_RIGHTS message")
	}

	fds, err := unix.ParseUnixRights(&cmsgs[0])
	if err != nil || len(fds) != 2 {
		panic("expected exactly 2 FDs (userns, netns)")
	}

	usernsFD = fds[0]
	netnsFD  = fds[1]

	return
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

	n, _, _, _, err := unix.Recvmsg(ipc.Init2Sup[0], buf, nil, 0)
	if err != nil {
		panic(err)
	}
	if n != 4 {
		panic("invalid workload PID message")
	}

	return int(binary.LittleEndian.Uint32(buf))
}


// Supervisor → joiner init: send userns + netns
func SendUserNetNSFD(ipc *IPC, usernsFD int, netnsFD int) error {
	oob := unix.UnixRights(usernsFD, netnsFD)
	return unix.Sendmsg(ipc.Sup2Init[1], nil, oob, nil, 0)
}


// Joiner init ← Supervisor: receive userns + netns
func RecvUserNetNSFD(ipc *IPC) (int, int) {
	oob := make([]byte, unix.CmsgSpace(8))
	_, oobn, _, _, err := unix.Recvmsg(ipc.Sup2Init[0], nil, oob, 0)
	if err != nil {
		panic(err)
	}
	cmsgs, _ := unix.ParseSocketControlMessage(oob[:oobn])
	fds, _ := unix.ParseUnixRights(&cmsgs[0])

	return fds[0], fds[1]
}




// Wait for the supervisor to signal that USER namespace setup is complete
func WaitForUserNSSetup(syncFD int) error {
		var buf [1]byte
		_, err := unix.Read(syncFD, buf[:])
		return err
}

func SetupUserNSAndContinue(pid int, syncFD int) error {
	pidStr := strconv.Itoa(pid)

	if err := denySetgroups(pidStr); err != nil {
		fmt.Fprintf(os.Stderr, "--[?] Failed to deny setgroups for PID %s: %v\n", pidStr, err)
		return err
	}
	fmt.Printf("--[>] Supervisor: Denied setgroups for PID %s\n", pidStr)

	if err := writeUIDMap(pidStr); err != nil {
		fmt.Fprintf(os.Stderr, "--[?] Failed to write UID map for PID %s: %v\n", pidStr, err)
		return err
	}
	fmt.Printf("--[>] Supervisor: Wrote UID map for PID %s\n", pidStr)

	if err := writeGIDMap(pidStr); err != nil {
		fmt.Fprintf(os.Stderr, "--[?] Failed to write GID map for PID %s: %v\n", pidStr, err)
		return err
	}
	fmt.Printf("--[>] Supervisor: Wrote GID map for PID %s\n", pidStr)

	_, err := unix.Write(syncFD, []byte{1})
	return err
}


