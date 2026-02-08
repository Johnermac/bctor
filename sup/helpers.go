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


/*func RunSupervisor(ctx lib.SupervisorCtx) lib.SupervisorCtx {

	// fd[0] is the read end, fd[1] is the write end
	err := unix.Pipe2(ctx.P2C[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

	err = unix.Pipe2(ctx.C2P[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

	err = unix.Pipe2(ctx.Init2sup[:], unix.O_CLOEXEC)
	if err != nil {
		panic(err)
	}

	ctx.ParentNS, _ = lib.ReadNamespaces(os.Getpid())

	return ctx

}*/
