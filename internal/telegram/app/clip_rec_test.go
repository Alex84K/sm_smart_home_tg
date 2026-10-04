package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/Alex84K/sm_smart_home_core_go/contract"
)

func TestHandleClip_Default(t *testing.T) {
	var requestedSec int
	var requestedMaxBytes int64
	var chatActions []string

	var actionMu sync.Mutex
	app, calls, mu := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/clip") {
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("fake-mp4-data"))
			return
		}
	}, func(endpoint string) {
		if strings.HasSuffix(endpoint, "/sendChatAction") {
			actionMu.Lock()
			chatActions = append(chatActions, endpoint)
			actionMu.Unlock()
		}
	})

	_ = requestedSec
	_ = requestedMaxBytes

	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   10,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: "/clip",
		},
	}

	app.HandleClip(context.Background(), app.Bot(), update)

	mu.Lock()
	defer mu.Unlock()

	var videoCall *tgCall
	for i := range *calls {
		if strings.HasSuffix((*calls)[i].Endpoint, "/sendVideo") {
			videoCall = &(*calls)[i]
			break
		}
	}

	if videoCall == nil {
		t.Fatal("expected /sendVideo to be called")
	}

	if len(chatActions) == 0 {
		t.Error("expected sendChatAction to be called while preparing clip")
	}
}

func TestHandleClip_CustomDuration(t *testing.T) {
	app, calls, mu := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/clip") {
			if r.URL.Query().Get("sec") != "45" {
				t.Errorf("expected sec=45, got %s", r.URL.Query().Get("sec"))
			}
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("fake-mp4-data"))
			return
		}
	})

	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   10,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: "/clip 45",
		},
	}

	app.HandleClip(context.Background(), app.Bot(), update)

	mu.Lock()
	defer mu.Unlock()

	var videoCall *tgCall
	for i := range *calls {
		if strings.HasSuffix((*calls)[i].Endpoint, "/sendVideo") {
			videoCall = &(*calls)[i]
			break
		}
	}

	if videoCall == nil {
		t.Fatal("expected /sendVideo to be called for /clip 45")
	}
}

func TestHandleClip_InvalidDuration(t *testing.T) {
	app, calls, mu := setupTestApp(t, nil)

	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   10,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: "/clip -5",
		},
	}

	app.HandleClip(context.Background(), app.Bot(), update)

	mu.Lock()
	defer mu.Unlock()

	var msgCall *tgCall
	for i := range *calls {
		if strings.HasSuffix((*calls)[i].Endpoint, "/sendMessage") {
			msgCall = &(*calls)[i]
			break
		}
	}

	if msgCall == nil {
		t.Fatal("expected /sendMessage with error for invalid duration")
	}
	text, _ := msgCall.Body["text"].(string)
	if !strings.Contains(text, "положительным") {
		t.Errorf("expected message about positive duration, got: %s", text)
	}
}

func TestHandleClip_RecorderDisabled(t *testing.T) {
	app, calls, mu := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/clip") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(contract.ErrorResponse{Error: "recorder disabled"})
			return
		}
	})

	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   10,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: "/clip",
		},
	}

	app.HandleClip(context.Background(), app.Bot(), update)

	mu.Lock()
	defer mu.Unlock()

	var msgCall *tgCall
	for i := range *calls {
		if strings.HasSuffix((*calls)[i].Endpoint, "/sendMessage") {
			msgCall = &(*calls)[i]
			break
		}
	}

	if msgCall == nil {
		t.Fatal("expected /sendMessage when recorder is disabled")
	}
	text, _ := msgCall.Body["text"].(string)
	if !strings.Contains(text, "выключена") {
		t.Errorf("expected message about recorder disabled, got: %s", text)
	}
}

