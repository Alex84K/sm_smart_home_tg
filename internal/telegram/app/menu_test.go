package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/Alex84K/core_syst_go/contract"
	"github.com/Alex84K/tg_gateway_go/internal/platform/config"
	"github.com/Alex84K/tg_gateway_go/internal/platform/log"
	tgapp "github.com/Alex84K/tg_gateway_go/internal/telegram/app"
)

type tgCall struct {
	Endpoint string
	Body     map[string]any
}

func setupTestApp(t *testing.T, coreHandler http.HandlerFunc, onCall ...func(endpoint string)) (*tgapp.App, *[]tgCall, *sync.Mutex) {
	t.Helper()

	var mu sync.Mutex
	var calls []tgCall

	fakeTG := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		bodyMap := make(map[string]any)
		if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			_ = r.ParseMultipartForm(10 << 20)
			if r.MultipartForm != nil {
				for k, v := range r.MultipartForm.Value {
					if len(v) > 0 {
						var nested any
						if json.Unmarshal([]byte(v[0]), &nested) == nil {
							bodyMap[k] = nested
						} else {
							bodyMap[k] = v[0]
						}
					}
				}
			}
		} else {
			bodyBytes, _ := io.ReadAll(r.Body)
			if len(bodyBytes) > 0 {
				_ = json.Unmarshal(bodyBytes, &bodyMap)
			}
		}

		mu.Lock()
		calls = append(calls, tgCall{
			Endpoint: r.URL.Path,
			Body:     bodyMap,
		})
		mu.Unlock()

		if len(onCall) > 0 && onCall[0] != nil {
			onCall[0](r.URL.Path)
		}

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
			msg := models.Message{
				ID:   1,
				Chat: models.Chat{ID: 100},
				Text: "ok",
			}
			raw, _ := json.Marshal(msg)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": json.RawMessage(raw),
			})
			return
		}

		if strings.HasSuffix(r.URL.Path, "/sendPhoto") {
			msg := models.Message{
				ID:   2,
				Chat: models.Chat{ID: 100},
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
	t.Cleanup(fakeTG.Close)

	var coreURL string
	if coreHandler != nil {
		fakeCore := httptest.NewServer(coreHandler)
		t.Cleanup(fakeCore.Close)
		coreURL = fakeCore.URL
	} else {
		coreURL = "http://127.0.0.1:54321"
	}

	cfg := &config.TGConfig{
		LogLevel:         "debug",
		TelegramBotToken: "dummy:token",
		TelegramAPIURL:   fakeTG.URL,
		CoreAPIURL:       coreURL,
		CoreAPIToken:     "dummy-token",
		AllowedIDs:       []int64{100},
	}

	logger := log.New("debug", io.Discard)
	app, err := tgapp.New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create tg app: %v", err)
	}

	return app, &calls, &mu
}

func TestHandleMenu_InlineKeyboardHasPhotoAndStatus(t *testing.T) {
	app, calls, mu := setupTestApp(t, nil)

	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   10,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: "/menu",
		},
	}

	app.HandleMenu(context.Background(), app.Bot(), update)

	mu.Lock()
	defer mu.Unlock()

	var menuCall *tgCall
	for i := range *calls {
		if strings.HasSuffix((*calls)[i].Endpoint, "/sendMessage") {
			menuCall = &(*calls)[i]
			break
		}
	}

	if menuCall == nil {
		t.Fatal("expected /sendMessage call for menu")
	}

	rawMarkup, ok := menuCall.Body["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("expected reply_markup in sendMessage, got: %v", menuCall.Body["reply_markup"])
	}

	inlineKb, ok := rawMarkup["inline_keyboard"].([]any)
	if !ok || len(inlineKb) == 0 {
		t.Fatalf("expected inline_keyboard array, got: %v", rawMarkup)
	}

	// Verify buttons
	buttons := inlineKb[0].([]any)
	var callbackData []string
	for _, b := range buttons {
		btnMap := b.(map[string]any)
		callbackData = append(callbackData, btnMap["callback_data"].(string))
	}

	hasPhoto := false
	hasStatus := false
	for _, cd := range callbackData {
		if cd == "photo" {
			hasPhoto = true
		}
		if cd == "status" {
			hasStatus = true
		}
	}

	if !hasPhoto || !hasStatus {
		t.Fatalf("expected both 'photo' and 'status' inline buttons, got: %v", callbackData)
	}
}

