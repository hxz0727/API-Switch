package logutil

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

// Level represents a log severity level.
type Level int

const (
	// LevelError only logs errors.
	LevelError Level = iota
	// LevelWarn logs warnings and errors.
	LevelWarn
	// LevelInfo logs info, warnings, and errors (default).
	LevelInfo
	// LevelDebug logs everything including debug messages.
	LevelDebug
)

var (
	currentLevel Level = LevelInfo
	mu           sync.RWMutex
)

// SetLevel sets the global log level.
func SetLevel(l Level) {
	mu.Lock()
	currentLevel = l
	mu.Unlock()
}

// GetLevel returns the current log level.
func GetLevel() Level {
	mu.RLock()
	defer mu.RUnlock()
	return currentLevel
}

// ParseLevel parses a log level from a string.
func ParseLevel(s string) (Level, error) {
	switch s {
	case "error", "ERROR":
		return LevelError, nil
	case "warn", "WARN", "warning", "WARNING":
		return LevelWarn, nil
	case "info", "INFO":
		return LevelInfo, nil
	case "debug", "DEBUG":
		return LevelDebug, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level: %s", s)
	}
}

// VerboseFlag converts a verbosity count (-v, -vv) to a log level.
func VerboseFlag(count int) Level {
	switch {
	case count >= 2:
		return LevelDebug
	case count == 1:
		// Keep default info level, but debug is also set
		// Actually -v should enable some extra info
		return LevelInfo
	default:
		return LevelInfo
	}
}

// QuietFlag returns the log level for quiet mode.
func QuietFlag(quiet bool) Level {
	if quiet {
		return LevelError
	}
	return LevelInfo
}

// Logger is a leveled logger that wraps Go's standard log package.
type Logger struct {
	logger *log.Logger
}

// New creates a new leveled logger.
func New(w io.Writer, prefix string, flags int) *Logger {
	return &Logger{
		logger: log.New(w, prefix, flags),
	}
}

// DefaultLogger is the package-level logger.
var DefaultLogger = New(os.Stderr, "", log.LstdFlags)

// Error logs at error level.
func Error(format string, v ...interface{}) {
	DefaultLogger.Error(format, v...)
}

// Warn logs at warn level.
func Warn(format string, v ...interface{}) {
	DefaultLogger.Warn(format, v...)
}

// Info logs at info level.
func Info(format string, v ...interface{}) {
	DefaultLogger.Info(format, v...)
}

// Debug logs at debug level.
func Debug(format string, v ...interface{}) {
	DefaultLogger.Debug(format, v...)
}

// Error logs at error level.
func (l *Logger) Error(format string, v ...interface{}) {
	if GetLevel() >= LevelError {
		l.logger.Printf("[ERROR] "+format, v...)
	}
}

// Warn logs at warn level.
func (l *Logger) Warn(format string, v ...interface{}) {
	if GetLevel() >= LevelWarn {
		l.logger.Printf("[WARN] "+format, v...)
	}
}

// Info logs at info level.
func (l *Logger) Info(format string, v ...interface{}) {
	if GetLevel() >= LevelInfo {
		l.logger.Printf("[INFO] "+format, v...)
	}
}

// Debug logs at debug level.
func (l *Logger) Debug(format string, v ...interface{}) {
	if GetLevel() >= LevelDebug {
		l.logger.Printf("[DEBUG] "+format, v...)
	}
}
