package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Alex84K/sm_smart_home_core_go/contract"
	"github.com/Alex84K/sm_smart_home_tg/internal/coreclient"
	"github.com/Alex84K/sm_smart_home_tg/internal/platform/config"
)

// Clock provides current time, abstracted for deterministic testing (CONVENTIONS.md).
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

// BotErrorsHandler creates a bot.ErrorsHandler that warns about 409 Conflict at most once per minute (D6).
func BotErrorsHandler(logger *slog.Logger, clock Clock) bot.ErrorsHandler {
	if clock == nil {
		clock = realClock{}
	}
	var mu sync.Mutex
	var lastConflictWarn time.Time

	return func(err error) {
		if errors.Is(err, bot.ErrorConflict) {
			mu.Lock()
			now := clock.Now()
			shouldLog := now.Sub(lastConflictWarn) >= time.Minute
			if shouldLog {
				lastConflictWarn = now
			}
			mu.Unlock()

			if shouldLog && logger != nil {
				logger.Warn("getUpdates conflict: another tg-gateway instance is running with this token", slog.Any("err", err))
			}
			return
		}

		if logger != nil {
			logger.Error("telegram bot internal error", slog.Any("err", err))
		}
	}
}

// App is the composition root for the Telegram gateway service (ADR-0016).
type App struct {
	bot        *bot.Bot
	coreClient *coreclient.Client
	logger     *slog.Logger

	// afterFunc schedules the auto-stop of a clip; replaced in tests.
	afterFunc func(d time.Duration, f func()) *time.Timer

	clipMu sync.Mutex
	clips  map[string]*pendingClip
}

// pendingClip is a clip recording started from this gateway, waiting for «Стоп» or the max duration.
type pendingClip struct {
	chatID    int64
	messageID int
	timer     *time.Timer
	stopping  bool
}

const clipStopPrefix = "clip_stop:"

// New creates and initializes the Telegram gateway bot application.
func New(cfg *config.TGConfig, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}

	client, err := coreclient.New(cfg.CoreAPIURL, cfg.CoreAPIToken)
	if err != nil {
		return nil, fmt.Errorf("tg-app: coreclient: %w", err)
	}

	app := &App{
		coreClient: client,
		logger:     logger,
		afterFunc:  time.AfterFunc,
		clips:      make(map[string]*pendingClip),
	}

	opts := []bot.Option{
		bot.WithServerURL(cfg.TelegramAPIURL),
		bot.WithMiddlewares(WhitelistMiddleware(cfg.AllowedIDs, logger)),
		bot.WithErrorsHandler(BotErrorsHandler(logger, realClock{})),
	}

	b, err := bot.New(cfg.TelegramBotToken, opts...)
	if err != nil {
		return nil, fmt.Errorf("tg-app: telegram bot: %w", err)
	}

	app.bot = b

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, app.HandleStart)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/menu", bot.MatchTypeExact, app.HandleMenu)
	b.RegisterHandler(bot.HandlerTypeMessageText, "🏠 Меню", bot.MatchTypeExact, app.HandleMenu)
	b.RegisterHandler(bot.HandlerTypeMessageText, "меню", bot.MatchTypeExact, app.HandleMenu)
	b.RegisterHandler(bot.HandlerTypeMessageText, "Меню", bot.MatchTypeExact, app.HandleMenu)

	b.RegisterHandler(bot.HandlerTypeMessageText, "/status", bot.MatchTypeExact, app.HandleStatus)
	b.RegisterHandler(bot.HandlerTypeMessageText, "📊 Статус", bot.MatchTypeExact, app.HandleStatus)
	b.RegisterHandler(bot.HandlerTypeMessageText, "статус", bot.MatchTypeExact, app.HandleStatus)
	b.RegisterHandler(bot.HandlerTypeMessageText, "Статус", bot.MatchTypeExact, app.HandleStatus)

	b.RegisterHandler(bot.HandlerTypeMessageText, "/photo", bot.MatchTypeExact, app.HandlePhoto)
	b.RegisterHandler(bot.HandlerTypeMessageText, "📷 Фото", bot.MatchTypeExact, app.HandlePhoto)
	b.RegisterHandler(bot.HandlerTypeMessageText, "фото", bot.MatchTypeExact, app.HandlePhoto)
	b.RegisterHandler(bot.HandlerTypeMessageText, "Фото", bot.MatchTypeExact, app.HandlePhoto)

	b.RegisterHandler(bot.HandlerTypeMessageText, "/clip", bot.MatchTypeExact, app.HandleClip)
	b.RegisterHandler(bot.HandlerTypeMessageText, "🎬 Клип", bot.MatchTypeExact, app.HandleClip)
	b.RegisterHandler(bot.HandlerTypeMessageText, "клип", bot.MatchTypeExact, app.HandleClip)
	b.RegisterHandler(bot.HandlerTypeMessageText, "Клип", bot.MatchTypeExact, app.HandleClip)

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "photo", bot.MatchTypeExact, app.HandleCallbackPhoto)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "clip", bot.MatchTypeExact, app.HandleCallbackClip)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, clipStopPrefix, bot.MatchTypePrefix, app.HandleCallbackClipStop)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "status", bot.MatchTypeExact, app.HandleCallbackStatus)

	return app, nil
}

