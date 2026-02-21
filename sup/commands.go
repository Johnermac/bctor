package sup

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/Johnermac/bctor/lib"
	"github.com/Johnermac/bctor/lib/ntw"
	"golang.org/x/sys/unix"
)

// menu of commands
func (m *Multiplexer) Dispatch(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}

	switch {
	case input == "list" ||
		input == "ls" ||
		input == "l" ||
		strings.HasPrefix(input, "list ") ||
		strings.HasPrefix(input, "ls ") ||
		strings.HasPrefix(input, "l "):
		m.d_list(input)
	case strings.HasPrefix(input, ":"):
		m.d_exec(input)
	case strings.HasPrefix(input, "attach ") || strings.HasPrefix(input, "a "):
		m.d_attach(input)
	case input == "new" ||
		input == "n" ||
		strings.HasPrefix(input, "new ") ||
		strings.HasPrefix(input, "n "):
		m.d_new(input)
	case strings.HasPrefix(input, "run ") ||
		strings.HasPrefix(input, "r "):
		m.d_run(input)
	case strings.HasPrefix(input, "kill ") || strings.HasPrefix(input, "k "):
		m.d_kill(input)
	case strings.HasPrefix(input, "forward ") || strings.HasPrefix(input, "f "):
		m.d_forward(input)
	case input == "clear" || input == "cls" || input == "c":
		m.d_clear()
	case input == "exit" || input == "bye":
		m.d_exit()
	case input == "help" || input == "h":
		m.d_help()
	default:
		fmt.Printf("\r\n[-] Unknown command: %s\r\n", input)
		m.RefreshPrompt()
	}
}

func (m *Multiplexer) d_forward(input string) {
	parts := strings.Fields(input)
	if len(parts) != 3 {
		fmt.Printf("Usage: forward <id> <port>\r\n")
		return
	}

	id := fmt.Sprintf("bctor-%s", parts[1])
	port, err := strconv.Atoi(parts[2]) // FIX: Check the error here
	if err != nil || port <= 0 || port > 65535 {
		fmt.Printf("Error: invalid port number %q\r\n", parts[2])
		return
	}

	m.state.scx.Mu.Lock()
	c, ok := m.state.containers[id]
	m.state.scx.Mu.Unlock()

	if !ok {
		fmt.Printf("Error: container %s not found\r\n", id)
		return
	}

	// check interface
	if m.state.scx.Forwards == nil {
		fmt.Printf("Error: Forwarding system not initialized\r\n")
		return
	}

	podName, _, ok := splitContainerID(id) // Get "a"
	if !ok {
		fmt.Printf("Error: invalid container id\r\n")
		return
	}

	// run the session
	err = m.state.scx.Forwards.AddSession(podName, port, port, c.WorkloadPID)
	if err != nil {
		fmt.Printf("Error starting forward: %v\r\n", err)
		return
	}
	//fmt.Printf("DEBUG: Stored forward for %s on port %d\n", c.Spec.ID, port)

	lib.GlobalLogChan <- lib.LogMsg{
		Type: lib.TypeSuccess,
		Data: fmt.Sprintf("Forwarding localhost:%d -> %s:%d", port, id, port),
	}
}

