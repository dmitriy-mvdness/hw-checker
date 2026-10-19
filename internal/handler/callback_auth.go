// callback_auth.go — экраны авторизации: меню входа, запрос кода,
// подтверждение и выполнение выхода.
package handler

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/dmitriy-mvdness/hw-checker/internal/keyboard"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// cbAuthMenu показывает меню авторизации с кнопками действий.
func (h *Handler) cbAuthMenu(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	text := "🔑 <b>Меню авторизации</b>\n\n<b>Выбери нужный раздел 👇</b>"

	parseMode := models.ParseModeHTML

	return h.editMessage(ctx, b, chatID, messageID, text, keyboard.AuthKeyboard(), parseMode)
}

// cbUserAuth показывает экран входа и переводит пользователя в режим
// ввода кода. Если пользователь уже вошёл, режим не включается.
func (h *Handler) cbUserAuth(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	parseMode := models.ParseModeHTML

	student, err := h.svc.CurrentStudent(ctx, userID)
	switch {
	case err == nil:
		text := fmt.Sprintf("ℹ️  <b>Ты уже авторизован</b>\n\n"+
			"Сейчас активен аккаунт: <b>%s</b>\n\n"+
			"Если это не ты — выйди из аккаунта через меню 👇",
			html.EscapeString(student.Name))
		return h.editMessage(ctx, b, chatID, messageID, text, keyboard.MenuButton(), parseMode)

	case !errors.Is(err, domain.ErrNotFound):
		return fmt.Errorf("current student: %w", err)
	}

	text := "🔐 <b>Вход в аккаунт</b>\n\n" +
		"Введи свой <b>персональный код</b> в этот чат, чтобы войти.\n" +
		"Например: <code>A9b8C7d6</code>\n\n" +
		"Чтобы отменить, нажми «Назад»."

	markup := keyboard.AddBackButton(keyboard.Empty(), "auth_menu")

	// Режим включаем только после того, как экран показан: иначе при
	// ошибке редактирования пользователь застрянет в режиме без подсказки.
	if err := h.editMessage(ctx, b, chatID, messageID, text, markup, parseMode); err != nil {
		return err
	}

	h.states.WaitCode(userID, messageID)

	return nil
}

// cbUserRequestCode сохраняет запрос кода в support/login/. Показывать
// результат можно только неавторизованным: вошедшему код не нужен.
func (h *Handler) cbUserRequestCode(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	parseMode := models.ParseModeHTML

	// Если уже вошёл — код не нужен.
	if _, err := h.svc.CurrentStudent(ctx, userID); err == nil {
		text := "ℹ️ <b>Ты уже авторизован</b>\n\nАккаунт активен."
		return h.editMessage(ctx, b, chatID, messageID, text, keyboard.MenuButton(), parseMode)
	} else if !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("check student: %w", err)
	}

	// Тянем имя/username из Telegram.
	chat, err := b.GetChat(ctx, &bot.GetChatParams{ChatID: userID})
	if err != nil {
		return fmt.Errorf("get chat: %w", err)
	}
	tgName := strings.TrimSpace(chat.FirstName + " " + chat.LastName)
	username := chat.Username

	err = h.svc.RequestCode(ctx, userID, tgName, username)
	switch {
	case errors.Is(err, domain.ErrSupportRequestExists):
		text := "📩 <b>Заявка уже отправлена</b>\n\n" +
			"Все еще ждем код... терпение, друг.\n\n" +
			"Если код не пришел в течение года — попробуй еще раз."
		return h.editMessage(ctx, b, chatID, messageID, text,
			keyboard.AddBackButton(keyboard.Empty(), "auth_menu"), parseMode)

	case err != nil:
		return fmt.Errorf("request code: %w", err)
	}

	text := "✅ <b>Заявка отправлена</b>\n\n" +
		"Осталось дождаться кода... да... ждем..."
	return h.editMessage(ctx, b, chatID, messageID, text,
		keyboard.AddBackButton(keyboard.Empty(), "auth_menu"), parseMode)
}

// cbLogoutConfirm показывает экран подтверждения выхода.
// Аккаунт на этом шаге ещё не удаляется.
func (h *Handler) cbLogoutConfirm(
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

	text := fmt.Sprintf(
		"🚪 <b>Выход</b>\n\n"+
			"Ты точно хочешь выйти из аккаунта '<b>%s</b>'?\n\n"+
			"<i>Чтобы войти снова, понадобится персональный код.</i>",
		html.EscapeString(student.Name),
	)

	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "✅ Да, выйти", CallbackData: "logout_confirm"},
				{Text: "⬅️ Отмена", CallbackData: "main_menu"},
			},
		},
	}

	return h.editMessage(ctx, b, chatID, messageID, text, kb, parseMode)
}

// cbLogout выполняет выход: удаляет аккаунт и возвращает меню авторизации.
func (h *Handler) cbLogout(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	if err := h.svc.Logout(ctx, userID); err != nil {
		return fmt.Errorf("logout: %w", err)
	}

	text := "👋 <b>Ты успешно вышел из аккаунта</b>\n\nВойди снова или запроси код 👇"

	return h.editMessage(ctx, b, chatID, messageID, text, keyboard.AuthKeyboard(), models.ParseModeHTML)
}
