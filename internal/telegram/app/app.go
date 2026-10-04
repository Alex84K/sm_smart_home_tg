package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
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
}

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

	b.RegisterHandler(bot.HandlerTypeMessageText, "/clip", bot.MatchTypePrefix, app.HandleClip)
	b.RegisterHandler(bot.HandlerTypeMessageText, "🎬 Клип 30с", bot.MatchTypeExact, app.HandleClipDefault)
	b.RegisterHandler(bot.HandlerTypeMessageText, "клип", bot.MatchTypeExact, app.HandleClipDefault)
	b.RegisterHandler(bot.HandlerTypeMessageText, "Клип", bot.MatchTypeExact, app.HandleClipDefault)

	b.RegisterHandler(bot.HandlerTypeMessageText, "/rec", bot.MatchTypePrefix, app.HandleRec)
	b.RegisterHandler(bot.HandlerTypeMessageText, "⏺ Запись", bot.MatchTypeExact, app.HandleRecToggle)
	b.RegisterHandler(bot.HandlerTypeMessageText, "запись", bot.MatchTypeExact, app.HandleRecToggle)
	b.RegisterHandler(bot.HandlerTypeMessageText, "Запись", bot.MatchTypeExact, app.HandleRecToggle)

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "photo", bot.MatchTypeExact, app.HandleCallbackPhoto)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "status", bot.MatchTypeExact, app.HandleCallbackStatus)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "clip_30", bot.MatchTypeExact, app.HandleCallbackClip30)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "rec_toggle", bot.MatchTypeExact, app.HandleCallbackRecToggle)

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
			{Command: "clip", Description: "Записать клип (по умолчанию 30с)"},
			{Command: "rec", Description: "Управление записью (on/off)"},
			{Command: "status", Description: "Статус системы (камера, запись, архив, диск)"},
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
				{Text: "🎬 Клип 30с"},
			},
			{
				{Text: "📊 Статус"},
				{Text: "⏺ Запись"},
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
				{Text: "🎬 Клип 30с", CallbackData: "clip_30"},
			},
			{
				{Text: "📊 Статус", CallbackData: "status"},
				{Text: "⏺ Запись", CallbackData: "rec_toggle"},
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
			},
			{
				{Text: "🎬 Клип 30с", CallbackData: "clip_30"},
				{Text: "⏺ Запись", CallbackData: "rec_toggle"},
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
				{Text: "🎬 Клип 30с", CallbackData: "clip_30"},
			},
			{
				{Text: "📊 Статус", CallbackData: "status"},
				{Text: "⏺ Запись", CallbackData: "rec_toggle"},
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

// HandleClip handles /clip [sec] command.
func (a *App) HandleClip(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := getChatID(update)
	if chatID == 0 {
		return
	}

	text := ""
	if update.Message != nil {
		text = update.Message.Text
	}
	arg := strings.TrimSpace(strings.TrimPrefix(text, "/clip"))

	sec := 30
	if arg != "" {
		parsed, err := strconv.Atoi(arg)
		if err != nil || parsed <= 0 {
			a.sendSimpleMessage(ctx, b, chatID, "⚠️ Длительность клипа должна быть положительным числом секунд (например, /clip 30).")
			return
		}
		sec = parsed
	}

	a.sendClipAction(ctx, b, chatID, sec)
}

// HandleClipDefault handles text message «🎬 Клип 30с».
func (a *App) HandleClipDefault(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := getChatID(update)
	if chatID == 0 {
		return
	}
	a.sendClipAction(ctx, b, chatID, 30)
}

// HandleCallbackClip30 handles clip_30 inline button click.
func (a *App) HandleCallbackClip30(ctx context.Context, b *bot.Bot, update *models.Update) {
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

	a.sendClipAction(ctx, b, chatID, 30)
}

func (a *App) sendClipAction(ctx context.Context, b *bot.Bot, chatID int64, sec int) {
	actionCtx, cancelAction := context.WithCancel(ctx)
	defer cancelAction()

	go func() {
		_, _ = b.SendChatAction(actionCtx, &bot.SendChatActionParams{
			ChatID: chatID,
			Action: models.ChatActionUploadVideo,
		})
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-actionCtx.Done():
				return
			case <-ticker.C:
				_, _ = b.SendChatAction(actionCtx, &bot.SendChatActionParams{
					ChatID: chatID,
					Action: models.ChatActionUploadVideo,
				})
			}
		}
	}()

	const maxClipBytes = 50 * 1024 * 1024 // 50 MB Telegram Bot API limit
	clipBytes, err := a.coreClient.GetClip(ctx, sec, maxClipBytes)
	cancelAction()

	if err != nil {
		if errors.Is(err, coreclient.ErrRecorderDisabled) {
			a.sendSimpleMessage(ctx, b, chatID, "⚠️ Запись выключена. Включите запись (/rec on), чтобы сохранять клипы.")
			return
		}
		if errors.Is(err, coreclient.ErrNoSegments) {
			a.sendSimpleMessage(ctx, b, chatID, "⚠️ Нет доступных записей для создания клипа. Возможно, запись только началась.")
			return
		}
		if errors.Is(err, coreclient.ErrCameraOffline) {
			a.sendSimpleMessage(ctx, b, chatID, "📷 Камера сейчас недоступна. Попробуйте позже.")
			return
		}
		var clipTooLarge *coreclient.ClipTooLargeError
		if errors.As(err, &clipTooLarge) {
			if clipTooLarge.MaxAllowedSeconds > 0 {
				a.sendSimpleMessage(ctx, b, chatID, fmt.Sprintf("⚠️ Клип не помещается в лимит (50 МБ). Максимальная длительность: ~%d сек.", clipTooLarge.MaxAllowedSeconds))
			} else {
				a.sendSimpleMessage(ctx, b, chatID, "⚠️ Клип не помещается в лимит (50 МБ). Попробуйте запросить меньшую длительность.")
			}
			return
		}
		if errors.Is(err, coreclient.ErrClipFailed) {
			a.sendSimpleMessage(ctx, b, chatID, "⚠️ Не удалось собрать клип. Попробуйте ещё раз.")
			return
		}
		if errors.Is(err, coreclient.ErrCoreUnavailable) {
			a.sendSimpleMessage(ctx, b, chatID, "⚠️ Ядро умного дома недоступно. Попробуйте позже.")
			return
		}

		a.logger.Error("failed to get clip from core", slog.Any("err", err))
		a.sendSimpleMessage(ctx, b, chatID, fmt.Sprintf("⚠️ Не удалось получить клип: %v", err))
		return
	}

	width, height, duration := probeMP4(clipBytes, 1920, 1080, sec)

	clipKeyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "🎬 Ещё клип 30с", CallbackData: "clip_30"},
				{Text: "📷 Фото", CallbackData: "photo"},
			},
			{
				{Text: "📊 Статус", CallbackData: "status"},
				{Text: "⏺ Запись", CallbackData: "rec_toggle"},
			},
		},
	}

	if _, err := b.SendVideo(ctx, &bot.SendVideoParams{
		ChatID: chatID,
		Video: &models.InputFileUpload{
			Filename: fmt.Sprintf("clip_%ds.mp4", duration),
			Data:     bytes.NewReader(clipBytes),
		},
		Duration:          duration,
		Width:             width,
		Height:            height,
		SupportsStreaming: true,
		Caption:           fmt.Sprintf("🎬 Клип (%dс)", duration),
		ReplyMarkup:       clipKeyboard,
	}); err != nil {
		a.logger.Error("failed to send video", slog.Any("err", err), slog.Int64("chat_id", chatID))
	}
}

