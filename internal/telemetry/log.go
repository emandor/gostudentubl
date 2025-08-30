package telemetry

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

func NewLogger() zerolog.Logger {
	// rotate app.log (~10MB, keep 3 backups)
	rotator := &lumberjack.Logger{
		Filename:   "app.log",
		MaxSize:    10,
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	multi := zerolog.MultiLevelWriter(os.Stdout, rotator)
	logger := zerolog.New(multi).With().Timestamp().Logger()
	zerolog.TimeFieldFormat = time.RFC3339
	return logger
}
