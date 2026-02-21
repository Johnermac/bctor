package sup

import (
	"golang.org/x/sys/unix"
)

func waitForAnyChild() (int, unix.WaitStatus, error) {
	var status unix.WaitStatus
	pid, err := unix.Wait4(-1, &status, 0, nil)
	if err != nil {
		return -1, status, err
	}
	return pid, status, nil
}

func findContainerByPID(containers map[string]*Container, pid int) *Container {
	for _, c := range containers {
		if c.WorkloadPID == pid || c.InitPID == pid {
			return c
		}
	}
	return nil
}
