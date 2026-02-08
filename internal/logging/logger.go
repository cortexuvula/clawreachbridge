package logging

import (
	"io"
	"log/slog"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Setup configures the global slog logger based on config settings.
// Returns the lumberjack logger (if file logging) so it can be closed on shutdown.
func Setup(level, format, file string, maxSizeMB, maxBackups, maxAgeDays int, compress bool) *lumberjack.Logger {
	handler, lj := SetupHandler(level, format, file, maxSizeMB, maxBackups, maxAgeDays, compress)
	slog.SetDefault(slog.New(handler))
	return lj
}

// SetupHandler creates a slog.Handler and optional lumberjack logger without
// setting the global default. This allows callers to wrap the handler (e.g.
// with TeeHandler) before calling slog.SetDefault.
func SetupHandler(level, format, file string, maxSizeMB, maxBackups, maxAgeDays int, compress bool) (slog.Handler, *lumberjack.Logger) {
	var w io.Writer = os.Stdout
	var lj *lumberjack.Logger

	if file != "" {
		lj = &lumberjack.Logger{
			Filename:   file,
			MaxSize:    maxSizeMB,
			MaxBackups: maxBackups,
			MaxAge:     maxAgeDays,
			Compress:   compress,
		}
		w = lj
	}

	lvl := parseLevel(level)

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: lvl}
	switch format {
	case "text":
		handler = slog.NewTextHandler(w, opts)
	default:
		handler = slog.NewJSONHandler(w, opts)
	}

	return handler, lj
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
