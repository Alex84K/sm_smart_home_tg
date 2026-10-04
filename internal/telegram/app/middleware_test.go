package app_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Alex84K/tg_gateway_go/internal/platform/log"
	tgapp "github.com/Alex84K/tg_gateway_go/internal/telegram/app"
)

func TestWhitelistMiddleware(t *testing.T) {
	var logBuf bytes.Buffer
	logger := log.New("debug", &logBuf)

	allowedIDs := []int64{111, 222}
	mw := tgapp.WhitelistMiddleware(allowedIDs, logger)

	called := false
	nextHandler := func(ctx context.Context, b *bot.Bot, update *models.Update) {
		called = true
	}

	wrapped := mw(nextHandler)
	ctx := context.Background()

	t.Run("whitelisted user allowed", func(t *testing.T) {
		called = false
		logBuf.Reset()

		update := &models.Update{
			Message: &models.Message{
				From: &models.User{ID: 111, Username: "alice"},
				Chat: models.Chat{ID: 111},
				Text: "/status",
			},
		}

		wrapped(ctx, nil, update)
		if !called {
			t.Fatal("expected handler to be called for allowed user")
		}
		if logBuf.Len() > 0 {
			t.Fatalf("expected no unauthorized warning, got: %s", logBuf.String())
		}
	})

	t.Run("non-whitelisted user silently ignored and logged", func(t *testing.T) {
		called = false
		logBuf.Reset()

		update := &models.Update{
			Message: &models.Message{
				From: &models.User{ID: 999, Username: "intruder"},
				Chat: models.Chat{ID: 999},
				Text: "/photo",
			},
		}

		wrapped(ctx, nil, update)
		if called {
			t.Fatal("expected handler NOT to be called for unauthorized user")
		}
		if !strings.Contains(logBuf.String(), "unauthorized telegram update ignored") {
			t.Fatalf("expected warning log about unauthorized update, got: %s", logBuf.String())
		}
	})
}
