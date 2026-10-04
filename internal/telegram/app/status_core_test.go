package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/Alex84K/sm_smart_home_tg/internal/platform/config"
	"github.com/Alex84K/sm_smart_home_tg/internal/platform/log"
	tgapp "github.com/Alex84K/sm_smart_home_tg/internal/telegram/app"
)

func TestGatewayStatusWhenCoreUnavailable(t *testing.T) {
	// Fake Telegram API server
	var sentText string
	fakeTG := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("fakeTG received: %s %s", r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")

		if strings.HasSuffix(r.URL.Path, "/getMe") {
			user := models.User{
				ID:        12345,
				IsBot:     true,
				FirstName: "SmartHomeBot",
				Username:  "smarthome_bot",
			}
			raw, _ := json.Marshal(user)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": json.RawMessage(raw),
			})
			return
		}

		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			body, _ := io.ReadAll(r.Body)
			sentText = string(body)

			msg := models.Message{
				ID:   1,
				Chat: models.Chat{ID: 100},
				Text: sentText,
			}
			raw, _ := json.Marshal(msg)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": json.RawMessage(raw),
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": true,
		})
	}))
	defer fakeTG.Close()

	// Core is unreachable
	cfg := &config.TGConfig{
		LogLevel:         "debug",
		TelegramBotToken: "dummy:token",
		TelegramAPIURL:   fakeTG.URL,
		CoreAPIURL:       "http://127.0.0.1:54321", // dead port
		CoreAPIToken:     "dummy-token",
		AllowedIDs:       []int64{100},
	}

	logger := log.New("debug", io.Discard)

	// Шлюз создаётся при недоступном адресе ядра
	app, err := tgapp.New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create gateway with unavailable core: %v", err)
	}

	// Dispatch /status command
	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   10,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: "/status",
		},
	}

	app.HandleStatus(context.Background(), app.Bot(), update)

	if !strings.Contains(sentText, "ядро") && !strings.Contains(sentText, "Ядро") && !strings.Contains(sentText, "недоступно") {
		t.Fatalf("expected message about unavailable core, got: %q", sentText)
	}
}
