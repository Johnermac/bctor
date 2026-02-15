package lib

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

type LogType int

const (
	TypeInfo LogType = iota
	TypeSuccess
	TypeWarn
	TypeError
	TypeContainer // For the actual workload output
)

type LogMsg struct {
	ContainerID string
	Data        string
	Type        LogType // Use this to determine styling
	IsHeader    bool
	IsFooter    bool
}

// Cores ANSI
const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
	Gray   = "\033[90m"
)

var LogMu sync.Mutex
var GlobalLogChan chan LogMsg

func LogInfo(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	if GlobalLogChan != nil {
		GlobalLogChan <- LogMsg{Data: msg, Type: TypeInfo}
	} else {
		// fallback
		fmt.Printf("\033[36mINFO:\033[0m %s\r\n", msg)
	}
}

func LogSuccess(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	if GlobalLogChan != nil {
		GlobalLogChan <- LogMsg{Data: msg, Type: TypeSuccess}
	} else {
		fmt.Printf("%sSUCCESS:%s %s\r\n", Green, Reset, msg)
	}
}

// lib/logger.go
func LogWarn(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)

	// Safety check
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("\033[90mWARN:\033[0m %s (logger closed)\r\n", msg)
		}
	}()

	if GlobalLogChan != nil {
		GlobalLogChan <- LogMsg{Data: msg, Type: TypeWarn}
	} else {
		fmt.Printf("\033[90mWARN:\033[0m %s\r\n", msg)
	}
}

func LogError(format string, a ...interface{}) error {
	msg := fmt.Sprintf(format, a...)
	if GlobalLogChan != nil {
		GlobalLogChan <- LogMsg{Data: msg, Type: TypeError}
	} else {
		fmt.Fprintf(os.Stderr, "%sERROR:%s %s\r\n", Red, Reset, msg)
	}
	return fmt.Errorf(msg)
}

func StartGlobalLogger(
	logChan chan LogMsg,
	done chan bool,
	mtx *Multiplexer,
) {
	go func() {

		buffers := make(map[string][]string)

		for msg := range logChan {

			isAttached := mtx.GetActiveID() == msg.ContainerID

			switch msg.Type {

			case TypeContainer:

				// ----- BATCH HEADER -----
				if msg.IsHeader {
					buffers[msg.ContainerID] = []string{}
					continue
				}

				// ----- BATCH FOOTER -----
				if msg.IsFooter {
					lines := buffers[msg.ContainerID]
					delete(buffers, msg.ContainerID)

					fmt.Print("\r\x1b[K")

					fmt.Printf("%s┌──────────────────────────────────────────────────┐%s\r\n", Cyan, Reset)
					fmt.Printf("%s│ EXEC: %-42s │%s\r\n", Cyan, msg.ContainerID, Reset)
					fmt.Printf("%s├──────────────────────────────────────────────────┤%s\r\n", Cyan, Reset)

					if len(lines) == 0 {
						fmt.Printf("%s│ %-48s │%s\r\n", Cyan, "(no output)", Reset)
					}

					for _, line := range lines {
						if len(line) > 48 {
							line = line[:45] + "..."
						}
						fmt.Printf("%s│%s %-48s %s│%s\r\n",
							Cyan, Reset, line, Cyan, Reset)
					}

					fmt.Printf("%s└──────────────────────────────────────────────────┘%s\r\n", Cyan, Reset)

					if mtx.GetActiveID() == "" {
						mtx.RefreshPrompt()
					}

					continue
				}

				// ----- BATCH BUFFERING -----
				if _, isBatch := buffers[msg.ContainerID]; isBatch {
					clean := strings.TrimRight(msg.Data, "\r")
					buffers[msg.ContainerID] = append(buffers[msg.ContainerID], clean)
					continue
				}

				// ----- INTERACTIVE -----
				if isAttached {
					// raw passthrough
					os.Stdout.Write([]byte(msg.Data))
				} else {
					// supervisor prefixed output
					fmt.Printf("\r\x1b[K%s[%s]%s %s\r\n",
						Cyan, msg.ContainerID, Reset, msg.Data)

					mtx.RefreshPrompt()
				}

			default:

				prefix := ""
				switch msg.Type {
				case TypeSuccess:
					prefix = Green + "SUCCESS:" + Reset
				case TypeWarn:
					prefix = Yellow + "WARN:" + Reset
				case TypeError:
					prefix = Red + "ERROR:" + Reset
				case TypeInfo:
					prefix = Cyan + "INFO:" + Reset
				}

				fmt.Printf("\r\x1b[K%s %s\r\n", prefix, msg.Data)

				if mtx.GetActiveID() == "" {
					mtx.RefreshPrompt()
				}
			}
		}

		done <- true
	}()
}

func CaptureLogs(
	id string,
	readFd int,
	writeFd int,
	mode ExecutionMode,
	logChan chan<- LogMsg,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	// Close write end in supervisor
	unix.Close(writeFd)

	f := os.NewFile(uintptr(readFd), "container-log")
	defer f.Close()

	if mode == ModeBatch {
		logChan <- LogMsg{ContainerID: id, Type: TypeContainer, IsHeader: true}
	}

	buf := make([]byte, 32*1024)
	var remainder string

	for {
		n, err := f.Read(buf)
		if n > 0 {
			chunk := remainder + string(buf[:n])
			lines := strings.Split(chunk, "\n")

			for i := 0; i < len(lines)-1; i++ {
				line := strings.TrimRight(lines[i], "\r")
				logChan <- LogMsg{
					ContainerID: id,
					Data:        line,
					Type:        TypeContainer,
				}
			}

			remainder = lines[len(lines)-1]
		}

		if err != nil {
			break
		}
	}

	if strings.TrimSpace(remainder) != "" {
		logChan <- LogMsg{
			ContainerID: id,
			Data:        strings.TrimRight(remainder, "\r"),
			Type:        TypeContainer,
		}
	}

	if mode == ModeBatch {
		logChan <- LogMsg{ContainerID: id, Type: TypeContainer, IsFooter: true}
	}
}
