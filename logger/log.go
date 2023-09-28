package logger

import (
	"log"
	"os"

	"github.com/abcdlsj/cr"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

var defaultLogger *log.Logger

func init() {
	if os.Getenv("DEBUG") != "" {
		SetLevel(DEBUG)
	}

	defaultLogger = log.New(os.Stderr, "", log.LstdFlags)
}

var (
	DEBUG_PREFIX = cr.PCyan("DBG") + " "
	INFO_PREFIX  = cr.PGreen("INF") + " "
	WARN_PREFIX  = cr.PYellow("WAR") + " "
	ERROR_PREFIX = cr.PRed("ERR") + " "
	FATAL_PREFIX = cr.PRedBgWhite("FAT") + " "
)

var globalLevel = INFO

func SetLevel(level Level) {
	globalLevel = level
}

func DebugF(format string, v ...interface{}) {
	if globalLevel <= DEBUG {
		defaultLogger.Printf(DEBUG_PREFIX+format, v...)
	}
}

func InfoF(format string, v ...interface{}) {
	if globalLevel <= INFO {
		defaultLogger.Printf(INFO_PREFIX+format, v...)
	}
}

func WarnF(format string, v ...interface{}) {
	if globalLevel <= WARN {
		defaultLogger.Printf(WARN_PREFIX+format, v...)
	}
}

func ErrorF(format string, v ...interface{}) {
	if globalLevel <= ERROR {
		defaultLogger.Printf(ERROR_PREFIX+format, v...)
	}
}

func Debug(v ...interface{}) {
	if globalLevel <= DEBUG {
		v = append([]interface{}{DEBUG_PREFIX}, v...)
		defaultLogger.Print(v...)
	}
}

func Info(v ...interface{}) {
	if globalLevel <= INFO {
		v = append([]interface{}{INFO_PREFIX}, v...)
		defaultLogger.Print(v...)
	}
}

func Warn(v ...interface{}) {
	if globalLevel <= WARN {
		v = append([]interface{}{WARN_PREFIX}, v...)
		defaultLogger.Print(v...)
	}
}

func Error(v ...interface{}) {
	if globalLevel <= ERROR {
		v = append([]interface{}{ERROR_PREFIX}, v...)
		defaultLogger.Print(v...)
	}
}

func FatalF(format string, v ...interface{}) {
	defaultLogger.Fatalf(FATAL_PREFIX+format, v...)
}

func Fatal(v ...interface{}) {
	v = append([]interface{}{FATAL_PREFIX}, v...)
	defaultLogger.Fatal(v...)
}