func (m *Multiplexer) d_run(input string) {
	lines := []string{}
	parts := strings.Fields(input)

	if len(parts) < 2 {
		lines = append(lines, "Usage: run <pod> <cmd>  OR  run <cmd>")
		lib.DrawBox("BATCH RUN", lines)
		return
	}

	var letter string
	var cmdParts []string
	var isJoiner bool

	// check parts[1] is a pod letter
	if len(parts[1]) == 1 && (parts[1][0] >= 'a' && parts[1][0] <= 'z') {
		letter = parts[1]
		cmdParts = parts[2:]
		isJoiner = true
	} else {
		// new pod
		newLetter, _ := m.state.GetNextPodLetter()
		letter = newLetter
		cmdParts = parts[1:]
		isJoiner = false
	}

	if len(cmdParts) == 0 {
		lines = append(lines, "[-] Error: No command specified")
		lib.DrawBox("BATCH RUN", lines)
		return
	}

	fullCmd := strings.Join(cmdParts, " ")
	ipc, _ := lib.NewIPC()

	if isJoiner {
		// JOINER BATCH
		m.state.scx.Mu.Lock()
		rootID := fmt.Sprintf("bctor-%s1", letter)
		root, exists := m.state.containers[rootID]
		m.state.scx.Mu.Unlock()

		if !exists {
			lines = append(lines, fmt.Sprintf("[-] Error: Pod %s doesn't exist", letter))
		} else {
			idx := m.state.GetNextContainerIndex(letter)
			name := fmt.Sprintf("bctor-%s%d", letter, idx)

			go StartJoinerBatch(root, name, fullCmd, m.state, ipc)
			lines = append(lines, fmt.Sprintf("[+] Running batch in %s: %s", letter, fullCmd))
		}
	} else {
		// CREATOR BATCH
		c, err := StartCreatorBatch(letter, fullCmd, m.state, ipc)
		if err != nil {
			lines = append(lines, "[-] Failed to start batch creator")
		} else {
			m.state.scx.Mu.Lock()
			m.state.containers[c.Spec.ID] = c
			m.state.scx.Mu.Unlock()
			lines = append(lines, fmt.Sprintf("[+] %sNew Pod [%s] running: %s", lib.Reset, letter, fullCmd))
		}
	}

	lib.DrawBox("BATCH RUN", lines)
}

func (m *Multiplexer) d_clear() {
	// ansi scape
	fmt.Print("\033[H\033[2J")
	fmt.Printf("%sBctor Supervisor Ready%s\r\n", lib.Cyan, lib.Reset)
}

func (m *Multiplexer) d_exit() {
	lib.LogInfo("Supervisor shutting down. Cleaning up global network...")

	m.state.scx.Mu.Lock()
	for id, c := range m.state.containers {
		unix.Kill(c.WorkloadPID, unix.SIGKILL)
		delete(m.state.containers, id)
	}
	m.state.scx.Mu.Unlock()

	// delete global
	ntw.RemoveNATRule("10.0.0.0/24", m.state.iface)
	ntw.DeleteBridge("bctor0")

	lib.LogSuccess("Global cleanup complete. Goodbye!")
	os.Exit(0)
}

func (m *Multiplexer) d_kill(input string) {
	lines := []string{}
	parts := strings.Fields(input)

	if len(parts) < 2 || len(parts) > 3 {
		lines = append(lines, "Usage: kill <pod_letter> | kill <pod_letter> <index>")
		lib.DrawBox("KILL STATUS", lines)
		return
	}

	podLetter := parts[1]
	m.state.scx.Mu.Lock()
	defer m.state.scx.Mu.Unlock()

	foundAny := false

	// kill specific container
	if len(parts) == 3 {
		index := parts[2]
		targetID := fmt.Sprintf("bctor-%s%s", podLetter, index)

		if c, exists := m.state.containers[targetID]; exists {
			syscall.Kill(c.WorkloadPID, syscall.SIGKILL)
			podName, _, ok := splitContainerID(targetID)
			if ok &&
				c.Spec != nil &&
				c.Spec.IsNetRoot &&
				c.IPC != nil &&
				c.IPC.KeepAlive[1] >= 0 &&
				podWouldBeEmptyAfterRemoving(podName, targetID, m.state.containers) {
				lib.FreeFd(c.IPC.KeepAlive[1])
				c.IPC.KeepAlive[1] = -1
			}
			lines = append(lines, fmt.Sprintf("[+] Killed container %s (PID %d)", targetID, c.WorkloadPID))
			foundAny = true
		}

		// kill pod
	} else {
		prefix := fmt.Sprintf("bctor-%s", podLetter)
		for id, c := range m.state.containers {
			if strings.HasPrefix(id, prefix) {
				syscall.Kill(c.WorkloadPID, syscall.SIGKILL)
				podName, _, ok := splitContainerID(id)
				if ok &&
					c.Spec != nil &&
					c.Spec.IsNetRoot &&
					c.IPC != nil &&
					c.IPC.KeepAlive[1] >= 0 &&
					podWouldBeEmptyAfterRemoving(podName, id, m.state.containers) {
					lib.FreeFd(c.IPC.KeepAlive[1])
					c.IPC.KeepAlive[1] = -1
				}
				lines = append(lines, fmt.Sprintf("[+] Killed %s (PID %d)", id, c.WorkloadPID))
				foundAny = true
			}
		}
	}

	if !foundAny {
		lines = append(lines, fmt.Sprintf("[-] No active containers found for Pod [%s]", podLetter))
	}

	title := fmt.Sprintf("KILL POD [%s]", strings.ToUpper(podLetter))
	lib.DrawBox(title, lines)
}

