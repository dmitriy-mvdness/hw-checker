// callback_support.go — экраны поддержки: промежуточный, список причин,
// справка и переход в режим ожидания обращения.
package handler

import (
	"context"
	"fmt"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/dmitriy-mvdness/hw-checker/internal/keyboard"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// cbSupport показывает промежуточный экран «Помощь» с двумя действиями:
// техподдержка и справка.
func (h *Handler) cbSupport(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	text := "🤝 <b>Помощь</b>\n\n" +
		"Здесь можно написать в техподдержку или почитать справку по боту.\n\n" +
		"<b>Выбери, что нужно 👇</b>"
	return h.editMessage(ctx, b, chatID, messageID, text, keyboard.HelpMenuKeyboard(), models.ParseModeHTML)
}

// cbSupportTech показывает список причин обращения в техподдержку.
func (h *Handler) cbSupportTech(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	text := "🛠️ <b>Техподдержка</b>\n\n" +
		"Нашел баг, проблема с ДЗ или придумал крутую фишку?\n\n" +
		"<b>Напиши нам — разберемся 👇</b>"
	return h.editMessage(ctx, b, chatID, messageID, text, keyboard.SupportKeyboard(), models.ParseModeHTML)
}

// cbSupportHelp показывает справку по боту.
func (h *Handler) cbSupportHelp(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	markup := keyboard.AddBackButton(keyboard.Empty(), "support")
	return h.editMessage(ctx, b, chatID, messageID, h.helpText(), markup, models.ParseModeHTML)
}

// cbSupportReason переводит в режим ожидания текста/файла. arg — код
// причины (bug/ideas/homework/other).
func (h *Handler) cbSupportReason(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
	arg string,
) error {
	if !domain.IsSupportReason(arg) {
		return fmt.Errorf("unknown support reason: %s", arg)
	}
	reasonTitle := domain.SupportReasonTitle(arg)

	text := fmt.Sprintf(
		"🛠️ <b>Техподдержка</b>\n\n"+
			"<b>Категория:</b> %s\n\n"+
			"<b>Опиши ситуацию:</b> текстом или файлом.\n\n"+
			"<b>Правила:</b>\n"+
			"• Файл: до %d МБ\n"+
			"• Форматы: %s\n"+
			"• Скриншот можно отправить как фото — любой формат\n\n"+
			"<b>Отправь следующим сообщением.</b>",
		reasonTitle,
		h.svc.MaxFileSizeMB(),
		h.svc.AllowedExtsDisplay(),
	)
	markup := keyboard.AddBackButton(keyboard.Empty(), "support_tech")

	if err := h.editMessage(ctx, b, chatID, messageID, text, markup, models.ParseModeHTML); err != nil {
		return err
	}

	h.states.WaitSupport(userID, arg, messageID)
	return nil
}
