// callback.go — точка входа и маршрутизация callback-нажатий.
package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/dmitriy-mvdness/hw-checker/internal/keyboard"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

// callbackHandlerFunc — обработчик точного маршрута (например, "main_menu").
type callbackHandlerFunc func(ctx context.Context, b *bot.Bot, userID, chatID int64, messageID int) error

// callbackPrefixHandlerFunc — обработчик префиксного маршрута
// (например, "subject:5": префикс "subject:", аргумент "5").
type callbackPrefixHandlerFunc func(ctx context.Context, b *bot.Bot, userID, chatID int64, messageID int, arg string) error

// protectedRoutes — экраны, доступные только после входа. Ключи — точные.
// Префиксные маршруты всегда считаются защищёнными (см. isProtected).
var protectedRoutes = map[string]bool{
	keyboard.CbMainMenu:      true,
	keyboard.CbProfile:       true,
	keyboard.CbLogout:        true,
	keyboard.CbLogoutConfirm: true,
	keyboard.CbHomework:      true,
	keyboard.CbSupport:       true,
	keyboard.CbSupportTech:   true,
	keyboard.CbSupportHelp:   true,
	keyboard.CbMySubmissions: true,
	keyboard.CbNews:          true,
	keyboard.CbCancelResume:  true,
}

// handleCallback — точка входа для всех callback-нажатий.
// Поддерживает переходы "назад" через префикс "back:".
func (h *Handler) handleCallback(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	if update.CallbackQuery == nil {
		return
	}

	// 1. СРАЗУ отвечаем на callback, чтобы уложиться в таймаут Telegram.
	//    Делаем это асинхронно, чтобы не блокировать основной поток.
	//    safeGo ловит панику: recoverMiddleware до этой горутины не добирается.
	safeGo(h.log, func() {
		params := &bot.AnswerCallbackQueryParams{CallbackQueryID: update.CallbackQuery.ID}
		// Если пользователь в режиме ввода кода, показываем алерт.
		// Но проверять это состояние нужно здесь, до отправки.
		if h.states.IsWaitingCode(update.CallbackQuery.From.ID) &&
			!strings.HasPrefix(update.CallbackQuery.Data, keyboard.BackCallbackPrefix) &&
			update.CallbackQuery.Data != keyboard.CbRequestCode {
			params.Text = "Сначала введи код или нажми «Назад»"
			params.ShowAlert = true
		}
		if _, err := b.AnswerCallbackQuery(ctx, params); err != nil {
			h.log.Warn("не удалось ответить на callback",
				zap.Int64("user_id", update.CallbackQuery.From.ID),
				zap.Error(err),
			)
		}
	})

	// 2. Сообщение могло стать недоступным (старое, удалённое, из чужого
	//    контекста). Telegram присылает CallbackQuery.Message как
	//    MaybeInaccessibleMessage — структуру с двумя указателями.
	//    Если вложенный Message == nil, значит сообщение недоступно
	//    (это InaccessibleMessage), редактировать нечего — выходим тихо.
	if update.CallbackQuery.Message.Message == nil {
		return
	}

	// 3. Теперь выполняем остальную логику (проверки, dispatch).
	data := update.CallbackQuery.Data
	userID := update.CallbackQuery.From.ID
	chatID := update.CallbackQuery.Message.Message.Chat.ID
	messageID := update.CallbackQuery.Message.Message.ID

	isBack := strings.HasPrefix(data, keyboard.BackCallbackPrefix)
	isRequestCode := data == keyboard.CbRequestCode

	// Блокировка, если пользователь вводит код (для основной логики).
	blocked := h.states.IsWaitingCode(userID) && !isBack && !isRequestCode
	if blocked {
		return
	}

	if isBack || isRequestCode {
		h.states.Clear(userID)
	}

	route := strings.TrimPrefix(data, keyboard.BackCallbackPrefix)

	if h.isProtected(route) {
		_, err := h.svc.CurrentStudent(ctx, userID)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			if err := h.editMessage(ctx, b, chatID, messageID, notLoggedText, keyboard.AuthKeyboard(), models.ParseModeHTML); err != nil {
				h.log.Warn("не удалось показать экран входа",
					zap.Int64("user_id", userID),
					zap.Error(err),
				)
			}
			return
		case err != nil:
			h.log.Error("не удалось проверить авторизацию",
				zap.Int64("user_id", userID),
				zap.Error(err),
			)
			return
		}
	}

	if err := h.dispatch(ctx, b, userID, chatID, messageID, route); err != nil {
		h.log.Error("ошибка обработки callback",
			zap.Int64("user_id", userID),
			zap.String("data", data),
			zap.String("route", route),
			zap.Error(err),
		)
	}
}

// dispatch выбирает обработчик: сначала точное совпадение,
// затем — первое подходящее префиксное.
func (h *Handler) dispatch(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
	route string,
) error {
	if fn, ok := h.callbackRoutes[route]; ok {
		return fn(ctx, b, userID, chatID, messageID)
	}
	for _, pr := range h.callbackPrefixRoutes {
		if strings.HasPrefix(route, pr.prefix) {
			return pr.fn(ctx, b, userID, chatID, messageID, strings.TrimPrefix(route, pr.prefix))
		}
	}
	return fmt.Errorf("unknown callback: %s", route)
}

// isProtected проверяет, требует ли маршрут входа в аккаунт.
// Любой префиксный маршрут защищён.
func (h *Handler) isProtected(route string) bool {
	if protectedRoutes[route] {
		return true
	}
	for _, pr := range h.callbackPrefixRoutes {
		if strings.HasPrefix(route, pr.prefix) {
			return true
		}
	}
	return false
}
