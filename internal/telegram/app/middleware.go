package app

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// WhitelistMiddleware blocks messages and callback queries from unauthorized users and chats.
func WhitelistMiddleware(allowedIDs []int64, logger *slog.Logger) bot.Middleware {
	allowedMap := make(map[int64]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		allowedMap[id] = true
	}

	return func(next bot.HandlerFunc) bot.HandlerFunc {
		return func(ctx context.Context, b *bot.Bot, update *models.Update) {
			var userID int64
			var chatID int64
			var username string
			var text string

			if update.Message != nil {
				if update.Message.From != nil {
					userID = update.Message.From.ID
					username = update.Message.From.Username
				}
				chatID = update.Message.Chat.ID
				text = update.Message.Text
			} else if update.CallbackQuery != nil {
				userID = update.CallbackQuery.From.ID
				username = update.CallbackQuery.From.Username
				if update.CallbackQuery.Message.Message != nil {
					chatID = update.CallbackQuery.Message.Message.Chat.ID
				}
				text = update.CallbackQuery.Data
			}

			if !allowedMap[userID] && !allowedMap[chatID] {
				if logger != nil {
					logger.Warn("unauthorized telegram update ignored",
						slog.Int64("user_id", userID),
						slog.Int64("chat_id", chatID),
						slog.String("username", username),
						slog.String("text", text),
					)
				}
				// Silent ignore per specification
				return
			}

			next(ctx, b, update)
		}
	}
}