func TestHandleStart_SetsReplyKeyboardAndShowsMenu(t *testing.T) {
	app, calls, mu := setupTestApp(t, nil)

	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   10,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: "/start",
		},
	}

	app.HandleStart(context.Background(), app.Bot(), update)

	mu.Lock()
	defer mu.Unlock()

	var sendMessages []tgCall
	for _, c := range *calls {
		if strings.HasSuffix(c.Endpoint, "/sendMessage") {
			sendMessages = append(sendMessages, c)
		}
	}

	if len(sendMessages) < 2 {
		t.Fatalf("expected at least 2 sendMessage calls (/start greeting with reply keyboard + menu), got %d", len(sendMessages))
	}

	// First message should have reply_keyboard with persistent keyboard buttons
	startMsg := sendMessages[0]
	rawMarkup, ok := startMsg.Body["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("expected reply_markup in start message, got: %v", startMsg.Body)
	}

	kb, ok := rawMarkup["keyboard"].([]any)
	if !ok || len(kb) == 0 {
		t.Fatalf("expected keyboard array in reply_markup, got: %v", rawMarkup)
	}

	firstRow := kb[0].([]any)
	var buttonTexts []string
	for _, b := range firstRow {
		btnMap := b.(map[string]any)
		buttonTexts = append(buttonTexts, btnMap["text"].(string))
	}

	hasPhotoText := false
	hasStatusText := false
	for _, text := range buttonTexts {
		if strings.Contains(text, "Фото") {
			hasPhotoText = true
		}
		if strings.Contains(text, "Статус") {
			hasStatusText = true
		}
	}

	if !hasPhotoText || !hasStatusText {
		t.Fatalf("expected reply keyboard to contain 'Фото' and 'Статус', got: %v", buttonTexts)
	}
}

func TestHandleCallbackStatus_AnswersCallbackAndSendsStatus(t *testing.T) {
	app, calls, mu := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(contract.StatusResponse{
				Camera: contract.CameraStatus{Online: true},
				Storage: contract.StorageStatus{
					FreeBytes:  10000000,
					TotalBytes: 20000000,
				},
			})
			return
		}
	})

	update := &models.Update{
		ID: 1,
		CallbackQuery: &models.CallbackQuery{
			ID: "cb123",
			Message: models.MaybeInaccessibleMessage{
				Message: &models.Message{
					ID:   20,
					Chat: models.Chat{ID: 100},
				},
			},
			Data: "status",
		},
	}

	app.HandleCallbackStatus(context.Background(), app.Bot(), update)

	mu.Lock()
	defer mu.Unlock()

	answered := false
	statusSent := false
	for _, c := range *calls {
		if strings.HasSuffix(c.Endpoint, "/answerCallbackQuery") {
			answered = true
		}
		if strings.HasSuffix(c.Endpoint, "/sendMessage") {
			statusSent = true
			text, _ := c.Body["text"].(string)
			if !strings.Contains(text, "Статус системы") {
				t.Errorf("expected status text in message, got %q", text)
			}
		}
	}

	if !answered {
		t.Fatal("expected AnswerCallbackQuery to be called")
	}
	if !statusSent {
		t.Fatal("expected status message to be sent")
	}
}

func TestHandleCallbackPhoto_AnswersCallbackAndSendsPhoto(t *testing.T) {
	app, calls, mu := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/photo" {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46})
			return
		}
	})

	update := &models.Update{
		ID: 1,
		CallbackQuery: &models.CallbackQuery{
			ID: "cb456",
			Message: models.MaybeInaccessibleMessage{
				Message: &models.Message{
					ID:   21,
					Chat: models.Chat{ID: 100},
				},
			},
			Data: "photo",
		},
	}

	app.HandleCallbackPhoto(context.Background(), app.Bot(), update)

	mu.Lock()
	defer mu.Unlock()

	answered := false
	photoSent := false
	for _, c := range *calls {
		if strings.HasSuffix(c.Endpoint, "/answerCallbackQuery") {
			answered = true
		}
		if strings.HasSuffix(c.Endpoint, "/sendPhoto") {
			photoSent = true
		}
	}

	if !answered {
		t.Fatal("expected AnswerCallbackQuery to be called")
	}
	if !photoSent {
		t.Fatal("expected photo to be sent")
	}
}

