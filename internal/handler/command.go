// command.go — обработка текстовых команд: /start, /menu, /help.
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
	"go.uber.org/zap"
)

// errorText — заглушка на случай внутренней ошибки.
const errorText = "😕 <b>Что-то пошло не так</b>\n\nПопробуй ещё раз чуть позже"

// commandName возвращает нормализованное имя команды из текста сообщения:
// без упоминания бота и в нижнем регистре. Если текста нет — пустую строку.
func commandName(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	cmd := fields[0]
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	return strings.ToLower(cmd)
}

// SetCommands регистрирует команды бота в Telegram. Они появляются в
// синей кнопке "Меню" рядом с полем ввода, поэтому отдельная кнопка
// для вызова команд не нужна.
func (h *Handler) SetCommands(ctx context.Context, b *bot.Bot) error {
	_, err := b.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{Command: "start", Description: "Начать"},
			{Command: "menu", Description: "Открыть меню"},
			{Command: "help", Description: "Справка по боту"},
		},
	})
	return err
}

// handleCommand принимает команды от пользователя и отвечает на них
// нужной реакцией. Здесь же решается, как бот реагирует на ввод команды
// и поддерживается порядок в чате.
func (h *Handler) handleCommand(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID

	// Команда — явное переключение. Юзер больше не «в контексте сдачи»,
	// забываем последнюю открытую тему: следующий orphan-апдейт не
	// должен выкидывать подсказку про сдачу.
	h.states.ClearLastTheme(userID)

	// Сообщение с командой убираем из чата, чтобы он оставался чистым.
	// Косметика — Debug.
	if err := h.deleteMessage(ctx, b, chatID, update.Message.ID); err != nil {
		h.log.Debug("не удалось удалить сообщение пользователя",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}

	var (
		text   string
		markup models.ReplyMarkup // nil: без клавиатуры
	)

	switch commandName(update.Message.Text) {
	case "/start":
		text, markup = h.startScreen(ctx, update)

	case "/menu":
		text, markup = h.menuScreen(ctx, update)

	case "/help":
		text = h.helpText()
		markup = nil

	default:
		text = "❓ <b>Неизвестная команда</b>\n\n" +
			"Попробуй /start - там все самое нужное"
	}

	if err := h.sendMessage(ctx, b, chatID, text, markup, models.ParseModeHTML); err != nil {
		h.log.Error("не удалось отправить ответ на команду",
			zap.Int64("user_id", userID),
			zap.String("command", commandName(update.Message.Text)),
			zap.Error(err),
		)
	}
}

// startScreen формирует ответ на /start. Вошедшему пользователю
// предлагается меню, остальным вход в аккаунт.
func (h *Handler) startScreen(ctx context.Context, update *models.Update) (string, models.ReplyMarkup) {
	userID := update.Message.From.ID
	userName := html.EscapeString(GetUserDisplayName(update))

	_, err := h.svc.CurrentStudent(ctx, userID)
	switch {
	case err == nil:
		text := fmt.Sprintf(
			"🤗 <b>С возвращением, %s!</b>\n\n"+
				"Здесь твой учебный помощник: сдачи ДЗ, поддержка и все почти по делу.\n\n"+
				"<b>Чтобы продолжить, воспользуйся кнопкой ниже</b> 👇",
			userName,
		)
		return text, keyboard.MenuButton()

	case errors.Is(err, domain.ErrNotFound):
		text := fmt.Sprintf(
			"🤗 <b>Добро пожаловать, %s!</b>\n\n"+
				"Здесь ты сможешь легко сдавать домашние задания.\n\n"+
				"Чтобы начать, <b>войди в аккаунт</b> 👇",
			userName,
		)
		return text, keyboard.AuthKeyboard()

	default:
		h.log.Error("не удалось проверить авторизацию в /start",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
		return errorText, nil
	}
}

// menuScreen формирует ответ на /menu.
func (h *Handler) menuScreen(ctx context.Context, update *models.Update) (string, models.ReplyMarkup) {
	userID := update.Message.From.ID

	_, err := h.svc.CurrentStudent(ctx, userID)
	switch {
	case err == nil:
		return menuText, keyboard.MainMenuKeyboard()

	case errors.Is(err, domain.ErrNotFound):
		return notLoggedText, keyboard.AuthKeyboard()

	default:
		h.log.Error("не удалось проверить авторизацию в /menu",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
		return errorText, nil
	}
}