// NewWithDeps creates an App with custom dependencies (e.g. for testing).
func NewWithDeps(coreClient *coreclient.Client, logger *slog.Logger) *App {
	if logger == nil {
		logger = slog.Default()
	}
	return &App{
		coreClient: coreClient,
		logger:     logger,
		afterFunc:  time.AfterFunc,
		clips:      make(map[string]*pendingClip),
	}
}

// SetBot sets the bot instance on App.
func (a *App) SetBot(b *bot.Bot) {
	a.bot = b
}

// Bot returns the underlying telegram bot instance.
func (a *App) Bot() *bot.Bot {
	return a.bot
}

// Run starts the long-polling Telegram bot (run.Runner).
func (a *App) Run(ctx context.Context) error {
	a.logger.Info("starting telegram gateway bot")

	if _, err := a.bot.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{Command: "photo", Description: "Сделать снимок с камеры"},
			{Command: "clip", Description: "Записать клип (старт/стоп)"},
			{Command: "status", Description: "Статус системы (камера, диск)"},
			{Command: "menu", Description: "Главное меню"},
		},
	}); err != nil {
		a.logger.Warn("failed to set telegram bot commands", slog.Any("err", err))
	}

	a.bot.Start(ctx)
	a.logger.Info("telegram gateway bot stopped")
	return nil
}

func defaultReplyKeyboard() *models.ReplyKeyboardMarkup {
	return &models.ReplyKeyboardMarkup{
		Keyboard: [][]models.KeyboardButton{
			{
				{Text: "📷 Фото"},
				{Text: "🎬 Клип"},
				{Text: "📊 Статус"},
			},
		},
		ResizeKeyboard:        true,
		IsPersistent:          true,
		InputFieldPlaceholder: "Выберите действие...",
	}
}

// HandleStart handles /start command, setting up reply keyboard and showing menu.
func (a *App) HandleStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := getChatID(update)
	if chatID == 0 {
		return
	}

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        "🏡 Добро пожаловать в Simple Smart Home!\n\nКнопки быстрого доступа закреплены внизу экрана.",
		ReplyMarkup: defaultReplyKeyboard(),
	}); err != nil {
		a.logger.Error("failed to send telegram start message", slog.Any("err", err), slog.Int64("chat_id", chatID))
	}

	a.HandleMenu(ctx, b, update)
}

// HandleMenu handles /menu command.
func (a *App) HandleMenu(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := getChatID(update)
	if chatID == 0 {
		return
	}

	menuKeyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "📷 Фото", CallbackData: "photo"},
				{Text: "🎬 Клип", CallbackData: "clip"},
				{Text: "📊 Статус", CallbackData: "status"},
			},
		},
	}

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        "🏡 Simple Smart Home\n\nВыберите действие:",
		ReplyMarkup: menuKeyboard,
	}); err != nil {
		a.logger.Error("failed to send telegram menu message", slog.Any("err", err), slog.Int64("chat_id", chatID))
	}
}