// HandleRec handles /rec [on|off].
func (a *App) HandleRec(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := getChatID(update)
	if chatID == 0 {
		return
	}

	text := ""
	if update.Message != nil {
		text = update.Message.Text
	}
	arg := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(text, "/rec")))

	switch arg {
	case "on":
		a.setRecorderAction(ctx, b, chatID, true)
	case "off":
		a.setRecorderAction(ctx, b, chatID, false)
	case "":
		a.toggleRecorderAction(ctx, b, chatID)
	default:
		a.sendSimpleMessage(ctx, b, chatID, "Использование: /rec on или /rec off (или нажмите кнопку «⏺ Запись»)")
	}
}

// HandleRecToggle handles text message «⏺ Запись».
func (a *App) HandleRecToggle(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := getChatID(update)
	if chatID == 0 {
		return
	}
	a.toggleRecorderAction(ctx, b, chatID)
}

// HandleCallbackRecToggle handles rec_toggle inline button click.
func (a *App) HandleCallbackRecToggle(ctx context.Context, b *bot.Bot, update *models.Update) {
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
	a.toggleRecorderAction(ctx, b, chatID)
}

func (a *App) setRecorderAction(ctx context.Context, b *bot.Bot, chatID int64, enabled bool) {
	st, err := a.coreClient.SetRecorder(ctx, enabled)
	if err != nil {
		a.logger.Error("failed to set recorder state", slog.Any("err", err))
		a.sendSimpleMessage(ctx, b, chatID, "⚠️ Ядро умного дома недоступно. Попробуйте позже.")
		return
	}

	var msg string
	if st.Enabled {
		msg = "⏺ Запись включена"
	} else {
		msg = "⏹ Запись выключена"
	}
	a.sendSimpleMessage(ctx, b, chatID, msg)
}

func (a *App) toggleRecorderAction(ctx context.Context, b *bot.Bot, chatID int64) {
	st, err := a.coreClient.GetStatus(ctx)
	if err != nil {
		a.logger.Error("failed to get status for recorder toggle", slog.Any("err", err))
		a.sendSimpleMessage(ctx, b, chatID, "⚠️ Ядро умного дома недоступно. Попробуйте позже.")
		return
	}

	nextState := true
	if st.Recorder != nil {
		nextState = !st.Recorder.Enabled
	}
	a.setRecorderAction(ctx, b, chatID, nextState)
}

func (a *App) sendSimpleMessage(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	}); err != nil {
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

	var recText string
	if st.Recorder != nil {
		if st.Recorder.Enabled {
			recText = "🟢 Включена"
		} else {
			recText = "⏹ Выключена"
		}
	} else {
		recText = "⚪️ Не настроена"
	}

	var archiveText string
	if st.Archive != nil {
		if st.Archive.Ok {
			pending := ""
			if st.Archive.PendingSegments != nil && *st.Archive.PendingSegments > 0 {
				pending = fmt.Sprintf(" (в очереди: %d)", *st.Archive.PendingSegments)
			}
			archiveText = "🟢 Работает" + pending
		} else {
			errStr := "ошибка"
			if st.Archive.Error != nil && *st.Archive.Error != "" {
				errStr = *st.Archive.Error
			}
			archiveText = fmt.Sprintf("🔴 Ошибка (%s)", errStr)
		}
	} else {
		archiveText = "⚪️ Не настроен"
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

	return fmt.Sprintf("📊 Статус системы:\n\n📷 Камера: %s\n⏺ Запись: %s\n📦 Архив: %s\n💾 Диск: %s",
		camText, recText, archiveText, storageText)
}