func TestHandleClip_ClipTooLarge(t *testing.T) {
	app, calls, mu := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/clip") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_ = json.NewEncoder(w).Encode(contract.ClipTooLargeResponse{
				Error:             "clip too large",
				MaxAllowedSeconds: 22,
			})
			return
		}
	})

	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   10,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: "/clip 60",
		},
	}

	app.HandleClip(context.Background(), app.Bot(), update)

	mu.Lock()
	defer mu.Unlock()

	var msgCall *tgCall
	for i := range *calls {
		if strings.HasSuffix((*calls)[i].Endpoint, "/sendMessage") {
			msgCall = &(*calls)[i]
			break
		}
	}

	if msgCall == nil {
		t.Fatal("expected /sendMessage when clip is too large")
	}
	text, _ := msgCall.Body["text"].(string)
	if !strings.Contains(text, "22") {
		t.Errorf("expected message mentioning max allowed duration ~22s, got: %s", text)
	}
}

func TestHandleRec_OnAndOff(t *testing.T) {
	var requestedState bool
	var stateMu sync.Mutex

	app, calls, mu := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/recorder" && r.Method == http.MethodPost {
			var req contract.RecorderRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			stateMu.Lock()
			requestedState = req.Enabled
			stateMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(contract.RecorderStatus(req))
			return
		}
	})

	// /rec off
	updateOff := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   10,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: "/rec off",
		},
	}

	app.HandleRec(context.Background(), app.Bot(), updateOff)

	stateMu.Lock()
	if requestedState != false {
		t.Error("expected recorder to be set to false")
	}
	stateMu.Unlock()

	mu.Lock()
	var lastMsg string
	for _, c := range *calls {
		if strings.HasSuffix(c.Endpoint, "/sendMessage") {
			lastMsg = c.Body["text"].(string)
		}
	}
	mu.Unlock()

	if !strings.Contains(lastMsg, "выключена") {
		t.Errorf("expected confirmation message about recorder off, got: %s", lastMsg)
	}

	// /rec on
	updateOn := &models.Update{
		ID: 2,
		Message: &models.Message{
			ID:   11,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: "/rec on",
		},
	}

	app.HandleRec(context.Background(), app.Bot(), updateOn)

	stateMu.Lock()
	if requestedState != true {
		t.Error("expected recorder to be set to true")
	}
	stateMu.Unlock()

	mu.Lock()
	for _, c := range *calls {
		if strings.HasSuffix(c.Endpoint, "/sendMessage") {
			lastMsg = c.Body["text"].(string)
		}
	}
	mu.Unlock()

	if !strings.Contains(lastMsg, "включена") {
		t.Errorf("expected confirmation message about recorder on, got: %s", lastMsg)
	}
}

func TestHandleStatus_IncludesRecorderAndArchive(t *testing.T) {
	app, calls, mu := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			pending := 3
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(contract.StatusResponse{
				Camera: contract.CameraStatus{Online: true},
				Recorder: &contract.RecorderStatus{
					Enabled: true,
				},
				Archive: &contract.ArchiveStatus{
					Ok:              true,
					PendingSegments: &pending,
				},
				Storage: contract.StorageStatus{
					FreeBytes:  50 * 1024 * 1024 * 1024,
					TotalBytes: 100 * 1024 * 1024 * 1024,
				},
			})
			return
		}
	})

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

	mu.Lock()
	defer mu.Unlock()

	var msgCall *tgCall
	for i := range *calls {
		if strings.HasSuffix((*calls)[i].Endpoint, "/sendMessage") {
			msgCall = &(*calls)[i]
			break
		}
	}

	if msgCall == nil {
		t.Fatal("expected /sendMessage for /status")
	}

	text := msgCall.Body["text"].(string)
	if !strings.Contains(text, "Запись: 🟢 Включена") {
		t.Errorf("expected status to show recording enabled, got: %s", text)
	}
	if !strings.Contains(text, "Архив: 🟢 Работает") {
		t.Errorf("expected status to show archive working, got: %s", text)
	}
	if !strings.Contains(text, "в очереди: 3") {
		t.Errorf("expected status to show pending segments, got: %s", text)
	}
}