// HandleStatus handles /status command.
func (a *App) HandleStatus(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := getChatID(update)
	if chatID == 0 {
		return
	}

	a.sendStatusAction(ctx, b, chatID)
}

// HandleCallbackStatus handles status inline button click.
func (a *App) HandleCallbackStatus(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}
	chatID := getChatID(update)
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	}); err != nil {
		a.logger.Error("failed to answer callback query", slog.Any("err", err), slog.Int64("chat_id", chatID))
	}

	if chatID == 0 {
		return
	}

	a.sendStatusAction(ctx, b, chatID)
}

func (a *App) sendStatusAction(ctx context.Context, b *bot.Bot, chatID int64) {
	st, err := a.coreClient.GetStatus(ctx)
	if err != nil {
		a.logger.Error("failed to get status from core", slog.Any("err", err))
		if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "⚠️ Ядро умного дома недоступно. Попробуйте позже.",
		}); sendErr != nil {
			a.logger.Error("failed to send telegram status error message", slog.Any("err", sendErr), slog.Int64("chat_id", chatID))
		}
		return
	}

	msg := formatStatusMessage(st)
	statusKeyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "🔄 Обновить", CallbackData: "status"},
				{Text: "📷 Фото", CallbackData: "photo"},
				{Text: "🎬 Клип", CallbackData: "clip"},
			},
		},
	}

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        msg,
		ReplyMarkup: statusKeyboard,
	}); err != nil {
		a.logger.Error("failed to send telegram status message", slog.Any("err", err), slog.Int64("chat_id", chatID))
	}
}

// HandlePhoto handles /photo command.
func (a *App) HandlePhoto(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := getChatID(update)
	if chatID == 0 {
		return
	}

	a.sendPhotoAction(ctx, b, chatID)
}

// HandleCallbackPhoto handles photo inline button click.
func (a *App) HandleCallbackPhoto(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}
	chatID := getChatID(update)
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	}); err != nil {
		a.logger.Error("failed to answer callback query", slog.Any("err", err), slog.Int64("chat_id", chatID))
	}

	if chatID == 0 {
		return
	}

	a.sendPhotoAction(ctx, b, chatID)
}

func (a *App) sendPhotoAction(ctx context.Context, b *bot.Bot, chatID int64) {
	if _, err := b.SendChatAction(ctx, &bot.SendChatActionParams{
		ChatID: chatID,
		Action: models.ChatActionUploadPhoto,
	}); err != nil {
		a.logger.Error("failed to send chat action", slog.Any("err", err), slog.Int64("chat_id", chatID))
	}

	photoBytes, err := a.coreClient.GetPhoto(ctx)
	if err != nil {
		if errors.Is(err, coreclient.ErrCameraOffline) {
			if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: chatID,
				Text:   "📷 Камера сейчас недоступна. Попробуйте позже.",
			}); sendErr != nil {
				a.logger.Error("failed to send telegram offline message", slog.Any("err", sendErr), slog.Int64("chat_id", chatID))
			}
			return
		}
		a.logger.Error("failed to get photo from core", slog.Any("err", err))
		if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "⚠️ Ядро умного дома недоступно. Попробуйте позже.",
		}); sendErr != nil {
			a.logger.Error("failed to send telegram core error message", slog.Any("err", sendErr), slog.Int64("chat_id", chatID))
		}
		return
	}

	photoKeyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "🔄 Ещё фото", CallbackData: "photo"},
				{Text: "🎬 Клип", CallbackData: "clip"},
				{Text: "📊 Статус", CallbackData: "status"},
			},
		},
	}

	if _, err := b.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID: chatID,
		Photo: &models.InputFileUpload{
			Filename: "photo.jpg",
			Data:     bytes.NewReader(photoBytes),
		},
		ReplyMarkup: photoKeyboard,
	}); err != nil {
		a.logger.Error("failed to send photo", slog.Any("err", err), slog.Int64("chat_id", chatID))
	}
}

// HandleClip handles /clip and the «🎬 Клип» reply button: starts a clip recording.
func (a *App) HandleClip(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := getChatID(update)
	if chatID == 0 {
		return
	}
	a.startClip(ctx, b, chatID)
}

