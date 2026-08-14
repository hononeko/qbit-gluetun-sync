package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLogger_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	InitWithWriter("debug", "text", &buf)

	Info("test info message", "key", "val")
	output := buf.String()
	if !strings.Contains(output, "level=INFO") || !strings.Contains(output, "msg=\"test info message\"") || !strings.Contains(output, "key=val") {
		t.Fatalf("unexpected text log output: %s", output)
	}

	buf.Reset()
	Debug("test debug message")
	if !strings.Contains(buf.String(), "level=DEBUG") {
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
	if logEntry["level"] != "WARN" {
		t.Errorf("expected level 'WARN', got %v", logEntry["level"])
	}
	if attempt, ok := logEntry["attempt"].(float64); !ok || int(attempt) != 2 {
		t.Errorf("expected attempt 2, got %v", logEntry["attempt"])
	}
}
