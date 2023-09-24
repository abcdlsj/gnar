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

func init() {
	if os.Getenv("DEBUG") != "" {
		SetLevel(DEBUG)
	}
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
		log.Default().Printf(DEBUG_PREFIX+format, v...)
	}
}

func InfoF(format string, v ...interface{}) {
	if globalLevel <= INFO {
		log.Default().Printf(INFO_PREFIX+format, v...)
	}
}

func WarnF(format string, v ...interface{}) {
	if globalLevel <= WARN {
		log.Default().Printf(WARN_PREFIX+format, v...)
	}
}

func ErrorF(format string, v ...interface{}) {
	if globalLevel <= ERROR {
		log.Default().Printf(ERROR_PREFIX+format, v...)
	}
}

func Debug(v ...interface{}) {
	if globalLevel <= DEBUG {
		v = append([]interface{}{DEBUG_PREFIX}, v...)
		log.Default().Print(v...)
	}
}

func Info(v ...interface{}) {
	if globalLevel <= INFO {
		v = append([]interface{}{INFO_PREFIX}, v...)
		log.Default().Print(v...)
	}
}

func Warn(v ...interface{}) {
	if globalLevel <= WARN {
		v = append([]interface{}{WARN_PREFIX}, v...)
		log.Default().Print(v...)
	}
}

func Error(v ...interface{}) {
	if globalLevel <= ERROR {
		v = append([]interface{}{ERROR_PREFIX}, v...)
		log.Default().Print(v...)
	}
}

func FatalF(format string, v ...interface{}) {
	log.Default().Fatalf(FATAL_PREFIX+format, v...)
}

func Fatal(v ...interface{}) {
	v = append([]interface{}{FATAL_PREFIX}, v...)
	log.Default().Fatal(v...)
}
