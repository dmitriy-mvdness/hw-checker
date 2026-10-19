// callback_menu.go — главное меню и экран профиля.
package handler

import (
	"context"
	"errors"
	"fmt"
	"html"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/dmitriy-mvdness/hw-checker/internal/keyboard"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Тексты, которые используются и в callback-ах, и в командах.
const (
	menuText = "📋 <b>Меню</b>\n\n" +
		"Здесь собрано всё для учёбы: сдача ДЗ, поддержка и новости.\n\n" +
		"<b>Выбери нужный раздел 👇</b>"

	notLoggedText = "🔒 <b>Сначала войди в аккаунт</b>\n\n" +
		"Это действие доступно только после входа.\n\n" +
		"<b>Выбери нужный раздел 👇</b>"
)

// cbMainMenu показывает главное меню.
func (h *Handler) cbMainMenu(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	return h.editMessage(ctx, b, chatID, messageID, menuText, keyboard.MainMenuKeyboard(), models.ParseModeHTML)
}

// cbProfile показывает данные студента, к которому привязан аккаунт.
func (h *Handler) cbProfile(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	parseMode := models.ParseModeHTML

	student, err := h.svc.CurrentStudent(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return h.editMessage(ctx, b, chatID, messageID, notLoggedText, keyboard.AuthKeyboard(), parseMode)
	}
	if err != nil {
		return fmt.Errorf("current student: %w", err)
	}

	text := "👤 <b>Профиль</b>\n\n"
	if student.Name != "" {
		text += fmt.Sprintf("🚩 <b>%s</b>\n\n", html.EscapeString(student.Name))
	}

	if student.GroupName != "" {
		text += fmt.Sprintf("🗂️ <b>Группа:</b> %s\n", html.EscapeString(student.GroupName))
	}

	if student.StreamName != "" {
		text += fmt.Sprintf("🌊 <b>Поток:</b> %s\n", html.EscapeString(student.StreamName))
	}

	if student.PersonalCode != "" {
		text += fmt.Sprintf("🔒 <b>Персональный код:</b> <tg-spoiler>%s</tg-spoiler>\n",
			html.EscapeString(student.PersonalCode))
	}

	text += "\n<i>Если данные неверны - обратись в техподдержку.</i>"

	kb := keyboard.Empty()
	kb.InlineKeyboard = append(kb.InlineKeyboard, []models.InlineKeyboardButton{
		{Text: keyboard.BtnLogout, CallbackData: "logout"},
	})
	markup := keyboard.AddBackButton(kb, "main_menu")

	return h.editMessage(ctx, b, chatID, messageID, text, markup, parseMode)
}
