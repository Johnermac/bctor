package lib

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

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

type MuxLogger interface {
	GetActiveID() string
	RefreshPrompt()
}

func StartGlobalLogger(
	logChan chan LogMsg,
	done chan bool,
	mtx MuxLogger,
) {
	go func() {
		for msg := range logChan {
			isAttached := mtx.GetActiveID() == msg.ContainerID
			switch msg.Type {
			case TypeContainer:
				// Batch markers are ignored in live streaming mode.
				if msg.IsHeader || msg.IsFooter {
					continue
				}

				if isAttached {
					os.Stdout.Write([]byte(msg.Data))
				} else {
					fmt.Print("\r\x1b[K")
					fmt.Printf("%s %s %s\r\n",
						"\x1b[0m",
						"\x1b[36m\x1b[1m│\x1b[0m",
						msg.Data)

					if mtx.GetActiveID() == "" {
						mtx.RefreshPrompt()
					}
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

func DrawBox(title string, lines []string) {
	minWidth := 50
	maxWidth := 100

	// 1. Calculate innerWidth using Runes, not Bytes
	innerWidth := utf8.RuneCountInString(StripANSI(title))

	for _, l := range lines {
		visLen := utf8.RuneCountInString(StripANSI(l)) // FIX: Use Runes here
		if visLen > innerWidth {
			innerWidth = visLen
		}
	}

	if innerWidth < minWidth {
		innerWidth = minWidth
	}
	if innerWidth > maxWidth {
		innerWidth = maxWidth
	}

	fmt.Print("\r\x1b[K")
	hline := strings.Repeat("─", innerWidth+2)

	// Header
	fmt.Printf("\r%s┌%s┐%s\n", Cyan, hline, Reset)
	// Use the %-*s with stripANSI(title) is okay because title usually has no dots/emojis
	fmt.Printf("\r%s│ %-*s │%s\n", Cyan, innerWidth, StripANSI(title), Reset)
	fmt.Printf("\r%s├%s┤%s\n", Cyan, hline, Reset)

	if len(lines) == 0 {
		fmt.Printf("\r%s│ %-*s │%s\n", Cyan, innerWidth, "(no data)", Reset)
	} else {
		for _, line := range lines {
			clean := strings.ReplaceAll(line, "\r", "")
			visible := StripANSI(clean)
			visLen := utf8.RuneCountInString(visible)

			displayLine := clean
			// FIX: Comparison should also be Rune-based
			if visLen > innerWidth {
				// Truncating Unicode is tricky; this is a safe way to do it
				runes := []rune(visible)
				displayLine = string(runes[:innerWidth-3]) + "..."
				visLen = innerWidth // Reset visLen for padding math
			}

			padding := innerWidth - visLen
			if padding < 0 {
				padding = 0
			}

			// Print: notice the extra space after displayLine for consistent "gutter"
			fmt.Printf("\r%s│%s %s%s %s│%s\n",
				Cyan, Reset, displayLine, strings.Repeat(" ", padding), Cyan, Reset)
		}
	}

	fmt.Printf("\r%s└%s┘%s\n", Cyan, hline, Reset)
}

func StripANSI(s string) string {
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansi.ReplaceAllString(s, "")
}
