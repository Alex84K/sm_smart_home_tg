package app_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-telegram/bot"

	"github.com/Alex84K/sm_smart_home_core_go/contract"
	"github.com/Alex84K/sm_smart_home_tg/internal/telegram/app"
)

func TestHandleEvent_AlertDelivery(t *testing.T) {
	var sentMessages []struct {
		ChatID int64  `json:"chat_id"`
		Text   string `json:"text"`
	}

	fakeTG := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/botdummy:token/sendMessage" {
			_ = r.ParseMultipartForm(1024 * 1024)
			cid, _ := strconv.ParseInt(r.FormValue("chat_id"), 10, 64)
			txt := r.FormValue("text")
			sentMessages = append(sentMessages, struct {
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}{ChatID: cid, Text: txt})

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
			return
		}
		if r.URL.Path == "/botdummy:token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"testbot","username":"testbot"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer fakeTG.Close()

	b, err := bot.New("dummy:token", bot.WithServerURL(fakeTG.URL))
	if err != nil {
		t.Fatalf("bot.New failed: %v", err)
	}

	tgApp := app.NewWithDeps(nil, nil)
	tgApp.SetBot(b)
	tgApp.SetAlertChatID(999888777)
	tgApp.SetRouting([]string{"camera/*"})

	// Case 1: camera/offline matches routing -> alert delivered to ChatID 999888777
	offlineEv := contract.EventEnvelope{
		ID:   "evt-1",
		At:   time.Now().UTC(),
		Kind: contract.KindCameraOffline,
		Text: "Камера cam1 отключена",
		Data: map[string]any{"camera_id": "cam1"},
	}
	tgApp.HandleEvent(offlineEv)

	if len(sentMessages) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sentMessages))
	}
	if sentMessages[0].ChatID != 999888777 {
		t.Errorf("expected ChatID 999888777, got %d", sentMessages[0].ChatID)
	}
	if sentMessages[0].Text != "Камера cam1 отключена" {
		t.Errorf("expected text 'Камера cam1 отключена', got %q", sentMessages[0].Text)
	}

	// Case 2: sensor/offline does not match camera/* routing -> dropped
	sensorEv := contract.EventEnvelope{
		ID:   "evt-2",
		At:   time.Now().UTC(),
		Kind: "sensor/offline",
		Text: "Датчик температуры отключен",
	}
	tgApp.HandleEvent(sensorEv)

	if len(sentMessages) != 1 {
		t.Fatalf("expected still 1 sent message after dropped event, got %d", len(sentMessages))
	}

	// Case 3: camera/online matches routing -> alert delivered
	onlineEv := contract.EventEnvelope{
		ID:   "evt-3",
		At:   time.Now().UTC(),
		Kind: contract.KindCameraOnline,
		Text: "Камера cam1 снова в сети",
		Data: map[string]any{"camera_id": "cam1"},
	}
	tgApp.HandleEvent(onlineEv)

	if len(sentMessages) != 2 {
		t.Fatalf("expected 2 sent messages, got %d", len(sentMessages))
	}
	if sentMessages[1].Text != "Камера cam1 снова в сети" {
		t.Errorf("expected text 'Камера cam1 снова в сети', got %q", sentMessages[1].Text)
	}
}

func TestHandleEvent_NoAlertChatID(t *testing.T) {
	tgApp := app.NewWithDeps(nil, nil)
	tgApp.SetAlertChatID(0) // not configured
	tgApp.SetRouting([]string{"camera/*"})

	// Should not panic or error
	tgApp.HandleEvent(contract.EventEnvelope{
		Kind: contract.KindCameraOffline,
		Text: "Offline",
	})
}
