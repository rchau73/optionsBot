package logger

import (
	"io"
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

func Init(logPath string, debug bool) error {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	w := io.MultiWriter(os.Stdout, f)
	defaultLogger = slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(defaultLogger)
	return nil
}

func Get() *slog.Logger {
	if defaultLogger == nil {
		defaultLogger = slog.Default()
	}
	return defaultLogger
}
