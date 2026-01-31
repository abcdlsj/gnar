package logger

import (
	"os"
	"strings"

	"github.com/charmbracelet/log"
)

var defaultLogger *log.Logger

func init() {
	defaultLogger = log.NewWithOptions(os.Stderr, log.Options{
		ReportTimestamp: true,
	})

	if val := os.Getenv("LOG_LEVEL"); val != "" {
		SetLevel(parseLevel(val))
	}
}

func parseLevel(s string) log.Level {
	switch strings.ToLower(s) {
	case "debug":
		return log.DebugLevel
	case "info":
		return log.InfoLevel
	case "warn":
		return log.WarnLevel
	case "error":
		return log.ErrorLevel
	case "fatal":
		return log.FatalLevel
	default:
		return log.InfoLevel
	}
}

func SetLevel(level log.Level) {
	defaultLogger.SetLevel(level)
}

func SetDebug() {
	defaultLogger.SetLevel(log.DebugLevel)
}

func With(keyvals ...interface{}) *log.Logger {
	return defaultLogger.With(keyvals...)
}

func Debug(msg interface{}, keyvals ...interface{}) {
	defaultLogger.Debug(msg, keyvals...)
}

func Debugf(format string, v ...interface{}) {
	defaultLogger.Debugf(format, v...)
}

func Info(msg interface{}, keyvals ...interface{}) {
	defaultLogger.Info(msg, keyvals...)
}

func Infof(format string, v ...interface{}) {
	defaultLogger.Infof(format, v...)
}

func Warn(msg interface{}, keyvals ...interface{}) {
	defaultLogger.Warn(msg, keyvals...)
}

func Warnf(format string, v ...interface{}) {
	defaultLogger.Warnf(format, v...)
}

func Error(msg interface{}, keyvals ...interface{}) {
	defaultLogger.Error(msg, keyvals...)
}

func Errorf(format string, v ...interface{}) {
	defaultLogger.Errorf(format, v...)
}

func Fatal(msg interface{}, keyvals ...interface{}) {
	defaultLogger.Fatal(msg, keyvals...)
}

func Fatalf(format string, v ...interface{}) {
	defaultLogger.Fatalf(format, v...)
}
