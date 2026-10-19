// album_input.go — реакция на альбомы (media group). Telegram шлёт
// каждый файл альбома отдельным апдейтом с общим media_group_id, для
// бота это выглядит как N независимых сообщений. Логика сдачи и
// поддержки рассчитана на один файл, поэтому альбомы отклоняем с
// понятной подсказкой — пользователь может прислать один файл заново.
package handler

import (
	"context"

	"github.com/dmitriy-mvdness/hw-checker/internal/session"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

// handleAlbum реагирует на альбом (media group) по текущему режиму:
//   - ждёт файл ДЗ → удаляем сообщение, на экране темы подсказка;
//   - ждёт вложение в поддержку → то же с текстом про один файл;
//   - ждёт код → молча удаляем (это не код);
//   - ничего не ждёт → игнорируем.
//
// Состояние НЕ сбрасываем: пользователь может прислать один файл и
// продолжить с того же места.
func (h *Handler) handleAlbum(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	msg := update.Message
	userID := msg.From.ID
	chatID := msg.Chat.ID

	// Альбомные апдейты летят пачкой: оставлять их в чате — мусор,
	// к тому же пользователь может подумать, что файлы ушли куда надо.
	// Удаление косметическое — Debug.
	if err := h.deleteMessage(ctx, b, chatID, msg.ID); err != nil {
		h.log.Debug("не удалось удалить альбомное сообщение",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}

	sess := h.states.Get(userID)

	warnText := "⚠️ <b>Альбомы не поддерживаются.</b>\n" +
		"Если очень хочется — можешь загрузить фото в архив, тем самым отправив файлом."

	switch sess.State {
	case session.StateWaitingFile:

		h.showThemeWithWarning(ctx, b, userID, chatID, sess, warnText)

	case session.StateWaitingSupport:

		h.askSupportAgain(ctx, b, userID, chatID, sess, warnText)

	case session.StateWaitingCode:
	// Это не код — просто удалили сообщение и всё. Пользователь
	// остаётся в режиме ввода и может отправить код.

	default:
		// Ничего не ждём. Если есть свежая открытая тема — превратим
		// экран в подсказку о продолжении сдачи (первый апдейт альбома);
		// остальные апдейты альбома попадут сюда же и просто удалятся,
		// потому что hintShown уже true.
		h.handleOrphanUpdate(ctx, b, update)
	}
}
