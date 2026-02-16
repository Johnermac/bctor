package lib

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/term"
)

type Target struct {
	PTY *os.File
	PID int
}

type Multiplexer struct {
	targets  map[string]*Target
	activeID string
	mu       sync.Mutex
	lineBuf  []byte
}

func (m *Multiplexer) write(id string, data []byte) {
	m.mu.Lock()
	target, ok := m.targets[id]
	m.mu.Unlock()

	if !ok || target == nil || target.PTY == nil {
		return
	}

	normalized := make([]byte, len(data))
	copy(normalized, data)
	for i, b := range normalized {
		if b == '\r' {
			normalized[i] = '\n'
		}
	}

	n, err := target.PTY.Write(normalized)

	if err != nil {
		fmt.Printf("\r\n[!] Write Error [%s]: %v\r\n", id, err)
		return
	}

	if n != len(normalized) {
		fmt.Printf("\r\n[!] Partial Write [%s]: wrote=%d expected=%d\r\n",
			id, n, len(normalized))
	}
}

func (m *Multiplexer) Register(id string, ptyMaster *os.File, pid int) {
	m.mu.Lock()
	m.targets[id] = &Target{
		PTY: ptyMaster,
		PID: pid,
	}
	m.mu.Unlock()
	go m.pipeOutput(id)
}

func (m *Multiplexer) pipeOutput(id string) {
	m.mu.Lock()
	target, ok := m.targets[id]
	m.mu.Unlock()
	if !ok {
		return
	}

	buf := make([]byte, 4096)

	for {
		n, err := target.PTY.Read(buf)
		if err != nil {
			return
		}

		m.mu.Lock()
		active := m.activeID == id
		m.mu.Unlock()

		if active {
			os.Stdout.Write(buf[:n])
		}
	}
}

func (m *Multiplexer) RefreshPrompt() {
	fmt.Printf("\r\x1b[Kbctor ❯ %s", string(m.lineBuf))
}

func NewMultiplexer() *Multiplexer {
	return &Multiplexer{
		targets: make(map[string]*Target),
	}
}

func (m *Multiplexer) GetActiveID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeID
}

func (m *Multiplexer) RunLoop() {
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Println("Error setting raw mode:", err)
		return
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	buf := make([]byte, 4096)

	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			break
		}

		m.mu.Lock()
		active := m.activeID
		m.mu.Unlock()

		if active != "" {
			if n == 1 && buf[0] == 24 {
				m.mu.Lock()
				m.activeID = ""
				m.mu.Unlock()

				fmt.Print("\r\x1b[K\r\n[!] Detached. Back to Supervisor.\r\n")
				m.RefreshPrompt()
				continue
			}

			m.write(active, buf[:n])
			continue
		}

		m.handleSupervisorInput(buf[:n])
	}
}

func (m *Multiplexer) handleSupervisorInput(input []byte) {
	for _, b := range input {
		switch b {
		case 3: // Ctrl+C
			fmt.Print("^C\r\n")
		case 13, 10: // Enter
			line := strings.TrimSpace(string(m.lineBuf))
			m.lineBuf = nil
			fmt.Print("\r\n")

			if line != "" {
				m.Dispatch(line)
			}

			m.mu.Lock()
			active := m.activeID
			m.mu.Unlock()

			if active == "" {
				m.RefreshPrompt()
				return
			} else {
				m.write(active, []byte("\r"))
				return
			}

		case 127, 8: // Backspace
			if len(m.lineBuf) > 0 {
				m.lineBuf = m.lineBuf[:len(m.lineBuf)-1]
				fmt.Print("\b \b")
			}
		default:
			m.lineBuf = append(m.lineBuf, b)
			fmt.Printf("%c", b)
		}
	}
}

func (m *Multiplexer) Dispatch(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}

	switch {
	case input == "list":
		m.d_list()
	case strings.HasPrefix(input, ":"):
		m.d_exec(input)
	case strings.HasPrefix(input, "attach "):
		m.d_attach(input)	
	default:
		fmt.Printf("\r\n[-] Unknown command: %s\r\n", input)
		m.RefreshPrompt()
	}
}

func (m *Multiplexer) d_list() {
	fmt.Printf("\r\n--- Container Status ---\r\n")
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, t := range m.targets {
		process, err := os.FindProcess(t.PID)
		status := fmt.Sprintf("%sALIVE%s", Green, Reset)

		if err != nil || process.Signal(syscall.Signal(0)) != nil {
			status = fmt.Sprintf("%sDEAD%s", Red, Reset)
		}
		fmt.Printf("  %-15s (PID: %d) [%s]\r\n", id, t.PID, status)
	}
	fmt.Printf("\r\n")
}

func (m *Multiplexer) d_attach(input string) {
	targetID := strings.TrimSpace(input[7:])
	if !strings.HasPrefix(targetID, "bctor-") {
		targetID = "bctor-c" + targetID
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
				if id == "bctor-c"+allExcept {
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
		targetID = "bctor-c" + targetID
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

    fmt.Print("\r\x1b[K")

    title := fmt.Sprintf("EXEC: %s (PID: %d)", targetID, target.PID)

    fmt.Printf("\r%s┌──────────────────────────────────────────────────┐%s\r\n", Cyan, Reset)
    fmt.Printf("\r%s│ %-48s │%s\r\n", Cyan, title, Reset)
    fmt.Printf("\r%s├──────────────────────────────────────────────────┤%s\r\n", Cyan, Reset)

    outputStr := string(out)
    lines := strings.Split(outputStr, "\n")

    hasOutput := false
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }
        hasOutput = true

        if len(line) > 48 {
            line = line[:45] + "..."
        }

        fmt.Printf("\r%s│%s %-48s %s│%s\r\n", Cyan, Reset, line, Cyan, Reset)
    }

    if !hasOutput {
        fmt.Printf("\r%s│ %-48s │%s\r\n", Cyan, "(no output)", Reset)
    }

    fmt.Printf("\r%s└──────────────────────────────────────────────────┘%s\r\n", Cyan, Reset)
}
