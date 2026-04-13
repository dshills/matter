// Package observe provides structured logging, tracing, metrics, and run recording.
package observe

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// LogLevel controls which log events are emitted.
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// ParseLogLevel converts a string to a LogLevel.
func ParseLogLevel(s string) LogLevel {
	switch s {
	case "debug":
		return LevelDebug
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// LogEntry is a structured log event.
type LogEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	RunID     string         `json:"run_id,omitempty"`
	Step      int            `json:"step,omitempty"`
	Component string         `json:"component,omitempty"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// Logger emits structured JSON log entries.
type Logger struct {
	mu    sync.Mutex
	out   io.Writer
	level LogLevel
	runID string
}

// NewLogger creates a structured logger writing to the given writer.
func NewLogger(out io.Writer, level LogLevel) *Logger {
	if out == nil {
		out = os.Stderr
	}
	return &Logger{out: out, level: level}
}

// SetRunID sets the run ID for all subsequent log entries.
func (l *Logger) SetRunID(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.runID = id
}

// Log emits a structured log entry at the given level.
func (l *Logger) Log(level LogLevel, step int, component, message string, fields map[string]any) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	runID := l.runID
	l.mu.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     levelString(level),
		RunID:     runID,
		Step:      step,
		Component: component,
		Message:   message,
		Fields:    fields,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.out.Write(append(data, '\n'))
}

// Debug logs at debug level.
func (l *Logger) Debug(step int, component, message string, fields map[string]any) {
	l.Log(LevelDebug, step, component, message, fields)
}

// Info logs at info level.
func (l *Logger) Info(step int, component, message string, fields map[string]any) {
	l.Log(LevelInfo, step, component, message, fields)
}

// Warn logs at warn level.
func (l *Logger) Warn(step int, component, message string, fields map[string]any) {
	l.Log(LevelWarn, step, component, message, fields)
}

// Error logs at error level.
func (l *Logger) Error(step int, component, message string, fields map[string]any) {
	l.Log(LevelError, step, component, message, fields)
}

func levelString(l LogLevel) string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}
