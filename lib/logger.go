package lib

import (
	"fmt"
	"os"
)

// Cores ANSI
const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
	Gray   = "\033[90m"
)

func LogInfo(format string, a ...interface{}) {

	prefix := fmt.Sprintf("%sINFO:%s ", Cyan, Reset)
	fmt.Printf(prefix+format+"\n", a...)
}

func LogSuccess(format string, a ...interface{}) {
	prefix := fmt.Sprintf("%sSUCCESS:%s ", Green, Reset)
	fmt.Printf(prefix+format+"\n", a...)
}

func LogError(format string, a ...interface{}) error {
	msg := fmt.Sprintf(format, a...)
	prefix := fmt.Sprintf("%sERROR:%s ", Red, Reset)
	fmt.Fprintf(os.Stderr, prefix+"%s\n", msg)
	return fmt.Errorf(msg)
}

func LogWarn(format string, a ...interface{}) {
	prefix := fmt.Sprintf("%sWARN:%s ", Yellow, Reset)
	fmt.Printf(prefix+format+"\n", a...)
}