// HandleCallbackClip handles the «🎬 Клип» inline button.
func (a *App) HandleCallbackClip(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}
	chatID := getChatID(update)
	a.answerCallback(ctx, b, update.CallbackQuery.ID, "")
	if chatID == 0 {
		return
	}
	a.startClip(ctx, b, chatID)
}

// HandleCallbackClipStop handles the «⏹ Стоп» button of a recording message.
func (a *App) HandleCallbackClipStop(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	id := strings.TrimPrefix(cq.Data, clipStopPrefix)
	chatID := getChatID(update)
	messageID := 0
	if cq.Message.Message != nil {
		messageID = cq.Message.Message.ID
	}

	a.clipMu.Lock()
	pc, ok := a.clips[id]
	if ok && pc.stopping {
		a.clipMu.Unlock()
		a.answerCallback(ctx, b, cq.ID, "Видео уже готовится")
		return
	}
	if ok {
		pc.stopping = true
		pc.timer.Stop()
	}
	a.clipMu.Unlock()

	a.answerCallback(ctx, b, cq.ID, "")
	if chatID == 0 {
		return
	}
	// Unknown here after a gateway restart: the button still carries the id, so the core is asked anyway.
	a.finishClip(ctx, b, id, chatID, messageID)
}

func (a *App) startClip(ctx context.Context, b *bot.Bot, chatID int64) {
	rec, err := a.coreClient.StartClip(ctx)
	if err != nil {
		switch {
		case errors.Is(err, coreclient.ErrClipBusy):
			a.sendSimpleMessage(ctx, b, chatID, "⏺ Уже идёт запись — дождитесь видео.")
		case errors.Is(err, coreclient.ErrCameraOffline):
			a.sendSimpleMessage(ctx, b, chatID, "📷 Камера сейчас недоступна. Попробуйте позже.")
		default:
			a.logger.Error("failed to start clip", slog.Any("err", err))
			a.sendSimpleMessage(ctx, b, chatID, "⚠️ Ядро умного дома недоступно. Попробуйте позже.")
		}
		return
	}

	msg, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("⏺ Идёт запись… (до %d с)\nНажмите «⏹ Стоп», чтобы закончить.", int(rec.MaxDuration.Seconds())),
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{{Text: "⏹ Стоп", CallbackData: clipStopPrefix + rec.ID}},
			},
		},
	})
	messageID := 0
	if err != nil {
		a.logger.Error("failed to send clip recording message", slog.Any("err", err), slog.Int64("chat_id", chatID))
	} else {
		messageID = msg.ID
	}

	pc := &pendingClip{chatID: chatID, messageID: messageID}
	a.clipMu.Lock()
	a.clips[rec.ID] = pc
	// The core stops ffmpeg at the max duration by itself; the gateway then fetches and sends the video.
	pc.timer = a.afterFunc(rec.MaxDuration, func() { a.autoStopClip(b, rec.ID) })
	a.clipMu.Unlock()
}

func (a *App) autoStopClip(b *bot.Bot, id string) {
	a.clipMu.Lock()
	pc, ok := a.clips[id]
	if !ok || pc.stopping {
		a.clipMu.Unlock()
		return
	}
	pc.stopping = true
	a.clipMu.Unlock()

	a.finishClip(context.Background(), b, id, pc.chatID, pc.messageID)
}

