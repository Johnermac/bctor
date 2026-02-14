package lib

import (
	"bufio"
	"fmt"
	"os"
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
		fmt.Printf("\033[36mINFO:\033[0m %s\n", msg)
	}
}

func LogSuccess(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	if GlobalLogChan != nil {
		GlobalLogChan <- LogMsg{Data: msg, Type: TypeSuccess}
	} else {
		fmt.Printf("%sSUCCESS:%s %s\n", Green, Reset, msg)
	}
}

// lib/logger.go
func LogWarn(format string, a ...interface{}) {
    msg := fmt.Sprintf(format, a...)
    
    // Safety check
    defer func() {
        if r := recover(); r != nil {
            fmt.Printf("\033[90mWARN:\033[0m %s (logger closed)\n", msg)
        }
    }()

    if GlobalLogChan != nil {
        GlobalLogChan <- LogMsg{Data: msg, Type: TypeWarn}
    } else {
        fmt.Printf("\033[90mWARN:\033[0m %s\n", msg)
    }
}

func LogError(format string, a ...interface{}) error {
	msg := fmt.Sprintf(format, a...)
	if GlobalLogChan != nil {
		GlobalLogChan <- LogMsg{Data: msg, Type: TypeError}
	} else {
		fmt.Fprintf(os.Stderr, "%sERROR:%s %s\n", Red, Reset, msg)
	}
	return fmt.Errorf(msg)
}

func StartGlobalLogger(logChan chan LogMsg, done chan bool) {
    go func() {
        // storage: ContainerID -> list of lines
        buffers := make(map[string][]string)

        for msg := range logChan {
            switch msg.Type {
            case TypeContainer:
                if msg.IsHeader {
                    // Just initialize the buffer
                    buffers[msg.ContainerID] = []string{}
                } else if msg.IsFooter {
                    // NOW print the whole box at once!
                    lines := buffers[msg.ContainerID]
                    fmt.Printf("\n%s╔═ START: %s ═════════════════════════════════%s\n", Cyan, msg.ContainerID, Reset)
                    for _, line := range lines {
                        fmt.Printf("%s║ %s %s\n", Cyan, Reset, line)
                    }
                    fmt.Printf("%s╚══════════════════════════════════════════════════╝%s\n", Cyan, Reset)
                    delete(buffers, msg.ContainerID)
                } else {
                    // Just store the line for later
                    buffers[msg.ContainerID] = append(buffers[msg.ContainerID], msg.Data)
                }

            default:
                // Info/Warn/Success don't need buffering
                prefix := ""
                switch msg.Type {
                case TypeSuccess: prefix = Green + "SUCCESS:" + Reset
                case TypeWarn:    prefix = Gray + "WARN:" + Reset
                case TypeError:   prefix = Red + "ERROR:" + Reset
                case TypeInfo:    prefix = Cyan + "INFO:" + Reset
                }
                fmt.Printf("%s %s\n", prefix, msg.Data)
            }
        }
				done <- true
    }()
		
}

func CaptureLogs(id string, readFd int, writeFd int, logChan chan<- LogMsg, wg *sync.WaitGroup) {
    defer wg.Done()    
    unix.Close(writeFd) 

    f := os.NewFile(uintptr(readFd), "container-log")
    defer f.Close()

    logChan <- LogMsg{ContainerID: id, Type: TypeContainer, IsHeader: true}
    
    reader := bufio.NewReader(f)
    for {
        line, err := reader.ReadString('\n')
        if len(line) > 0 {            
            logChan <- LogMsg{
                ContainerID: id,
                Data:        line[:len(line)-1],
                Type:        TypeContainer,
            }
        }
        if err != nil {
            break // EOF or other error
        }
    }

    logChan <- LogMsg{ContainerID: id, Type: TypeContainer, IsFooter: true}
}
