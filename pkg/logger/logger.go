package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	charmlog "github.com/charmbracelet/log"
)

// Logger is a type alias to charmlog.Logger.
type Logger = charmlog.Logger

// Level is a type alias to charmlog.Level.
type Level = charmlog.Level

// Options defines configuration options for the logger.
type Options struct {
	Level           string // "debug", "info", "warn", "error", "fatal"
	Format          string // "text", "json", "logfmt"
	ReportTimestamp bool   // whether to report timestamps
	TimeFormat      string // time format string (e.g. time.RFC3339)
	ReportCaller    bool   // whether to report caller file and line
	Prefix          string // optional prefix for log messages
}

var internalLogger atomic.Pointer[charmlog.Logger]

func init() {
	defaultLogger := charmlog.NewWithOptions(os.Stdout, charmlog.Options{
		Level:           charmlog.InfoLevel,
		Formatter:       charmlog.TextFormatter,
		ReportTimestamp: true,
		TimeFormat:      charmlog.DefaultTimeFormat,
	})
	internalLogger.Store(defaultLogger)
	slog.SetDefault(slog.New(defaultLogger))
}

// Init initializes the global logger with the specified level and text format.
func Init(levelStr string) {
	InitWithFormat(levelStr, "text")
}

// InitWithFormat initializes the global logger with level and format ("text", "json", or "logfmt").
func InitWithFormat(levelStr, formatStr string) {
	InitWithWriter(levelStr, formatStr, os.Stdout)
}

// InitWithWriter initializes the global logger with level, format, and custom output writer (useful for tests).
func InitWithWriter(levelStr, formatStr string, w io.Writer) {
	InitWithOptions(Options{
		Level:           levelStr,
		Format:          formatStr,
		ReportTimestamp: true,
	}, w)
}

// InitWithOptions initializes the global logger with full Options and an output writer.
func InitWithOptions(opts Options, w io.Writer) {
	if w == nil {
		w = os.Stdout
	}

	var level charmlog.Level
	switch strings.ToLower(opts.Level) {
	case "debug":
		level = charmlog.DebugLevel
	case "info":
		level = charmlog.InfoLevel
	case "warn", "warning":
		level = charmlog.WarnLevel
	case "error":
		level = charmlog.ErrorLevel
	case "fatal":
		level = charmlog.FatalLevel
	default:
		level = charmlog.InfoLevel
	}

	var formatter charmlog.Formatter
	timeFormat := opts.TimeFormat
	switch strings.ToLower(opts.Format) {
	case "json":
		formatter = charmlog.JSONFormatter
		if timeFormat == "" {
			timeFormat = time.RFC3339
		}
	case "logfmt":
		formatter = charmlog.LogfmtFormatter
		if timeFormat == "" {
			timeFormat = time.RFC3339
		}
	case "text", "pretty", "console":
		fallthrough
	default:
		formatter = charmlog.TextFormatter
		if timeFormat == "" {
			timeFormat = charmlog.DefaultTimeFormat
		}
	}

	charmOpts := charmlog.Options{
		Level:           level,
		Formatter:       formatter,
		ReportTimestamp: opts.ReportTimestamp,
		TimeFormat:      timeFormat,
		ReportCaller:    opts.ReportCaller,
		Prefix:          opts.Prefix,
	}

	l := charmlog.NewWithOptions(w, charmOpts)
	internalLogger.Store(l)
	slog.SetDefault(slog.New(l))
}

func getDefault() *charmlog.Logger {
	l := internalLogger.Load()
	if l != nil {
		return l
	}
	fallback := charmlog.NewWithOptions(os.Stdout, charmlog.Options{
		Level:           charmlog.InfoLevel,
		Formatter:       charmlog.TextFormatter,
		ReportTimestamp: true,
		TimeFormat:      charmlog.DefaultTimeFormat,
	})
	internalLogger.Store(fallback)
	slog.SetDefault(slog.New(fallback))
	return fallback
}

// GetLogger returns the underlying *charmlog.Logger instance.
func GetLogger() *charmlog.Logger {
	return getDefault()
}

// Slog returns an *slog.Logger backed by the active charm logger.
func Slog() *slog.Logger {
	return slog.New(getDefault())
}

// Handler returns the slog.Handler of the active charm logger.
func Handler() slog.Handler {
	return getDefault()
}

// With returns a sub-logger with the given key-value pairs pre-populated.
func With(keyvals ...any) *charmlog.Logger {
	return getDefault().With(keyvals...)
}

// WithPrefix returns a sub-logger with the given prefix.
func WithPrefix(prefix string) *charmlog.Logger {
	return getDefault().WithPrefix(prefix)
}

// SetLevel updates the logging level at runtime.
func SetLevel(levelStr string) {
	var level charmlog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = charmlog.DebugLevel
	case "info":
		level = charmlog.InfoLevel
	case "warn", "warning":
		level = charmlog.WarnLevel
	case "error":
		level = charmlog.ErrorLevel
	case "fatal":
		level = charmlog.FatalLevel
	default:
		level = charmlog.InfoLevel
	}
	getDefault().SetLevel(level)
}

// GetLevel returns the current logging level as a lowercase string.
func GetLevel() string {
	return getDefault().GetLevel().String()
}

// SetStyles sets the styles for the active text logger.
func SetStyles(s *charmlog.Styles) {
	getDefault().SetStyles(s)
}

// FromContext retrieves a logger stored in context, or the global default if not present.
func FromContext(ctx context.Context) *charmlog.Logger {
	if l := charmlog.FromContext(ctx); l != nil {
		return l
	}
	return getDefault()
}

// WithContext returns a new context containing the given logger.
func WithContext(ctx context.Context, l *charmlog.Logger) context.Context {
	return charmlog.WithContext(ctx, l)
}

// Info logs an informational message.
func Info(msg string, args ...any) {
	l := getDefault()
	l.Helper()
	l.Info(msg, args...)
}

// Warn logs a warning message.
func Warn(msg string, args ...any) {
	l := getDefault()
	l.Helper()
	l.Warn(msg, args...)
}

// Error logs an error message.
func Error(msg string, args ...any) {
	l := getDefault()
	l.Helper()
	l.Error(msg, args...)
}

// Debug logs a debug message.
func Debug(msg string, args ...any) {
	l := getDefault()
	l.Helper()
	l.Debug(msg, args...)
}

// Fatal logs a fatal message and exits the program.
func Fatal(msg string, args ...any) {
	l := getDefault()
	l.Helper()
	l.Fatal(msg, args...)
}

// Infof logs a formatted informational message.
func Infof(format string, args ...any) {
	l := getDefault()
	l.Helper()
	l.Infof(format, args...)
}

// Warnf logs a formatted warning message.
func Warnf(format string, args ...any) {
	l := getDefault()
	l.Helper()
	l.Warnf(format, args...)
}

// Errorf logs a formatted error message.
func Errorf(format string, args ...any) {
	l := getDefault()
	l.Helper()
	l.Errorf(format, args...)
}

// Debugf logs a formatted debug message.
func Debugf(format string, args ...any) {
	l := getDefault()
	l.Helper()
	l.Debugf(format, args...)
}

// Fatalf logs a formatted fatal message and exits the program.
func Fatalf(format string, args ...any) {
	l := getDefault()
	l.Helper()
	l.Fatalf(format, args...)
}
