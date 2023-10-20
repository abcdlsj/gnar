package logger

import (
	"fmt"
	"log"
	"os"
	"strings"

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
	prefixs []string
	logger  *log.Logger
}

func New(prefix string) *Logger {
	return &Logger{
		prefixs: []string{prefix},
		logger:  log.New(os.Stderr, "", log.LstdFlags),
	}
}

func (l *Logger) Add(prefix string) {
	l.prefixs = append(l.prefixs, prefix)
}

func (l *Logger) CloneAdd(prefix string) *Logger {
	return &Logger{
		prefixs: append(l.prefixs, prefix),
		logger:  log.New(os.Stderr, "", log.LstdFlags),
	}
}

var defalt *Logger

func init() {
	if os.Getenv("DEBUG") != "" {
		SetLevel(DEBUG)
	}

	defalt = New("[GLOBAL]")
}

var globalLevel = INFO

func SetLevel(level Level) {
	globalLevel = level
}

func header(prefixs []string, level Level) string {
	rainbow := []func(string) string{
		cr.PLYellow,
		cr.PLBlue,
		cr.PLGreen,
		cr.PLCyan,
		cr.PLMagenta,
	}

	apply := func(texts ...string) string {
		var sb strings.Builder
		for i, text := range texts {
			sb.WriteString(rainbow[i%len(rainbow)](text) + " ")
		}
		return sb.String()
	}

	if len(prefixs) == 0 {
		return fmt.Sprintf("%s ", level)
	}

	return fmt.Sprintf("%s %s", level, apply(prefixs...))
}

func builderf(logger *log.Logger, prefixs []string, level Level, format string, v ...interface{}) {
	if level == FATAL {
		logger.Fatalf(header(prefixs, level)+format, v...)
	}
	if globalLevel <= level {
		logger.Printf(header(prefixs, level)+format, v...)
	}
}

func builder(logger *log.Logger, prefixs []string, level Level, v ...interface{}) {
	if level == FATAL {
		logger.Fatalf(header(prefixs, level) + fmt.Sprintln(v...))
	}
	if globalLevel <= level {
		logger.Print(header(prefixs, level) + fmt.Sprintln(v...))
	}
}

func (l *Logger) Debugf(format string, v ...interface{}) {
	builderf(l.logger, l.prefixs, DEBUG, format, v...)
}

func (l *Logger) Infof(format string, v ...interface{}) {
	builderf(l.logger, l.prefixs, INFO, format, v...)
}

func (l *Logger) Warnf(format string, v ...interface{}) {
	builderf(l.logger, l.prefixs, WARN, format, v...)
}

func (l *Logger) Errorf(format string, v ...interface{}) {
	builderf(l.logger, l.prefixs, ERROR, format, v...)
}

func (l *Logger) Fatalf(format string, v ...interface{}) {
	builderf(l.logger, l.prefixs, FATAL, format, v...)
}

func (l *Logger) Debug(v ...interface{}) {
	builder(l.logger, l.prefixs, DEBUG, v...)
}

func (l *Logger) Info(v ...interface{}) {
	builder(l.logger, l.prefixs, INFO, v...)
}

func (l *Logger) Warn(v ...interface{}) {
	builder(l.logger, l.prefixs, WARN, v...)
}

func (l *Logger) Error(v ...interface{}) {
	builder(l.logger, l.prefixs, ERROR, v...)
}

func (l *Logger) Fatal(v ...interface{}) {
	builder(l.logger, l.prefixs, FATAL, v...)
}

func Debugf(format string, v ...interface{}) {
	defalt.Debugf(format, v...)
}

func Infof(format string, v ...interface{}) {
	defalt.Infof(format, v...)
}

func Warnf(format string, v ...interface{}) {
	defalt.Warnf(format, v...)
}

func Errorf(format string, v ...interface{}) {
	defalt.Errorf(format, v...)
}

func Debug(v ...interface{}) {
	defalt.Debug(v...)
}

func Info(v ...interface{}) {
	defalt.Info(v...)
}

func Warn(v ...interface{}) {
	defalt.Warn(v...)
}

func Error(v ...interface{}) {
	defalt.Error(v...)
}

func Fatalf(format string, v ...interface{}) {
	defalt.Fatalf(format, v...)
}

func Fatal(v ...interface{}) {
	defalt.Fatal(v...)
}
