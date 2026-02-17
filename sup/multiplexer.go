package sup

import (
	"fmt"
	"os"
	"strings"
	"sync"

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
	state    *appState
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

func NewMultiplexer(s *appState) *Multiplexer {
	return &Multiplexer{
		state:   s,
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