func TestAppRun_RegistersBotCommands(t *testing.T) {
	commandsDone := make(chan struct{}, 1)
	app, calls, mu := setupTestApp(t, nil, func(endpoint string) {
		if strings.HasSuffix(endpoint, "/setMyCommands") {
			select {
			case commandsDone <- struct{}{}:
			default:
			}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-commandsDone
		cancel()
	}()

	_ = app.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	var setCommandsCall *tgCall
	for i := range *calls {
		if strings.HasSuffix((*calls)[i].Endpoint, "/setMyCommands") {
			setCommandsCall = &(*calls)[i]
			break
		}
	}

	if setCommandsCall == nil {
		t.Fatal("expected /setMyCommands call on app.Run")
	}

	rawCmds, ok := setCommandsCall.Body["commands"].([]any)
	if !ok || len(rawCmds) < 3 {
		t.Fatalf("expected at least 3 bot commands, got: %v", setCommandsCall.Body)
	}

	var cmdNames []string
	for _, c := range rawCmds {
		cMap := c.(map[string]any)
		cmdNames = append(cmdNames, cMap["command"].(string))
	}

	expected := map[string]bool{"photo": false, "status": false, "menu": false}
	for _, name := range cmdNames {
		expected[name] = true
	}

	for cmd, found := range expected {
		if !found {
			t.Errorf("expected command %q in setMyCommands, got %v", cmd, cmdNames)
		}
	}
}

func TestHandleStatus_HasInlineButtons(t *testing.T) {
	app, calls, mu := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(contract.StatusResponse{
				Camera: contract.CameraStatus{Online: true},
				Storage: contract.StorageStatus{
					FreeBytes:  500,
					TotalBytes: 1000,
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
			Text: "📊 Статус",
		},
	}

	app.HandleStatus(context.Background(), app.Bot(), update)

	mu.Lock()
	defer mu.Unlock()

	var statusCall *tgCall
	for i := range *calls {
		if strings.HasSuffix((*calls)[i].Endpoint, "/sendMessage") {
			statusCall = &(*calls)[i]
			break
		}
	}

	if statusCall == nil {
		t.Fatal("expected /sendMessage call for status")
	}

	rawMarkup, ok := statusCall.Body["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("expected reply_markup in status message, got: %v", statusCall.Body)
	}

	inlineKb, ok := rawMarkup["inline_keyboard"].([]any)
	if !ok || len(inlineKb) == 0 {
		t.Fatalf("expected inline_keyboard in status message, got: %v", rawMarkup)
	}
}

func TestHandlePhoto_HasInlineButtons(t *testing.T) {
	app, calls, mu := setupTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/photo" {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46})
			return
		}
	})

	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   10,
			From: &models.User{ID: 100, Username: "user"},
			Chat: models.Chat{ID: 100},
			Text: "📷 Фото",
		},
	}

	app.HandlePhoto(context.Background(), app.Bot(), update)

	mu.Lock()
	defer mu.Unlock()

	var photoCall *tgCall
	for i := range *calls {
		if strings.HasSuffix((*calls)[i].Endpoint, "/sendPhoto") {
			photoCall = &(*calls)[i]
			break
		}
	}

	if photoCall == nil {
		t.Fatal("expected /sendPhoto call")
	}

	rawMarkup, ok := photoCall.Body["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("expected reply_markup in photo message, got: %v", photoCall.Body)
	}

	inlineKb, ok := rawMarkup["inline_keyboard"].([]any)
	if !ok || len(inlineKb) == 0 {
		t.Fatalf("expected inline_keyboard in photo message, got: %v", rawMarkup)
	}
}
