// helpers.go — обёртки над методами Telegram Bot API:
// отправка, удаление и редактирование сообщений.
package handler

import (
	"context"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// sendMessage отправляет новое сообщение в указанный чат.
//
// Гасить "message is not modified" здесь не нужно: эта ошибка приходит
// только от EditMessageText. У нового сообщения всегда новый message_id,
// «не модифицировать» его невозможно. Логика гашения — в editMessage.
func (h *Handler) sendMessage(
	ctx context.Context,
	b *bot.Bot,
	chatID int64,
	text string,
	markup models.ReplyMarkup,
	parse models.ParseMode,
) error {
	_, err := b.SendMessage(
		ctx,
		&bot.SendMessageParams{
			ChatID:      chatID,
			Text:        text,
			ReplyMarkup: markup,
			ParseMode:   parse,
		},
	)
	return err
}

// deleteMessage удаляет сообщение по его идентификатору в указанном чате.
func (h *Handler) deleteMessage(
	ctx context.Context,
	b *bot.Bot,
	chatID int64,
	messageID int,
) error {
	_, err := b.DeleteMessage(
		ctx,
		&bot.DeleteMessageParams{
			ChatID:    chatID,
			MessageID: messageID,
		},
	)
	return err
}

// editMessage редактирует текст и разметку уже отправленного сообщения.
// Если содержимое не изменилось (например, дважды подряд введён неверный
// код), Telegram возвращает ошибку "message is not modified". Для нас
// это не сбой, экран и так выглядит правильно, поэтому ошибка гасится.
func (h *Handler) editMessage(
	ctx context.Context,
	b *bot.Bot,
	chatID int64,
	messageID int,
	text string,
	markup models.ReplyMarkup,
	parse models.ParseMode,
) error {
	_, err := b.EditMessageText(
		ctx,
		&bot.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   parse,
			ReplyMarkup: markup,
		},
	)
	if err != nil && strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}
