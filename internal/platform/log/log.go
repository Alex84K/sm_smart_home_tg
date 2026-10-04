package log

import (
	"context"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

var rtspPasswordRegex = regexp.MustCompile(`(?i)(rtsp[s]?://[^\s/@:]+:)(.*?)(@[a-zA-Z0-9.\-_\[\]]+(?::\d+)?(?:/|[?]|\s|"|$))`)

// MaskSecrets replaces passwords in RTSP URLs with '***'.
func MaskSecrets(input string) string {
	return rtspPasswordRegex.ReplaceAllString(input, "${1}***${3}")
}

// MaskingHandler wraps a slog.Handler to mask sensitive information in all log records.
type MaskingHandler struct {
	slog.Handler
}

// NewMaskingHandler creates a handler that masks secrets in values.
func NewMaskingHandler(h slog.Handler) *MaskingHandler {
	return &MaskingHandler{Handler: h}
}

// Handle processes the Record and masks any secret strings.
func (h *MaskingHandler) Handle(ctx context.Context, r slog.Record) error {
	newRecord := slog.NewRecord(r.Time, r.Level, MaskSecrets(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		newRecord.AddAttrs(maskAttr(a))
		return true
	})
	return h.Handler.Handle(ctx, newRecord)
}

func maskAttr(a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, MaskSecrets(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, MaskSecrets(err.Error()))
		}
		if s, ok := a.Value.Any().(string); ok {
			return slog.String(a.Key, MaskSecrets(s))
		}
	}
	return a
}

// ParseLevel converts a string into a slog.Level.
func ParseLevel(levelStr string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// New creates a new structured logger with the given level and output writer, masking secrets.
func New(levelStr string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	opts := &slog.HandlerOptions{
		Level: ParseLevel(levelStr),
	}
	baseHandler := slog.NewJSONHandler(w, opts)
	return slog.New(NewMaskingHandler(baseHandler))
}
