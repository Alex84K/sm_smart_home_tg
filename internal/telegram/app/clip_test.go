package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Alex84K/sm_smart_home_core_go/contract"
	tgapp "github.com/Alex84K/sm_smart_home_tg/internal/telegram/app"
)

// fakeClipCore serves the clip endpoints of the core.
type fakeClipCore struct {
	mu          sync.Mutex
	startStatus int
	stopStatus  int
	stops       int
}

func (f *fakeClipCore) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/clips":
		status := f.startStatus
		if status == 0 {
			status = http.StatusCreated
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusCreated {
			_ = json.NewEncoder(w).Encode(contract.ClipRecording{Id: "abc", StartedAt: time.Now(), MaxSeconds: 60})
		} else {
			_ = json.NewEncoder(w).Encode(contract.ErrorResponse{Error: "x"})
		}
	case r.Method == http.MethodPost && r.URL.Path == "/api/clips/abc/stop":
		f.stops++
		status := f.stopStatus
		if status == 0 {
			status = http.StatusOK
		}
		if status == http.StatusOK {
			w.Header().Set("Content-Type", "video/mp4")
			w.WriteHeader(status)
			_, _ = w.Write([]byte("mp4-data"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(contract.ErrorResponse{Error: "x"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func clipMessage(text string) *models.Update {
	return &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   10,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: text,
		},
	}
}

func clipStopCallback(data string) *models.Update {
	return &models.Update{
		ID: 2,
		CallbackQuery: &models.CallbackQuery{
			ID:   "cb1",
			From: models.User{ID: 100},
			Data: data,
			Message: models.MaybeInaccessibleMessage{
				Message: &models.Message{ID: 1, Chat: models.Chat{ID: 100}},
			},
		},
	}
}

// captureAfterFunc records the auto-stop instead of scheduling it.
func captureAfterFunc(app *tgapp.App) (*time.Duration, *func()) {
	var d time.Duration
	var fn func()
	app.SetAfterFunc(func(dd time.Duration, f func()) *time.Timer {
		d, fn = dd, f
		return time.NewTimer(time.Hour)
	})
	return &d, &fn
}

func findCalls(calls []tgCall, endpoint string) []tgCall {
	var out []tgCall
	for _, c := range calls {
		if strings.HasSuffix(c.Endpoint, endpoint) {
			out = append(out, c)
		}
	}
	return out
}

func TestClip_StartThenStop(t *testing.T) {
	core := &fakeClipCore{}
	app, calls, mu := setupTestApp(t, core.handler)
	d, _ := captureAfterFunc(app)
	ctx := context.Background()

	app.HandleClip(ctx, app.Bot(), clipMessage("/clip"))

	mu.Lock()
	msgs := findCalls(*calls, "/sendMessage")
	mu.Unlock()
	if len(msgs) != 1 {
		t.Fatalf("expected one recording message, got %d", len(msgs))
	}
	if text, _ := msgs[0].Body["text"].(string); !strings.Contains(text, "Идёт запись") || !strings.Contains(text, "до 60 с") {
		t.Fatalf("unexpected recording message %q", text)
	}
	markup, _ := json.Marshal(msgs[0].Body["reply_markup"])
	if !strings.Contains(string(markup), "clip_stop:abc") || !strings.Contains(string(markup), "⏹ Стоп") {
		t.Fatalf("expected «⏹ Стоп» button with clip id, got %s", markup)
	}
	if *d != 60*time.Second {
		t.Fatalf("auto-stop scheduled after %v, want 60s", *d)
	}

	app.HandleCallbackClipStop(ctx, app.Bot(), clipStopCallback("clip_stop:abc"))

	mu.Lock()
	defer mu.Unlock()
	videos := findCalls(*calls, "/sendVideo")
	if len(videos) != 1 {
		t.Fatalf("expected one video sent, got %d", len(videos))
	}
	if videos[0].Body["supports_streaming"] != true {
		t.Fatalf("expected supports_streaming, got %v", videos[0].Body["supports_streaming"])
	}
	if len(findCalls(*calls, "/editMessageText")) == 0 {
		t.Fatal("expected recording message to change to «Готовлю видео»")
	}
	if len(findCalls(*calls, "/deleteMessage")) != 1 {
		t.Fatal("expected recording message to be deleted after the video")
	}
	if core.stops != 1 {
		t.Fatalf("expected one stop request to the core, got %d", core.stops)
	}
}

func TestClip_AutoStopAtMax(t *testing.T) {
	core := &fakeClipCore{}
	app, calls, mu := setupTestApp(t, core.handler)
	_, fn := captureAfterFunc(app)

	app.HandleClip(context.Background(), app.Bot(), clipMessage("🎬 Клип"))
	if *fn == nil {
		t.Fatal("expected auto-stop to be scheduled")
	}
	(*fn)()

	mu.Lock()
	defer mu.Unlock()
	if len(findCalls(*calls, "/sendVideo")) != 1 {
		t.Fatal("expected video to be sent at max duration without «Стоп»")
	}
}

func TestClip_StopAfterAutoStopIsIgnored(t *testing.T) {
	core := &fakeClipCore{}
	app, _, _ := setupTestApp(t, core.handler)
	_, fn := captureAfterFunc(app)
	ctx := context.Background()

	app.HandleClip(ctx, app.Bot(), clipMessage("/clip"))
	(*fn)()
	core.mu.Lock()
	core.stopStatus = http.StatusNotFound
	core.mu.Unlock()
	app.HandleCallbackClipStop(ctx, app.Bot(), clipStopCallback("clip_stop:abc"))

	core.mu.Lock()
	defer core.mu.Unlock()
	if core.stops != 2 {
		t.Fatalf("expected the late button to ask the core once more, got %d stops", core.stops)
	}
}

func TestClip_Errors(t *testing.T) {
	tests := []struct {
		name        string
		startStatus int
		stopStatus  int
		stop        bool
		want        string
	}{
		{name: "Scenario: Запись уже идёт", startStatus: http.StatusConflict, want: "Уже идёт запись"},
		{name: "Scenario: Камера недоступна", startStatus: http.StatusServiceUnavailable, want: "Камера сейчас недоступна"},
		{name: "empty clip", stopStatus: http.StatusUnprocessableEntity, stop: true, want: "Клип пустой"},
		{name: "recording failed", stopStatus: http.StatusInternalServerError, stop: true, want: "Не удалось записать клип"},
		{name: "already stopped", stopStatus: http.StatusNotFound, stop: true, want: "Запись уже остановлена"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := &fakeClipCore{startStatus: tt.startStatus, stopStatus: tt.stopStatus}
			app, calls, mu := setupTestApp(t, core.handler)
			captureAfterFunc(app)
			ctx := context.Background()

			app.HandleClip(ctx, app.Bot(), clipMessage("/clip"))
			if tt.stop {
				app.HandleCallbackClipStop(ctx, app.Bot(), clipStopCallback("clip_stop:abc"))
			}

			mu.Lock()
			defer mu.Unlock()
			var texts []string
			for _, c := range append(findCalls(*calls, "/sendMessage"), findCalls(*calls, "/editMessageText")...) {
				if text, ok := c.Body["text"].(string); ok {
					texts = append(texts, text)
				}
			}
			if !strings.Contains(strings.Join(texts, "\n"), tt.want) {
				t.Fatalf("expected a message containing %q, got %q", tt.want, texts)
			}
			if len(findCalls(*calls, "/sendVideo")) != 0 {
				t.Fatal("no video expected")
			}
		})
	}
}

func TestMenu_HasClipButton(t *testing.T) {
	app, calls, mu := setupTestApp(t, nil)
	app.HandleMenu(context.Background(), app.Bot(), clipMessage("/menu"))

	mu.Lock()
	defer mu.Unlock()
	msgs := findCalls(*calls, "/sendMessage")
	if len(msgs) == 0 {
		t.Fatal("expected menu message")
	}
	markup, _ := json.Marshal(msgs[0].Body["reply_markup"])
	if !strings.Contains(string(markup), `"callback_data":"clip"`) || !strings.Contains(string(markup), "🎬 Клип") {
		t.Fatalf("expected «🎬 Клип» in the menu, got %s", markup)
	}
}
