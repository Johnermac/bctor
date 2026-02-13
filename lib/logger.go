package lib

import (
	"bufio"
	"fmt"
	"os"
	"sync"
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

func LogWarn(format string, a ...interface{}) {
    msg := fmt.Sprintf(format, a...)
    if GlobalLogChan != nil {
        GlobalLogChan <- LogMsg{Data: msg, Type: TypeWarn}
    } else {
        fmt.Printf("%sWARN:%s %s\n", Gray, Reset, msg)
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

func StartGlobalLogger(logChan chan LogMsg) {
    go func() {
        for msg := range logChan {
            switch msg.Type {
            case TypeSuccess:
                fmt.Printf("%sSUCCESS:%s %s\n", Green, Reset, msg.Data)
            case TypeWarn:
                fmt.Printf("%sWARN:%s %s\n", Gray, Reset, msg.Data)
            case TypeError:
                fmt.Fprintf(os.Stderr, "%sERROR:%s %s\n", Red, Reset, msg.Data)
            case TypeInfo:
                fmt.Printf("%sINFO:%s %s\n", Cyan, Reset, msg.Data)
            
            case TypeContainer:                
                if msg.IsHeader {
                    fmt.Printf("\n%s╔═ START: %s ═════════════════════════════════%s\n", Cyan, msg.ContainerID, Reset)
                } else if msg.IsFooter {
                    fmt.Printf("%s╚══════════════════════════════════════════════════╝%s\n", Cyan, Reset)
                } else {
                    fmt.Printf("%s║ %s %s\n", Cyan, Reset, msg.Data)
                }
            }
        }
    }()
}

func CaptureLogs(containerID string, readFd int, logChan chan LogMsg) {
    f := os.NewFile(uintptr(readFd), "container-log")
    defer f.Close()

    var lines []string
    scanner := bufio.NewScanner(f)
    for scanner.Scan() {
        lines = append(lines, scanner.Text())
    }

    // Send the atomic block
    logChan <- LogMsg{ContainerID: containerID, Type: TypeContainer, IsHeader: true}
    for _, line := range lines {
        logChan <- LogMsg{
            ContainerID: containerID,
            Data:        line,
            Type:        TypeContainer,
        }
    }
    logChan <- LogMsg{ContainerID: containerID, Type: TypeContainer, IsFooter: true}
}

