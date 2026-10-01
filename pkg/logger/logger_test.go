package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestLogger_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter("debug", "text", &buf)

	Info("test info message", "key", "val")
	output := buf.String()
	if !strings.Contains(output, "INFO") || !strings.Contains(output, "test info message") || !strings.Contains(output, "key=val") {
		t.Fatalf("unexpected text log output: %s", output)
	}

	buf.Reset()
	Debug("test debug message")
	if !strings.Contains(buf.String(), "DEBU") && !strings.Contains(buf.String(), "DEBUG") {
		t.Fatalf("expected debug message, got %s", buf.String())
	}
}

func TestLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter("info", "json", &buf)

	Warn("test warning", "attempt", 2)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("failed to unmarshal JSON log: %v, raw output: %s", err, buf.String())
	}

	if logEntry["msg"] != "test warning" {
		t.Errorf("expected msg 'test warning', got %v", logEntry["msg"])
	}
	if !strings.EqualFold(logEntry["level"].(string), "warn") {
		t.Errorf("expected level 'warn', got %v", logEntry["level"])
	}
	if attempt, ok := logEntry["attempt"].(float64); !ok || int(attempt) != 2 {
		t.Errorf("expected attempt 2, got %v", logEntry["attempt"])
	}
}

func TestLogger_LogfmtFormat(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter("info", "logfmt", &buf)

	Info("syncing port", "port", 12345, "status", "ok")
	output := buf.String()
	if !strings.Contains(output, "level=info") || !strings.Contains(output, "msg=\"syncing port\"") || !strings.Contains(output, "port=12345") {
		t.Fatalf("unexpected logfmt output: %s", output)
	}
}

func TestLogger_CallerReporting(t *testing.T) {
	var buf bytes.Buffer
	InitWithOptions(Options{
		Level:           "info",
		Format:          "text",
		ReportCaller:    true,
		ReportTimestamp: false,
	}, &buf)

	Info("testing caller reporting")
	output := buf.String()
	// Should report logger_test.go as the caller, not logger.go
	if !strings.Contains(output, "logger_test.go") {
		t.Fatalf("expected output to contain caller 'logger_test.go', got: %s", output)
	}
}

func TestLogger_WithAndPrefix(t *testing.T) {
	var buf bytes.Buffer
	InitWithOptions(Options{
		Level:           "info",
		Format:          "text",
		ReportTimestamp: false,
	}, &buf)

	prefixed := WithPrefix("worker")
	prefixed.Info("task started")
	if !strings.Contains(buf.String(), "worker") || !strings.Contains(buf.String(), "task started") {
		t.Fatalf("expected worker prefix in output, got: %s", buf.String())
	}

	buf.Reset()
	scoped := With("component", "engine")
	scoped.Info("engine ready")
	if !strings.Contains(buf.String(), "component=engine") {
		t.Fatalf("expected component=engine in output, got: %s", buf.String())
	}
}

func TestLogger_SlogIntegration(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter("info", "json", &buf)

	// Direct slog call via standard library slog
	slog.Info("slog message", "user_id", 42)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("failed to unmarshal JSON from slog: %v, raw output: %s", err, buf.String())
	}
	if logEntry["msg"] != "slog message" {
		t.Errorf("expected msg 'slog message', got %v", logEntry["msg"])
	}
	if uid, ok := logEntry["user_id"].(float64); !ok || int(uid) != 42 {
		t.Errorf("expected user_id 42, got %v", logEntry["user_id"])
	}

	// Slog() accessor
	buf.Reset()
	slogger := Slog()
	slogger.Warn("slog accessor message", "test", true)
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if logEntry["msg"] != "slog accessor message" {
		t.Errorf("expected msg 'slog accessor message', got %v", logEntry["msg"])
	}
}

func TestLogger_SetLevelAndGetLevel(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter("info", "text", &buf)

	if GetLevel() != "info" {
		t.Fatalf("expected level 'info', got %s", GetLevel())
	}

	Debug("should not appear")
	if buf.Len() > 0 {
		t.Fatalf("expected no output for debug at info level, got %s", buf.String())
	}

	SetLevel("debug")
	if GetLevel() != "debug" {
		t.Fatalf("expected level 'debug', got %s", GetLevel())
	}

	Debug("now it appears")
	if !strings.Contains(buf.String(), "now it appears") {
		t.Fatalf("expected debug message after SetLevel(debug), got %s", buf.String())
	}
}

func TestLogger_ContextIntegration(t *testing.T) {
	var buf bytes.Buffer
	customLogger := With("req_id", "abc-123")
	customLogger.SetOutput(&buf)

	ctx := WithContext(context.Background(), customLogger)
	retrieved := FromContext(ctx)
	retrieved.Info("contextual log")

	if !strings.Contains(buf.String(), "req_id=abc-123") || !strings.Contains(buf.String(), "contextual log") {
		t.Fatalf("unexpected contextual log output: %s", buf.String())
	}
}

func TestLogger_FormatHelpers(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter("debug", "text", &buf)

	Infof("formatted %s %d", "info", 1)
	if !strings.Contains(buf.String(), "formatted info 1") {
		t.Fatalf("unexpected Infof output: %s", buf.String())
	}

	buf.Reset()
	Warnf("formatted %s %d", "warn", 2)
	if !strings.Contains(buf.String(), "formatted warn 2") {
		t.Fatalf("unexpected Warnf output: %s", buf.String())
	}

	buf.Reset()
	Errorf("formatted %s %d", "error", 3)
	if !strings.Contains(buf.String(), "formatted error 3") {
		t.Fatalf("unexpected Errorf output: %s", buf.String())
	}

	buf.Reset()
	Debugf("formatted %s %d", "debug", 4)
	if !strings.Contains(buf.String(), "formatted debug 4") {
		t.Fatalf("unexpected Debugf output: %s", buf.String())
	}
}
