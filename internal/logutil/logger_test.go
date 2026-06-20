package logutil

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestLevelConstants(t *testing.T) {
	// Verify level ordering: Error < Warn < Info < Debug
	if LevelError >= LevelWarn {
		t.Error("LevelError should be less than LevelWarn")
	}
	if LevelWarn >= LevelInfo {
		t.Error("LevelWarn should be less than LevelInfo")
	}
	if LevelInfo >= LevelDebug {
		t.Error("LevelInfo should be less than LevelDebug")
	}
}

func TestSetLevel(t *testing.T) {
	// Save and restore original level
	original := GetLevel()
	defer SetLevel(original)

	SetLevel(LevelDebug)
	if GetLevel() != LevelDebug {
		t.Errorf("expected LevelDebug, got %v", GetLevel())
	}

	SetLevel(LevelError)
	if GetLevel() != LevelError {
		t.Errorf("expected LevelError, got %v", GetLevel())
	}

	SetLevel(LevelWarn)
	if GetLevel() != LevelWarn {
		t.Errorf("expected LevelWarn, got %v", GetLevel())
	}

	SetLevel(LevelInfo)
	if GetLevel() != LevelInfo {
		t.Errorf("expected LevelInfo, got %v", GetLevel())
	}
}

func TestSetLevel_Concurrent(t *testing.T) {
	original := GetLevel()
	defer SetLevel(original)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			levels := []Level{LevelError, LevelWarn, LevelInfo, LevelDebug}
			SetLevel(levels[n%len(levels)])
			_ = GetLevel() // concurrent read
		}(i)
	}
	wg.Wait()
	// Should not panic or race
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
		hasError bool
	}{
		{"error", LevelError, false},
		{"ERROR", LevelError, false},
		{"warn", LevelWarn, false},
		{"WARN", LevelWarn, false},
		{"warning", LevelWarn, false},
		{"WARNING", LevelWarn, false},
		{"info", LevelInfo, false},
		{"INFO", LevelInfo, false},
		{"debug", LevelDebug, false},
		{"DEBUG", LevelDebug, false},
		{"unknown", LevelInfo, true}, // returns LevelInfo on error
		{"trace", LevelInfo, true},
		{"", LevelInfo, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level, err := ParseLevel(tt.input)
			if level != tt.expected {
				t.Errorf("expected level %v, got %v", tt.expected, level)
			}
			if tt.hasError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.hasError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestVerboseFlag(t *testing.T) {
	tests := []struct {
		count    int
		expected Level
	}{
		{0, LevelInfo},
		{1, LevelInfo},
		{2, LevelDebug},
		{3, LevelDebug},
		{10, LevelDebug},
		{-1, LevelInfo},
	}

	for _, tt := range tests {
		level := VerboseFlag(tt.count)
		if level != tt.expected {
			t.Errorf("VerboseFlag(%d): expected %v, got %v", tt.count, tt.expected, level)
		}
	}
}

func TestQuietFlag(t *testing.T) {
	if level := QuietFlag(true); level != LevelError {
		t.Errorf("QuietFlag(true): expected LevelError, got %v", level)
	}
	if level := QuietFlag(false); level != LevelInfo {
		t.Errorf("QuietFlag(false): expected LevelInfo, got %v", level)
	}
}

func TestNew(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "TEST ", 0)
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
	if l.logger == nil {
		t.Fatal("expected non-nil internal logger")
	}
}

// --- Logger output tests ---

func TestLogger_Error(t *testing.T) {
	original := GetLevel()
	defer SetLevel(original)

	var buf bytes.Buffer
	l := New(&buf, "", 0)

	// Error level should log errors
	SetLevel(LevelError)
	l.Error("test error %d", 42)
	output := buf.String()
	if !strings.Contains(output, "[ERROR]") {
		t.Error("expected [ERROR] prefix")
	}
	if !strings.Contains(output, "test error 42") {
		t.Errorf("expected 'test error 42', got %q", output)
	}
}

func TestLogger_Error_SuppressedAtInfo(t *testing.T) {
	original := GetLevel()
	defer SetLevel(original)

	var buf bytes.Buffer
	l := New(&buf, "", 0)

	// Error should always be logged (LevelError is the minimum)
	SetLevel(LevelError)
	buf.Reset()
	l.Error("should appear")
	if !strings.Contains(buf.String(), "should appear") {
		t.Error("error should appear at error level")
	}

	SetLevel(LevelDebug)
	buf.Reset()
	l.Error("should appear too")
	if !strings.Contains(buf.String(), "should appear too") {
		t.Error("error should appear at debug level")
	}
}

func TestLogger_Warn(t *testing.T) {
	original := GetLevel()
	defer SetLevel(original)

	var buf bytes.Buffer
	l := New(&buf, "", 0)

	// At Error level, warn should be suppressed
	SetLevel(LevelError)
	buf.Reset()
	l.Warn("should not appear")
	if buf.Len() != 0 {
		t.Error("warn should be suppressed at error level")
	}

	// At Warn level, warn should appear
	SetLevel(LevelWarn)
	buf.Reset()
	l.Warn("test warning")
	if !strings.Contains(buf.String(), "[WARN]") {
		t.Error("expected [WARN] prefix")
	}
	if !strings.Contains(buf.String(), "test warning") {
		t.Error("expected 'test warning'")
	}
}

func TestLogger_Info(t *testing.T) {
	original := GetLevel()
	defer SetLevel(original)

	var buf bytes.Buffer
	l := New(&buf, "", 0)

	// At Warn level, info should be suppressed
	SetLevel(LevelWarn)
	buf.Reset()
	l.Info("should not appear")
	if buf.Len() != 0 {
		t.Error("info should be suppressed at warn level")
	}

	// At Info level, info should appear
	SetLevel(LevelInfo)
	buf.Reset()
	l.Info("test info")
	if !strings.Contains(buf.String(), "[INFO]") {
		t.Error("expected [INFO] prefix")
	}
	if !strings.Contains(buf.String(), "test info") {
		t.Error("expected 'test info'")
	}
}

func TestLogger_Debug(t *testing.T) {
	original := GetLevel()
	defer SetLevel(original)

	var buf bytes.Buffer
	l := New(&buf, "", 0)

	// At Info level, debug should be suppressed
	SetLevel(LevelInfo)
	buf.Reset()
	l.Debug("should not appear")
	if buf.Len() != 0 {
		t.Error("debug should be suppressed at info level")
	}

	// At Debug level, debug should appear
	SetLevel(LevelDebug)
	buf.Reset()
	l.Debug("test debug %s", "msg")
	if !strings.Contains(buf.String(), "[DEBUG]") {
		t.Error("expected [DEBUG] prefix")
	}
	if !strings.Contains(buf.String(), "test debug msg") {
		t.Errorf("expected 'test debug msg', got %q", buf.String())
	}
}

func TestLogger_AllLevelsAtDebug(t *testing.T) {
	original := GetLevel()
	defer SetLevel(original)

	var buf bytes.Buffer
	l := New(&buf, "", 0)

	SetLevel(LevelDebug)

	l.Error("e")
	l.Warn("w")
	l.Info("i")
	l.Debug("d")

	output := buf.String()
	if !strings.Contains(output, "[ERROR] e") {
		t.Error("missing error message")
	}
	if !strings.Contains(output, "[WARN] w") {
		t.Error("missing warn message")
	}
	if !strings.Contains(output, "[INFO] i") {
		t.Error("missing info message")
	}
	if !strings.Contains(output, "[DEBUG] d") {
		t.Error("missing debug message")
	}
}

func TestLogger_OnlyErrorAtErrorLevel(t *testing.T) {
	original := GetLevel()
	defer SetLevel(original)

	var buf bytes.Buffer
	l := New(&buf, "", 0)

	SetLevel(LevelError)

	l.Error("e")
	l.Warn("w")
	l.Info("i")
	l.Debug("d")

	output := buf.String()
	if !strings.Contains(output, "[ERROR] e") {
		t.Error("missing error message")
	}
	if strings.Contains(output, "[WARN]") {
		t.Error("warn should not appear at error level")
	}
	if strings.Contains(output, "[INFO]") {
		t.Error("info should not appear at error level")
	}
	if strings.Contains(output, "[DEBUG]") {
		t.Error("debug should not appear at error level")
	}
}

// --- Package-level functions tests ---

func TestPackageFunctions(t *testing.T) {
	original := GetLevel()
	defer SetLevel(original)

	// Test that package-level functions don't panic
	// (they write to DefaultLogger which writes to stderr)
	SetLevel(LevelDebug)
	Error("test error")
	Warn("test warn")
	Info("test info")
	Debug("test debug")

	// At error level, only Error should write
	SetLevel(LevelError)
	Error("only this")
	Warn("not this")
	Info("not this")
	Debug("not this")
}

func TestDefaultLogger(t *testing.T) {
	if DefaultLogger == nil {
		t.Fatal("DefaultLogger should not be nil")
	}
	if DefaultLogger.logger == nil {
		t.Fatal("DefaultLogger's internal logger should not be nil")
	}
}

func TestLogger_Prefix(t *testing.T) {
	original := GetLevel()
	defer SetLevel(original)

	var buf bytes.Buffer
	l := New(&buf, "MYAPP ", 0)

	SetLevel(LevelInfo)
	l.Info("message")

	output := buf.String()
	// The prefix is set on the underlying log.Logger
	if !strings.Contains(output, "MYAPP ") {
		t.Errorf("expected prefix 'MYAPP ', got %q", output)
	}
}

func TestLogger_Concurrent(t *testing.T) {
	original := GetLevel()
	defer SetLevel(original)

	var buf bytes.Buffer
	l := New(&buf, "", 0)

	SetLevel(LevelDebug)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			l.Error("error %d", n)
			l.Warn("warn %d", n)
			l.Info("info %d", n)
			l.Debug("debug %d", n)
		}(i)
	}
	wg.Wait()
	// Should not panic or race — output correctness is best-effort
}