func (m *Multiplexer) d_new(input string) {
	lines := []string{}
	parts := strings.Fields(input)

	// creator
	if len(parts) == 1 {
		ipc, _ := lib.NewIPC()
		letter, _ := m.state.GetNextPodLetter()
		c, err := StartCreator(letter, lib.ModeInteractive, lib.ProfileDebugShell, m.state, ipc)
		if err != nil {
			lines = append(lines, "[-] Start Creator failed")
		} else {
			m.state.scx.Mu.Lock()
			m.state.containers[c.Spec.ID] = c
			m.state.scx.Mu.Unlock()

			lines = append(lines, fmt.Sprintf("[+] Created Pod [%s]", letter))
		}

		// joiner
	} else if len(parts) >= 2 {
		letter := parts[1]
		count := 1

		// parsing
		if len(parts) == 3 {
			if val, err := strconv.Atoi(parts[2]); err == nil && val > 0 {
				count = val
			} else {
				lines = append(lines, "[-] Invalid count, defaulting to 1")
			}
		}

		m.state.scx.Mu.Lock()
		rootID := fmt.Sprintf("bctor-%s1", letter)
		root, exists := m.state.containers[rootID]
		m.state.scx.Mu.Unlock()

		if !exists {
			lines = append(lines, fmt.Sprintf("[-] Error: Pod %s does not exist", letter))
		} else {
			for i := 0; i < count; i++ {
				ipc, _ := lib.NewIPC()

				num := m.state.GetNextContainerIndex(letter)
				name := fmt.Sprintf("bctor-%s%d", letter, num)

				m.state.scx.Mu.Lock()
				m.state.containers[name] = &Container{State: ContainerInitializing}
				m.state.scx.Mu.Unlock()

				go StartJoiner(root, name, lib.ModeInteractive, lib.ProfileDebugShell, m.state, ipc)
				lines = append(lines, fmt.Sprintf("[+] Container [%s] joining Pod [%s]", name, letter))
			}
		}
	}

	lib.DrawBox("POD MANAGEMENT", lines)
}

