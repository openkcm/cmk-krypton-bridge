package logger

import (
	"log/slog"
	"os"
)

// New creates an slog.Logger for the given level.
// development=true gives a human-readable text logger; false gives JSON production logger.
func New(level string, development bool) (*slog.Logger, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	if development {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	return slog.New(handler), nil
}
