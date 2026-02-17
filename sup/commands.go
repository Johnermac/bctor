package sup

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/Johnermac/bctor/lib"
)

// menu of commands
func (m *Multiplexer) Dispatch(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}

	switch {
	case input == "list" || strings.HasPrefix(input, "list "):
		m.d_list(input)
	case strings.HasPrefix(input, ":"):
		m.d_exec(input)
	case strings.HasPrefix(input, "attach "):
		m.d_attach(input)
	case input == "new" || strings.HasPrefix(input, "new "):
		m.d_new(input)
	case input == "help":
		m.d_help()
	default:
		fmt.Printf("\r\n[-] Unknown command: %s\r\n", input)
		m.RefreshPrompt()
	}
}


func (m *Multiplexer) d_new(input string) {
    lines := []string{}
    parts := strings.Fields(input)

    // CASE 1: "new" (Creator)
    if len(parts) == 1 {        
				ipc, _ := lib.NewIPC()
				letter, _ := m.state.GetNextPodLetter()					
				c, err := StartCreator(letter, lib.ModeInteractive, lib.ProfileDebugShell, m.state, ipc)
				if err != nil {
						lines = append(lines, "[-] Start Creator failed")
				} else {
						// Update state map
						m.mu.Lock()
						m.state.containers[c.Spec.ID] = c
						m.mu.Unlock()		
									
						lines = append(lines, fmt.Sprintf("[+] Created Pod [%s]", letter))
				}
        

    // CASE 2: "new a" or "new a 2" (Joiner)
    } else if len(parts) >= 2 {
        letter := parts[1]
        count := 1

        // Argument parsing
        if len(parts) == 3 {
            if val, err := strconv.Atoi(parts[2]); err == nil && val > 0 {
                count = val
            } else {
                lines = append(lines, "[-] Invalid count, defaulting to 1")
            }
        }

        // Verify Root exists
        m.mu.Lock()
        rootID := fmt.Sprintf("bctor-%s1", letter)
        root, exists := m.state.containers[rootID]
        m.mu.Unlock()

        if !exists {
            lines = append(lines, fmt.Sprintf("[-] Error: Pod %s does not exist", letter))
        } else {
            for i := 0; i < count; i++ {
                ipc, _ := lib.NewIPC()
                
                // Get next number (e.g., 2)
                num := m.state.GetNextContainerIndex(letter)
                name := fmt.Sprintf("bctor-%s%d", letter, num)

                // IMPORTANT: We need a placeholder in the map immediately 
                // so the NEXT iteration of the loop sees this index as 'taken'
                m.mu.Lock()
                m.state.containers[name] = &Container{State: ContainerInitializing} 
                m.mu.Unlock()

                go StartJoiner(root, name, lib.ModeInteractive, lib.ProfileDebugShell, m.state, ipc)
                lines = append(lines, fmt.Sprintf("[+] Container [%s] joining Pod [%s]", name, letter))
            }
        }
    }

    // Final display
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
				lines = append(lines, fmt.Sprintf("Pod [%s] Containers:%d Alive:%d Dead:%d", pod, total, alive, dead))
			}
		}		
		lib.DrawBox("POD STATUS", lines)

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
			lines = append(lines, fmt.Sprintf("%-12s PID:%-6d [%s]", id, t.PID, status))
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
		"  new              Create a new Pod (Creator)",
		"  new <pod> <n>    Join <n> containers to Pod (default 1)",
		"  list             List all active Pods and IPs",
		"  list <pod>       List details for a specific Pod",
		"",
		"INTERACTION",
		"  attach <id>      Connect TTY to container (e.g., attach a1)",
		"  detach           Exit current container TTY (Ctrl+X)",
		"",
		"EXECUTION",
		"  :<id> <cmd>      Run command in one container (e.g., :a1 id)",
		"  :* <cmd>         Broadcast command to ALL containers",
		"  :!<id> <cmd>     Broadcast to all EXCEPT <id>",
		"",
		"SYSTEM",
		"  help             Show this menu",
		"  exit             Shutdown all pods and exit supervisor",
	}

	lib.DrawBox("BCTOR COMMAND REFERENCE", lines)
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

func (m *Multiplexer) d_attach(input string) {
	targetID := strings.TrimSpace(input[7:])
	if !strings.HasPrefix(targetID, "bctor-") {
		targetID = "bctor-" + targetID
	}

	m.mu.Lock()
	_, ok := m.targets[targetID]
	if ok {
		m.activeID = targetID
		m.lineBuf = nil
	}
	m.mu.Unlock()

	if ok {
		fmt.Print("\r\x1b[K")
		fmt.Printf("[!] Attached to %s. (Ctrl+X to detach)\r\n", targetID)
	} else {
		fmt.Printf("\r\n[-] Unknown container: %s\r\n", targetID)
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
	//fmt.Printf("parts: %v\n", parts)

	// BROADCAST COMMAND
	if targetID == "*" {
		m.mu.Lock()
		targetsCopy := make(map[string]*Target, len(m.targets))
		for id, t := range m.targets {
			targetsCopy[id] = t
		}
		m.mu.Unlock()

		for id, target := range targetsCopy {
			m.execOne(id, target, cmd)
		}

		// restore prompt
		m.mu.Lock()
		if m.activeID == "" {
			fmt.Print("\rbctor ❯ ")
		}
		m.mu.Unlock()

		return

	}

	// ALL EXCEPT
	if strings.HasPrefix(targetID, "!") {
		allExcept := strings.TrimPrefix(targetID, "!")

		m.mu.Lock()
		targetsCopy := make(map[string]*Target, len(m.targets)-1)
		for id, t := range m.targets {
			if id == "bctor-"+allExcept {
				continue
			}
			targetsCopy[id] = t
		}
		m.mu.Unlock()

		for id, target := range targetsCopy {
			m.execOne(id, target, cmd)
		}

		// restore prompt
		m.mu.Lock()
		if m.activeID == "" {
			fmt.Print("\rbctor ❯ ")
		}
		m.mu.Unlock()

		return
	}

	// ONE TARGET COMMAND
	if !strings.HasPrefix(targetID, "bctor-") {
		targetID = "bctor-" + targetID
	}

	m.mu.Lock()
	target, ok := m.targets[targetID]
	m.mu.Unlock()

	if !ok {
		fmt.Printf("\r\n[-] Unknown container: %s\r\n", targetID)
		return
	}

	m.execOne(targetID, target, cmd)

	// restore prompt
	m.mu.Lock()
	if m.activeID == "" {
		fmt.Print("\rbctor ❯ ")
	}
	m.mu.Unlock()
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
