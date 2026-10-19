// register.go — подписка обработчиков Handler на события бота.
package handler

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Register подписывает обработчики на текстовые сообщения и callback-кнопки.
//
// Порядок обёрток важен: recoverMiddleware снаружи, drainMiddleware внутри.
// Если поменять местами — паника внутри обработчика не даст drainMiddleware
// снять счётчик (defer там, но recover уже проглотил панику, до внешнего
// defer с wg.Done дело дойдёт только при разворачивании стека). Снаружи
// recover видит панику уже после всех defer'ов, включая wg.Done.
func (h *Handler) Register(b *bot.Bot) {
	b.RegisterHandler(
		bot.HandlerTypeMessageText,
		"",
		bot.MatchTypeContains,
		recoverMiddleware(h.log, h.drainMiddleware(h.handleUpdate)),
	)

	b.RegisterHandler(
		bot.HandlerTypeCallbackQueryData,
		"",
		bot.MatchTypePrefix,
		recoverMiddleware(h.log, h.drainMiddleware(h.handleCallback)),
	)
}

// handleUpdate — точка входа для сообщений. Порядок веток важен:
//  1. альбом — отдельная реакция, до всех остальных веток;
//  2. фото — в поддержке принимаем, в ДЗ отклоняем;
//  3. документ — если пользователь ждёт файл, принимаем его;
//  4. режим ввода кода;
//  5. режим ожидания файла (текст мимо — подсказка);
//  6. режим ожидания обращения в поддержку;
//  7. команды;
//  8. всё остальное, если есть свежая открытая тема.
func (h *Handler) handleUpdate(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	if update.Message == nil || update.Message.From == nil {
		return
	}

	// Альбомы (media group) не поддерживаем: Telegram шлёт каждый файл
	// отдельным апдейтом, и пользователь может думать, что все файлы
	// сохранятся. Обрабатываем по текущему режиму.
	if isAlbum(update.Message) {
		h.handleAlbum(ctx, b, update)
		return
	}

	userID := update.Message.From.ID

	// Фото: в поддержке принимаем как вложение, в ДЗ — отклоняем
	// с подсказкой. Вне контекста — orphan.
	if update.Message.Photo != nil {
		if h.states.IsWaitingSupport(userID) {
			h.handleSupportPhoto(ctx, b, update)
			return
		}
		if h.states.IsWaitingFile(userID) {
			h.handlePhotoWhileWaitingFile(ctx, b, update)
			return
		}
		h.handleOrphanUpdate(ctx, b, update)
		return
	}

	// Документы: либо ждём файл ДЗ, либо ждём вложение для поддержки.
	// Вне контекста — orphan.
	if update.Message.Document != nil {
		if h.states.IsWaitingFile(userID) {
			h.handleDocument(ctx, b, update)
			return
		}
		if h.states.IsWaitingSupport(userID) {
			h.handleSupportFile(ctx, b, update)
			return
		}
		h.handleOrphanUpdate(ctx, b, update)
		return
	}

	text := update.Message.Text
	if text != "" {
		if h.states.IsWaitingCode(userID) {
			h.handleCodeInput(ctx, b, update)
			return
		}
		if h.states.IsWaitingFile(userID) {
			h.handleTextWhileWaitingFile(ctx, b, update)
			return
		}
		if h.states.IsWaitingSupport(userID) {
			h.handleSupportInput(ctx, b, update)
			return
		}
		// Команды живут вне режимов: работают всегда.
		if h.isCommand(text) {
			h.handleCommand(ctx, b, update)
			return
		}
		// Свободный текст вне контекста — orphan.
		h.handleOrphanUpdate(ctx, b, update)
		return
	}

	// Всё остальное: стикеры, голосовые, видео, кружки, гифки и т.п.
	// Если есть свежая открытая тема — подскажем продолжить; иначе тихо.
	h.handleOrphanUpdate(ctx, b, update)
}
