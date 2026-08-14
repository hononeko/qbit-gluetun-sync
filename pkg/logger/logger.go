package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
)

var internalLogger atomic.Pointer[slog.Logger]

func init() {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	defaultLogger := slog.New(slog.NewTextHandler(os.Stdout, opts))
	internalLogger.Store(defaultLogger)
}

// Init initializes the global logger with the specified level and format.
func Init(levelStr string) {
	InitWithFormat(levelStr, "text")
}

// InitWithFormat initializes the global logger with level and format ("text" or "json").
func InitWithFormat(levelStr, formatStr string) {
	InitWithWriter(levelStr, formatStr, os.Stdout)
}

// InitWithWriter initializes the global logger with level, format, and custom output writer (useful for tests).
func InitWithWriter(levelStr, formatStr string, w io.Writer) {
	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if strings.EqualFold(formatStr, "json") {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	internalLogger.Store(slog.New(handler))
}

func getDefault() *slog.Logger {
	l := internalLogger.Load()
	if l != nil {
		return l
	}
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	fallback := slog.New(slog.NewTextHandler(os.Stdout, opts))
	internalLogger.Store(fallback)
	return fallback
}

// Info logs an informational message.
func Info(msg string, args ...any) {
	getDefault().Info(msg, args...)
}

// Warn logs a warning message.
func Warn(msg string, args ...any) {
	getDefault().Warn(msg, args...)
}

// Error logs an error message.
func Error(msg string, args ...any) {
	getDefault().Error(msg, args...)
}

// Debug logs a debug message.
func Debug(msg string, args ...any) {
	getDefault().Debug(msg, args...)
}

// Fatal logs an error and exits the program.
func Fatal(msg string, args ...any) {
	getDefault().Error(msg, args...)
	os.Exit(1)
}
