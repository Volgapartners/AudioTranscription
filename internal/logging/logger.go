package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorDim    = "\033[2m"
	colorBold   = "\033[1m"
)

// SetupLogger configures the global slog logger.
// If the output is a TTY, it uses color-coded text.
// Otherwise (piped, redirected, CI), it uses JSON.
func SetupLogger(level slog.Level, forceJSON bool) {
	if forceJSON || !isTerminal(os.Stderr) {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
		return
	}
	slog.SetDefault(slog.New(NewColorHandler(os.Stderr, level)))
}

// isTerminal checks if the given file is a terminal.
func isTerminal(f *os.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// ColorHandler is a slog.Handler that writes color-coded log output to a terminal.
type ColorHandler struct {
	w     io.Writer
	level slog.Level
	mu    sync.Mutex
	attrs []slog.Attr
	group string
}

// NewColorHandler creates a color-coded slog handler.
func NewColorHandler(w io.Writer, level slog.Level) *ColorHandler {
	return &ColorHandler{w: w, level: level}
}

func (h *ColorHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *ColorHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Pick color based on level
	var levelColor, msgColor string
	switch {
	case r.Level >= slog.LevelError:
		levelColor = colorRed
		msgColor = colorRed
	case r.Level >= slog.LevelWarn:
		levelColor = colorYellow
		msgColor = colorYellow
	case r.Level >= slog.LevelInfo:
		levelColor = colorCyan
		msgColor = colorCyan
	default:
		levelColor = colorDim
		msgColor = colorDim
	}

	// Format timestamp
	ts := r.Time.Format(time.TimeOnly)

	// Build the line
	line := fmt.Sprintf("%s%s%s %s%-5s%s %s%s%s",
		colorDim, ts, colorReset,
		levelColor, r.Level.String(), colorReset,
		msgColor, r.Message, colorReset,
	)

	// Append pre-set attrs
	for _, a := range h.attrs {
		line += fmt.Sprintf(" %s%s=%v%s", colorDim, a.Key, a.Value.Any(), colorReset)
	}

	// Append record attrs
	r.Attrs(func(a slog.Attr) bool {
		line += fmt.Sprintf(" %s%s=%v%s", colorDim, a.Key, a.Value.Any(), colorReset)
		return true
	})

	_, err := fmt.Fprintln(h.w, line)
	return err
}

func (h *ColorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &ColorHandler{w: h.w, level: h.level, attrs: newAttrs, group: h.group}
}

func (h *ColorHandler) WithGroup(name string) slog.Handler {
	return &ColorHandler{w: h.w, level: h.level, attrs: h.attrs, group: name}
}

// --- Step-level output helpers (bypass slog for clean UX) ---

// StepHeader prints a bold step header like "[1/14] Analyzing Drive folder..."
func StepHeader(step, total int, msg string) {
	fmt.Fprintf(os.Stderr, "\n%s%s[%d/%d]%s %s%s%s\n",
		colorBold, colorCyan, step, total, colorReset,
		colorCyan, msg, colorReset)
}

// StepSuccess prints a green success line.
func StepSuccess(msg string) {
	fmt.Fprintf(os.Stderr, "       %s✓ %s%s\n", colorGreen, msg, colorReset)
}

// StepError prints a red error line.
func StepError(msg string) {
	fmt.Fprintf(os.Stderr, "       %s✗ %s%s\n", colorRed, msg, colorReset)
}

// StepWarn prints a yellow warning line.
func StepWarn(msg string) {
	fmt.Fprintf(os.Stderr, "       %s⚠ %s%s\n", colorYellow, msg, colorReset)
}

// StepDetail prints a dim detail line.
func StepDetail(msg string) {
	fmt.Fprintf(os.Stderr, "       %s%s%s\n", colorDim, msg, colorReset)
}

// StepInfo prints an info line (no icon).
func StepInfo(msg string) {
	fmt.Fprintf(os.Stderr, "       %s%s%s\n", colorCyan, msg, colorReset)
}

// Banner prints a bold banner with borders.
func Banner(msg string) {
	border := ""
	for i := 0; i < len(msg)+4; i++ {
		border += "═"
	}
	fmt.Fprintf(os.Stderr, "\n%s%s%s\n", colorBold, border, colorReset)
	fmt.Fprintf(os.Stderr, "%s  %s  %s\n", colorBold, msg, colorReset)
	fmt.Fprintf(os.Stderr, "%s%s%s\n", colorBold, border, colorReset)
}