func (m *Multiplexer) d_list(input string) {
	lines := []string{}
	parts := strings.Fields(input)

	m.mu.Lock()
	targetsCopy := make(map[string]*Target, len(m.targets))
	for id, t := range m.targets {
		targetsCopy[id] = t
	}
	m.mu.Unlock()

	// list
	if len(parts) == 1 {
		podCounts := map[string]int{}
		podAlive := map[string]int{}

		for id, t := range targetsCopy {
			pod, _, ok := splitContainerID(id)
			if !ok {
				continue
			}
			podCounts[pod]++
			if err := syscall.Kill(t.PID, 0); err == nil {
				podAlive[pod]++
			}
		}

		if len(podCounts) == 0 {
			lines = append(lines, "No running pods")
		} else {
			pods := make([]string, 0, len(podCounts))
			for pod := range podCounts {
				pods = append(pods, pod)
			}
			sort.Strings(pods)

			for _, pod := range pods {
				total := podCounts[pod]
				alive := podAlive[pod]
				dead := total - alive

				podColor := lib.Green
				if alive == 0 && total > 0 {
					podColor = lib.Red
				} else if dead > 0 {
					podColor = lib.Yellow
				}

				// health bar
				healthBar := ""
				for i := 0; i < alive; i++ {
					healthBar += lib.Green + "●"
				}
				for i := 0; i < dead; i++ {
					healthBar += lib.Red + "○"
				}
				healthBar += lib.Reset

				visualLen := utf8.RuneCountInString(lib.StripANSI(healthBar))

				// Get active forwards

				ports := m.state.scx.Forwards.List(pod)
				forwardStr := ""
				if len(ports) > 0 {
					forwardStr = fmt.Sprintf("  \x1b[90m➜ %v\x1b[0m", ports)
				}

				// spacing
				barWidth := 10
				padding := barWidth - visualLen
				if padding < 0 {
					padding = 0
				}
				spacer := strings.Repeat(" ", padding)

				line := fmt.Sprintf("%sPod [%s]%s  %s%s%s%s  Total:%2d  Alive:%s%2d%s  Dead:%s%2d%s%s",
					podColor, pod, lib.Reset,
					"\x1b[2m", healthBar, spacer, "\x1b[0m",
					total,
					lib.Cyan, alive, lib.Reset,
					lib.Red, dead, lib.Reset,
					forwardStr,
				)
				lines = append(lines, line)
			}
			lib.DrawBox("POD STATUS", lines)
		}
	} else if len(parts) == 2 {
		// list <pod>
		pod := parts[1]
		found := false
		ids := make([]string, 0, len(targetsCopy))
		for id := range targetsCopy {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		for _, id := range ids {
			t := targetsCopy[id]
			podID, _, ok := splitContainerID(id)
			if !ok || podID != pod {
				continue
			}
			found = true

			status := fmt.Sprintf("%sALIVE%s", lib.Green, lib.Reset)
			if err := syscall.Kill(t.PID, 0); err != nil {
				status = fmt.Sprintf("%sDEAD%s", lib.Red, lib.Reset)
			}
			lines = append(lines, fmt.Sprintf("%s%-12s PID:%-6d [%s]", lib.Reset, id, t.PID, status))
		}

		if !found {
			lines = append(lines, fmt.Sprintf("[-] Error: Pod %s does not exist", pod))
		}
		title := fmt.Sprintf("POD [%s] STATUS", pod)
		lib.DrawBox(title, lines)

	} else {
		lines = append(lines, "Usage: list | list <pod>")
		lib.DrawBox("POD STATUS", lines)
	}

	if m.activeID == "" {
		fmt.Print("\rbctor ❯ ")
	}
}

func (m *Multiplexer) d_help() {
	lines := []string{
		"POD MANAGEMENT",
		"  new | n           Create a new Pod (interactive)",
		"  new <pod> <n>     Create/Join <n> containers to Pod.",
		"  list | l          List all Pods, health, and forwards",
		"  list <pod>        List details for a specific Pod",
		"  kill | k <id>     Kill a specific container",
		"  kill | k <pod>    Kill a Pod and all containers inside of it",
		"",
		"INTERACTION",
		"  attach | a <id>   Connect TTY to container (e.g., a a1)",
		"  Ctrl+X            Detach from current container",
		"  run | r           Run batch commands from HOST to container",
		"  <id> <command>    - Id is optional: (r a1 ls | r ls)",
		"  clear | c         Clear the terminal screen",
		"",
		"NETWORKING",
		"  forward | f <id> <port>  Map host port to Pod port",
		"  (e.g., f a1 3000)        ➜ localhost:3000 -> Pod A:3000",
		"",
		"EXECUTION",
		"  :<id> <cmd>       Run command in container (e.g., :a1 id)",
		"  :* <cmd>          Broadcast command to ALL containers",
		"  :!<id> <cmd>      Broadcast to all EXCEPT <id>",
		"",
		"SYSTEM",
		"  help | h          Show this menu",
		"  exit | bye        Shutdown all pods and exit supervisor",
	}

	lib.DrawBox("BCTOR COMMAND REFERENCE", lines)
}

func (m *Multiplexer) d_attach(input string) {
	parts := strings.Fields(input)

	// check input
	if len(parts) < 2 {
		fmt.Println("\r\n[-] Usage: attach <id> (e.g., attach a1)")
		m.RefreshPrompt()
		return
	}

	targetID := parts[1] // b3 or bctor-b3
	if !strings.HasPrefix(targetID, "bctor-") {
		targetID = "bctor-" + targetID
	}

	// check if alive
	m.state.scx.Mu.Lock()
	container, exists := m.state.containers[targetID]
	m.state.scx.Mu.Unlock()

	if !exists || container == nil {
		fmt.Printf("\r\n[-] Error: Container %s does not exist.\r\n", targetID)
		m.RefreshPrompt()
		return
	}

	// check lifecycle
	if container.State == ContainerExited || container.State == ContainerStopped {
		fmt.Printf("\r\n[-] Error: Cannot attach to %s (Status: %v).\r\n", targetID, container.State)
		m.RefreshPrompt()
		return
	}

	// check pty
	m.mu.Lock()
	_, hasTarget := m.targets[targetID]
	if hasTarget {
		m.activeID = targetID
		m.lineBuf = nil
	}
	m.mu.Unlock()

	if hasTarget {
		fmt.Print("\r\x1b[K")
		fmt.Printf("[!] Attached to %s. (Ctrl+X to detach)\r\n", targetID)
	} else {
		fmt.Printf("\r\n[-] Error: Container %s is alive but PTY is missing.\r\n", targetID)
		m.RefreshPrompt()
	}
}

func (m *Multiplexer) d_exec(input string) {

	parts := strings.SplitN(input[1:], " ", 2)
	if len(parts) != 2 {
		fmt.Printf("\r\n[-] Usage: :<id> <command>\r\n")
		return
	}
	targetID, cmd := parts[0], strings.TrimSpace(parts[1])

	m.mu.Lock()
	pendingExecs := make(map[string]*Target)

	if targetID == "*" {
		for id, t := range m.targets {
			pendingExecs[id] = t
		}
	} else if strings.HasPrefix(targetID, "!") {
		exclude := "bctor-" + strings.TrimPrefix(targetID, "!")
		for id, t := range m.targets {
			if id != exclude {
				pendingExecs[id] = t
			}
		}
	} else {
		fullID := targetID
		if !strings.HasPrefix(fullID, "bctor-") {
			fullID = "bctor-" + targetID
		}
		if t, ok := m.targets[fullID]; ok {
			pendingExecs[fullID] = t
		} else {
			fmt.Printf("\r\n[-] Unknown container: %s\r\n", fullID)
			m.mu.Unlock()
			return
		}
	}
	m.mu.Unlock()

	for id, target := range pendingExecs {
		m.state.scx.Mu.Lock()
		container, exists := m.state.containers[id]

		// check if alive
		if !exists || container == nil || container.State == ContainerExited {
			m.state.scx.Mu.Unlock()
			if len(pendingExecs) == 1 {
				fmt.Printf("\r\n[-] Cannot exec: %s is not running.\r\n", id)
			}
			continue
		}

		// final check
		if err := syscall.Kill(container.WorkloadPID, 0); err != nil {
			container.State = ContainerExited // sync
			m.state.scx.Mu.Unlock()
			continue
		}
		m.state.scx.Mu.Unlock()

		m.execOne(id, target, cmd)
	}

	m.RefreshPrompt()
}

func (m *Multiplexer) execOne(targetID string, target *Target, cmd string) {
	execCmd := exec.Command(
		"nsenter",
		"-t", strconv.Itoa(target.PID),
		"-m", "-u", "-i", "-n", "-p",
		"sh", "-c", cmd,
	)

	out, _ := execCmd.CombinedOutput()

	var lines []string

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
		if line != "" {
			lines = append(lines, line)
		}
	}

	lib.DrawBox(
		fmt.Sprintf("EXEC: %s (PID: %d)", targetID, target.PID),
		lines,
	)
}

func splitContainerID(id string) (pod string, index string, ok bool) {
	if !strings.HasPrefix(id, "bctor-") {
		return "", "", false
	}

	rest := strings.TrimPrefix(id, "bctor-")
	if len(rest) < 2 {
		return "", "", false
	}

	i := 0
	for i < len(rest) && rest[i] >= 'a' && rest[i] <= 'z' {
		i++
	}
	if i == 0 || i == len(rest) {
		return "", "", false
	}
	for j := i; j < len(rest); j++ {
		if rest[j] < '0' || rest[j] > '9' {
			return "", "", false
		}
	}

	return rest[:i], rest[i:], true
}

func podWouldBeEmptyAfterRemoving(podName, removeID string, containers map[string]*Container) bool {
	tmp := make(map[string]*Container, len(containers))
	for id, c := range containers {
		if id == removeID {
			continue
		}
		tmp[id] = c
	}
	return isPodEmpty(podName, tmp)
}
