package logger

import (
	"fmt"
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

func (l Level) String() string {
	switch l {
	case DEBUG:
		return cr.PCyan("DBG")
	case INFO:
		return cr.PGreen("INF")
	case WARN:
		return cr.PYellow("WAR")
	case ERROR:
		return cr.PRed("ERR")
	case FATAL:
		return cr.PRedBgWhite("FAT")
	}

	return cr.PLBlack("???")
}

type Logger struct {
	prefix string
	logger *log.Logger
}

func New(prefix string) *Logger {
	return &Logger{
		prefix: prefix,
		logger: log.New(os.Stderr, "", log.LstdFlags),
	}
}

var DefaultLogger *Logger

func init() {
	if os.Getenv("DEBUG") != "" {
		SetLevel(DEBUG)
	}

	DefaultLogger = New("")
}

var globalLevel = INFO

func SetLevel(level Level) {
	globalLevel = level
}

func Prefix(prefix string, level Level) string {
	if prefix == "" {
		return fmt.Sprintf("%s ", level)
	}

	return fmt.Sprintf("%s %s ", level, cr.PLBlue(prefix))
}

func Printf(logger *log.Logger, prefix string, level Level, format string, v ...interface{}) {
	if globalLevel <= level {
		logger.Printf(Prefix(prefix, level)+format, v...)
	}
}

func Print(logger *log.Logger, prefix string, level Level, v ...interface{}) {
	if globalLevel <= level {
		logger.Print(Prefix(prefix, level) + fmt.Sprintln(v...))
	}
}

func DebugF(format string, v ...interface{}) {
	LDebugF(DefaultLogger, format, v...)
}

func LDebugF(logger *Logger, format string, v ...interface{}) {
	Printf(logger.logger, logger.prefix, DEBUG, format, v...)
}

func InfoF(format string, v ...interface{}) {
	LInfoF(DefaultLogger, format, v...)
}

func LInfoF(logger *Logger, format string, v ...interface{}) {
	Printf(logger.logger, logger.prefix, INFO, format, v...)
}

func WarnF(format string, v ...interface{}) {
	LWarnF(DefaultLogger, format, v...)
}

func LWarnF(logger *Logger, format string, v ...interface{}) {
	Printf(logger.logger, logger.prefix, WARN, format, v...)
}

func ErrorF(format string, v ...interface{}) {
	LErrorF(DefaultLogger, format, v...)
}

func LErrorF(logger *Logger, format string, v ...interface{}) {
	Printf(logger.logger, logger.prefix, ERROR, format, v...)
}

func Debug(v ...interface{}) {
	LDebug(DefaultLogger, v...)
}

func LDebug(logger *Logger, v ...interface{}) {
	Print(logger.logger, logger.prefix, DEBUG, v...)
}

func Info(v ...interface{}) {
	LInfo(DefaultLogger, v...)
}

func LInfo(logger *Logger, v ...interface{}) {
	Print(logger.logger, logger.prefix, INFO, v...)
}

func Warn(v ...interface{}) {
	Lwarn(DefaultLogger, v...)
}

func Lwarn(logger *Logger, v ...interface{}) {
	Print(logger.logger, logger.prefix, WARN, v...)
}

func Error(v ...interface{}) {
	LError(DefaultLogger, v...)
}

func LError(logger *Logger, v ...interface{}) {
	Print(logger.logger, logger.prefix, ERROR, v...)
}

func FatalF(format string, v ...interface{}) {
	LFatalF(DefaultLogger, format, v...)
}

func LFatalF(logger *Logger, format string, v ...interface{}) {
	Printf(logger.logger, logger.prefix, FATAL, format, v...)
}

func Fatal(v ...interface{}) {
	LFatal(DefaultLogger, v...)
}

func LFatal(logger *Logger, v ...interface{}) {
	Print(logger.logger, logger.prefix, FATAL, v...)
}

func NewPrefixLogger(prefix string) *log.Logger {
	return log.New(os.Stderr, prefix, log.LstdFlags)
}
