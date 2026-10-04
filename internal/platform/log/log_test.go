package log_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/Alex84K/tg_gateway_go/internal/platform/log"
)

func TestMaskSecrets(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "rtsp://admin:superSecret123@192.168.1.100:554/stream1",
			expected: "rtsp://admin:***@192.168.1.100:554/stream1",
		},
		{
			input:    "RTSPS://user:pass_word@cam.local:322/stream",
			expected: "RTSPS://user:***@cam.local:322/stream",
		},
		{
			input:    "connection error to rtsp://cam_user:p@ss:word@10.0.0.1/live failed",
			expected: "connection error to rtsp://cam_user:***@10.0.0.1/live failed",
		},
		{
			input:    "http://example.com/test",
			expected: "http://example.com/test",
		},
	}

	for _, tc := range tests {
		got := log.MaskSecrets(tc.input)
		if got != tc.expected {
			t.Errorf("MaskSecrets(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestLoggerMasking(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New("info", &buf)

	rawURL := "rtsp://alice:secretPass@192.168.8.170:554/stream1"
	logger.Info("connecting to camera", slog.String("url", rawURL))

	out := buf.String()
	if strings.Contains(out, "secretPass") {
		t.Fatalf("log contains unmasked secret: %s", out)
	}
	if !strings.Contains(out, "rtsp://alice:***@192.168.8.170:554/stream1") {
		t.Fatalf("log does not contain masked URL: %s", out)
	}

	buf.Reset()
	errWithSecret := errors.New("failed RTSP dial to rtsp://bob:anotherSecret@10.0.0.5:554/sub")
	logger.Error("camera failure", slog.Any("err", errWithSecret))

	out = buf.String()
	if strings.Contains(out, "anotherSecret") {
		t.Fatalf("log error attr contains unmasked secret: %s", out)
	}
	if !strings.Contains(out, "rtsp://bob:***@10.0.0.5:554/sub") {
		t.Fatalf("log error attr does not contain masked URL: %s", out)
	}
}

func TestParseLevel(t *testing.T) {
	if log.ParseLevel("debug") != slog.LevelDebug {
		t.Errorf("expected debug, got %v", log.ParseLevel("debug"))
	}
	if log.ParseLevel("WARN") != slog.LevelWarn {
		t.Errorf("expected warn, got %v", log.ParseLevel("WARN"))
	}
	if log.ParseLevel("error") != slog.LevelError {
		t.Errorf("expected error, got %v", log.ParseLevel("error"))
	}
	if log.ParseLevel("unknown") != slog.LevelInfo {
		t.Errorf("expected info for unknown, got %v", log.ParseLevel("unknown"))
	}
}
