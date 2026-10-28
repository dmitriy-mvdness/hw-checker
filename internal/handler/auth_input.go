// auth_input.go — обработка сообщения с персональным кодом.
package handler

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"strings"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/dmitriy-mvdness/hw-checker/internal/keyboard"
	"github.com/dmitriy-mvdness/hw-checker/internal/session"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

// handleCodeInput обрабатывает сообщение пользователя, который находится
// в режиме ввода кода. Любая команда в этом режиме запрещена: сообщение
// просто удаляется, а выйти из режима можно только кнопкой "Назад".
// Всё остальное считается попыткой ввести код.
func (h *Handler) handleCodeInput(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	msg := update.Message
	userID := msg.From.ID
	chatID := msg.Chat.ID

	// Сообщение пользователя убираем из чата в любом случае: так в
	// переписке не остаётся ни кода, ни лишних команд. При сбое —
	// Debug: удаление косметическое, шуметь Warn'ом не за что.
	if err := h.deleteMessage(ctx, b, chatID, msg.ID); err != nil {
		h.log.Debug("не удалось удалить сообщение пользователя",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}

	if h.isCommand(msg.Text) {
		return
	}

	// Пока шли удаление и проверки, пользователь мог нажать "Назад".
	sess := h.states.Get(userID)
	if sess.State != session.StateWaitingCode {
		return
	}

	student, err := h.svc.Login(ctx, userID, msg.Text)

	var (
		text   string
		markup models.ReplyMarkup // nil: клавиатура убирается
	)

	switch {
	case err == nil:
		h.states.Clear(userID)
		h.log.Info("пользователь вошёл",
			zap.Int64("user_id", userID),
			zap.Int64("student_code", student.Code),
		)
		text = successText(student)
		markup = keyboard.MenuButton()

	case errors.Is(err, domain.ErrTooManyAttempts):
		// Лимитер сработал: попытки в текущем окне исчерпаны.
		// Показываем реальное время до сброса окна — оно уменьшается
		// с каждой новой попыткой, потому что окно fixed.
		// Состояние НЕ очищаем: пользователь остаётся в режиме ввода
		// и сможет ввести код, как только окно сбросится.
		secs := int(math.Ceil(h.svc.LoginRetryAfter(userID).Seconds()))
		if secs < 1 {
			secs = 1
		}
		text = fmt.Sprintf("⏳ <b>Слишком много попыток</b>\n\n"+
			"Подожди %d сек. и попробуй снова.", secs)
		markup = keyboard.CodeErrorKeyboard()

	case errors.Is(err, domain.ErrNotFound):
		text = "❌ <b>Неверный код</b>\n\n" +
			"Проверь код и введи его ещё раз.\n" +
			"Если забыл или еще не получил код, напиши в поддержку 👇"
		markup = keyboard.CodeErrorKeyboard()

	case errors.Is(err, domain.ErrCodeTaken):
		text = "⚠️ <b>Этот код уже занят</b>\n\n" +
			"Он привязан к другому аккаунту.\n" +
			"Если это ошибка, запроси другой код 👇"
		markup = keyboard.CodeErrorKeyboard()

	case errors.Is(err, domain.ErrAccountExists):
		h.states.Clear(userID)
		text = "✅ <b>Ты уже вошёл</b>"
		markup = keyboard.MenuButton()

	default:
		h.log.Error("ошибка входа",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
		text = "😕 <b>Что-то пошло не так</b>\n\n" +
			"Попробуй ещё раз чуть позже или напиши в поддержку 👇"
		markup = keyboard.CodeErrorKeyboard()
	}

	if err := h.editMessage(ctx, b, chatID, sess.MessageID, text, markup, models.ParseModeHTML); err != nil {
		h.log.Warn("не удалось показать результат входа",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}
}

// successText собирает сообщение об успешном входе. Обращение — по имени,
// если оно есть в ФИО из 1С, иначе по полному имени. Комплимент — случайный
// из loginKindWords, чтобы один и тот же текст не приедался.
func successText(s domain.Student) string {
	parts := strings.Fields(s.Name)
	greeting := s.Name
	if len(parts) >= 2 {
		greeting = parts[1] // берём имя
	} else if len(parts) == 1 {
		greeting = parts[0]
	}

	text := fmt.Sprintf("✅ <b>Вход выполнен</b>\n\n"+
		"Привет, <b>%s</b>! 👋\n\n"+
		"🙌 %s\n\n"+
		"<b>Выбери нужный раздел 👇</b>",
		html.EscapeString(greeting),
		pickKindWord(loginKindWords))
	return text
}
