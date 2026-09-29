package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New creates a zap.Logger for the given level.
// development=true gives a human-readable console logger; false gives JSON production logger.
func New(level string, development bool) (*zap.Logger, error) {
	if development {
		return zap.NewDevelopment()
	}
	lvl := zap.InfoLevel
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = zap.InfoLevel
	}
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	cfg.EncoderConfig.TimeKey = "time"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	return cfg.Build()
}