// finishClip stops the recording in the core, sends the video and removes the recording message.
func (a *App) finishClip(ctx context.Context, b *bot.Bot, id string, chatID int64, messageID int) {
	defer func() {
		a.clipMu.Lock()
		delete(a.clips, id)
		a.clipMu.Unlock()
	}()

	if messageID != 0 {
		if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      "⏳ Готовлю видео…",
		}); err != nil {
			a.logger.Warn("failed to edit clip recording message", slog.Any("err", err), slog.Int64("chat_id", chatID))
		}
	}
	if _, err := b.SendChatAction(ctx, &bot.SendChatActionParams{
		ChatID: chatID,
		Action: models.ChatActionUploadVideo,
	}); err != nil {
		a.logger.Error("failed to send chat action", slog.Any("err", err), slog.Int64("chat_id", chatID))
	}

	data, err := a.coreClient.StopClip(ctx, id)
	if err != nil {
		var text string
		switch {
		case errors.Is(err, coreclient.ErrClipNotFound):
			text = "Запись уже остановлена."
		case errors.Is(err, coreclient.ErrClipEmpty):
			text = "⚠️ Клип пустой: камера не успела отдать видео. Попробуйте записать подольше."
		case errors.Is(err, coreclient.ErrClipFailed):
			text = "⚠️ Не удалось записать клип. Попробуйте ещё раз."
		default:
			a.logger.Error("failed to stop clip", slog.String("clip_id", id), slog.Any("err", err))
			text = "⚠️ Ядро умного дома недоступно. Попробуйте позже."
		}
		a.replaceClipMessage(ctx, b, chatID, messageID, text)
		return
	}

	width, height, duration := probeMP4(data, 1280, 720, 0)
	if _, err := b.SendVideo(ctx, &bot.SendVideoParams{
		ChatID:            chatID,
		Video:             &models.InputFileUpload{Filename: "clip.mp4", Data: bytes.NewReader(data)},
		Width:             width,
		Height:            height,
		Duration:          duration,
		SupportsStreaming: true,
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "🎬 Ещё клип", CallbackData: "clip"},
					{Text: "📷 Фото", CallbackData: "photo"},
				},
			},
		},
	}); err != nil {
		a.logger.Error("failed to send clip video", slog.Any("err", err), slog.Int64("chat_id", chatID))
		a.replaceClipMessage(ctx, b, chatID, messageID, "⚠️ Не удалось отправить видео в Telegram.")
		return
	}

	if messageID != 0 {
		if _, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: messageID}); err != nil {
			a.logger.Warn("failed to delete clip recording message", slog.Any("err", err), slog.Int64("chat_id", chatID))
		}
	}
}

// replaceClipMessage shows the outcome in place of the recording message, or as a new one if there is none.
func (a *App) replaceClipMessage(ctx context.Context, b *bot.Bot, chatID int64, messageID int, text string) {
	if messageID != 0 {
		if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      text,
		}); err == nil {
			return
		}
	}
	a.sendSimpleMessage(ctx, b, chatID, text)
}

func (a *App) answerCallback(ctx context.Context, b *bot.Bot, callbackID, text string) {
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackID,
		Text:            text,
	}); err != nil {
		a.logger.Error("failed to answer callback query", slog.Any("err", err))
	}
}

func (a *App) sendSimpleMessage(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text}); err != nil {
		a.logger.Error("failed to send telegram message", slog.Any("err", err), slog.Int64("chat_id", chatID))
	}
}

func getChatID(update *models.Update) int64 {
	if update.Message != nil {
		return update.Message.Chat.ID
	}
	if update.CallbackQuery != nil && update.CallbackQuery.Message.Message != nil {
		return update.CallbackQuery.Message.Message.Chat.ID
	}
	return 0
}

func formatBytes(b int64) string {
	const unit = 1024.0
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatStatusMessage(st *contract.StatusResponse) string {
	var camText string
	if st.Camera.Online {
		ageStr := ""
		if st.Camera.LastFrameAgeSeconds != nil {
			ageStr = fmt.Sprintf(" (кадр %.1fс назад)", *st.Camera.LastFrameAgeSeconds)
		}
		camText = "🟢 В сети" + ageStr
	} else {
		reason := "нет данных"
		if st.Camera.Error != nil && *st.Camera.Error != "" {
			reason = *st.Camera.Error
		}
		camText = fmt.Sprintf("🔴 Недоступна (%s)", reason)
	}

	var storageText string
	if st.Storage.Error != nil && *st.Storage.Error != "" {
		storageText = fmt.Sprintf("🔴 Ошибка (%s)", *st.Storage.Error)
	} else {
		storageText = fmt.Sprintf("%s свободно из %s",
			formatBytes(st.Storage.FreeBytes),
			formatBytes(st.Storage.TotalBytes),
		)
	}

	return fmt.Sprintf("📊 Статус системы:\n\n📷 Камера: %s\n💾 Диск: %s", camText, storageText)
}
