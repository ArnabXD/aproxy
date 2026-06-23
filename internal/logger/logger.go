package logger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
)

// slog handler shared by all component loggers. JSON to stdout, honoring LOG_LEVEL.
var handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: levelFromEnv()})

func levelFromEnv() slog.Level {
	switch os.Getenv("LOG_LEVEL") {
	case "debug", "DEBUG":
		return slog.LevelDebug
	case "warn", "WARN":
		return slog.LevelWarn
	case "error", "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Logger is a thin component-scoped facade over log/slog.
type Logger struct {
	log *slog.Logger
}

// New creates a logger tagged with the given component.
func New(component string) *Logger {
	return &Logger{log: slog.New(handler).With("component", component)}
}

// GenerateID creates a short unique identifier for request/operation tracing.
func GenerateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (l *Logger) at(level slog.Level, id, msg string, args ...any) {
	l.log.Log(context.Background(), level, fmt.Sprintf(msg, args...), "id", id)
}

func (l *Logger) Debug(id, msg string, args ...any) { l.at(slog.LevelDebug, id, msg, args...) }
func (l *Logger) Info(id, msg string, args ...any)  { l.at(slog.LevelInfo, id, msg, args...) }
func (l *Logger) Warn(id, msg string, args ...any)  { l.at(slog.LevelWarn, id, msg, args...) }
func (l *Logger) Error(id, msg string, args ...any) { l.at(slog.LevelError, id, msg, args...) }

// *Bg variants are for background/non-correlated operations (no trace id).
func (l *Logger) DebugBg(msg string, args ...any) { l.log.Debug(fmt.Sprintf(msg, args...)) }
func (l *Logger) InfoBg(msg string, args ...any)  { l.log.Info(fmt.Sprintf(msg, args...)) }
func (l *Logger) WarnBg(msg string, args ...any)  { l.log.Warn(fmt.Sprintf(msg, args...)) }
func (l *Logger) ErrorBg(msg string, args ...any) { l.log.Error(fmt.Sprintf(msg, args...)) }

// Fatal logs at error level then exits non-zero, mirroring stdlib log.Fatalf.
func (l *Logger) Fatal(msg string, args ...any) {
	l.log.Error(fmt.Sprintf(msg, args...))
	os.Exit(1)
}
