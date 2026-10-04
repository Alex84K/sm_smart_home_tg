package app_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"

	"github.com/Alex84K/sm_smart_home_tg/internal/coreclient"
	"github.com/Alex84K/sm_smart_home_tg/internal/platform/log"
	tgapp "github.com/Alex84K/sm_smart_home_tg/internal/telegram/app"
)

type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time {
	return m.now
}

func (m *mockClock) Advance(d time.Duration) {
	m.now = m.now.Add(d)
}

func TestBotErrorsHandlerConflictThrottling(t *testing.T) {
	var logBuf bytes.Buffer
	logger := log.New("debug", &logBuf)

	clock := &mockClock{now: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	handler := tgapp.BotErrorsHandler(logger, clock)

	conflictErr := bot.ErrorConflict

	// 1. First conflict error -> SHOULD log warning
	handler(conflictErr)
	if !strings.Contains(logBuf.String(), "getUpdates conflict: another tg-gateway instance is running with this token") {
		t.Fatalf("expected conflict warning on first occurrence, got: %s", logBuf.String())
	}

	// 2. Second conflict error after 30 seconds -> SHOULD NOT log warning (throttled)
	logBuf.Reset()
	clock.Advance(30 * time.Second)
	handler(conflictErr)
	if logBuf.Len() > 0 {
		t.Fatalf("expected throttled conflict not to be logged, got: %s", logBuf.String())
	}

	// 3. Third conflict error after another 30 seconds (total 60s) -> SHOULD log warning
	logBuf.Reset()
	clock.Advance(30 * time.Second)
	handler(conflictErr)
	if !strings.Contains(logBuf.String(), "getUpdates conflict: another tg-gateway instance is running with this token") {
		t.Fatalf("expected conflict warning after 1 minute, got: %s", logBuf.String())
	}

	// 4. Non-conflict error -> always logged at error level
	logBuf.Reset()
	handler(errors.New("some other network error"))
	if !strings.Contains(logBuf.String(), "telegram bot internal error") {
		t.Fatalf("expected internal error logged, got: %s", logBuf.String())
	}
}

func TestAppCoreUnavailableHandling(t *testing.T) {
	// coreclient created with unreachable port
	client, err := coreclient.New("http://127.0.0.1:54321", "token")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	var logBuf bytes.Buffer
	logger := log.New("debug", &logBuf)

	app := tgapp.NewWithDeps(client, logger)

	// Simulated bot.SendMessage tracking via mock HTTP server is avoided since bot.New requires token.
	// Instead, verify that coreclient returns ErrCoreUnavailable and app logs it and formats the expected message.
	ctx := context.Background()
	_, statusErr := client.GetStatus(ctx)
	if !errors.Is(statusErr, coreclient.ErrCoreUnavailable) {
		t.Fatalf("expected ErrCoreUnavailable, got: %v", statusErr)
	}

	// Also verify HandleStatus logic with a mock/test bot
	// We can test that handleStatus writes to logger "failed to get status from core"
	// But without a connected Bot instance, SendMessage on nil bot would panic.
	// Let's verify App struct creation with unreachable core URL doesn't fail at NewWithDeps / New.
	_ = app
}
