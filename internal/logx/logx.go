// Package logx is the operational logger for the daemon, built on log/slog.
// It prints pretty, colorized lines on a TTY (via github.com/lmittmann/tint)
// and structured JSON (NDJSON) when piped/redirected. Verbosity comes from the
// CLAUDE_SAYS_LOG env var, then LOG_LEVEL, defaulting to info. In TUI mode the
// caller redirects logs to a file via InitTo so they never corrupt the render.
package logx

import (
	"io"
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
)

// Custom levels filling gaps slog doesn't define, mapping pino's names.
const (
	LevelTrace  slog.Level = -8
	LevelFatal  slog.Level = 12
	LevelSilent slog.Level = 1 << 30
)

// Init selects stderr as the destination: tint (pretty) when stderr is a TTY,
// JSON when piped. Level comes from the env. It installs the result as the
// slog default and returns it.
func Init() *slog.Logger {
	tty := isTTY(os.Stderr)
	return InitTo(os.Stderr, tty)
}

// InitTo builds a logger writing to w. When tty is true a pretty handler is
// used; otherwise JSON. It installs the result as the slog default and returns
// it. The TUI uses this to redirect operational logs to a file.
func InitTo(w io.Writer, tty bool) *slog.Logger {
	var h slog.Handler
	opts := &slog.HandlerOptions{Level: Level()}
	if tty {
		h = tintHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	l := slog.New(h)
	slog.SetDefault(l)
	return l
}

// tintHandler builds the pretty TTY handler via github.com/lmittmann/tint.
func tintHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	return tint.NewHandler(w, &tint.Options{
		Level:      opts.Level,
		TimeFormat: "15:04:05",
	})
}

// Level resolves the effective level from CLAUDE_SAYS_LOG, then LOG_LEVEL,
// defaulting to info.
func Level() slog.Level {
	if v := os.Getenv("CLAUDE_SAYS_LOG"); v != "" {
		return parseLevel(v)
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		return parseLevel(v)
	}
	return slog.LevelInfo
}

// parseLevel maps pino names (trace/debug/info/warn/error/fatal/silent) to
// slog levels. Unknown values fall back to info.
func parseLevel(s string) slog.Level {
	switch s {
	case "trace":
		return LevelTrace
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "fatal":
		return LevelFatal
	case "silent":
		return LevelSilent
	default:
		return slog.LevelInfo
	}
}

// isTTY reports whether f is an interactive terminal.
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
