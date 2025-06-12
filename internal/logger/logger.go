package logger

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
)

// Logger provides structured logging across the application
type Logger struct {
	component string
}

// New creates a new logger for a specific component
func New(component string) *Logger {
	return &Logger{component: component}
}

// GenerateID creates a short unique identifier for request/operation tracing
func GenerateID() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// Log writes a structured log message with fixed-width formatting
func (l *Logger) Log(id, level, message string, args ...interface{}) {
	formattedMsg := fmt.Sprintf(message, args...)
	log.Printf("[%s] [%-5s] [%-8s] %s", id, level, l.component, formattedMsg)
}

// Debug logs debug level messages
func (l *Logger) Debug(id, message string, args ...interface{}) {
	l.Log(id, "DEBUG", message, args...)
}

// Info logs info level messages
func (l *Logger) Info(id, message string, args ...interface{}) {
	l.Log(id, "INFO", message, args...)
}

// Warn logs warning level messages
func (l *Logger) Warn(id, message string, args ...interface{}) {
	l.Log(id, "WARN", message, args...)
}

// Error logs error level messages
func (l *Logger) Error(id, message string, args ...interface{}) {
	l.Log(id, "ERROR", message, args...)
}

// LogWithoutID logs without an ID (for background operations)
func (l *Logger) LogWithoutID(level, message string, args ...interface{}) {
	formattedMsg := fmt.Sprintf(message, args...)
	log.Printf("[xxxxxxxx] [%-5s] [%-8s] %s", level, l.component, formattedMsg)
}

// DebugBg logs debug messages for background operations
func (l *Logger) DebugBg(message string, args ...interface{}) {
	l.LogWithoutID("DEBUG", message, args...)
}

// InfoBg logs info messages for background operations
func (l *Logger) InfoBg(message string, args ...interface{}) {
	l.LogWithoutID("INFO", message, args...)
}

// WarnBg logs warning messages for background operations
func (l *Logger) WarnBg(message string, args ...interface{}) {
	l.LogWithoutID("WARN", message, args...)
}

// ErrorBg logs error messages for background operations
func (l *Logger) ErrorBg(message string, args ...interface{}) {
	l.LogWithoutID("ERROR", message, args...)
}