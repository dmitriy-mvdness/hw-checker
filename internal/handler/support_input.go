// support_input.go — приём обращений в поддержку: текст, файл, фото.
package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/dmitriy-mvdness/hw-checker/internal/keyboard"
	"github.com/dmitriy-mvdness/hw-checker/internal/session"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

// handleSupportInput обрабатывает текстовое обращение.
func (h *Handler) handleSupportInput(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	msg := update.Message
	userID := msg.From.ID
	chatID := msg.Chat.ID

	sess, ok := h.states.TakeSupport(userID)
	if !ok {
		return
	}

	if err := h.deleteMessage(ctx, b, chatID, msg.ID); err != nil {
		h.log.Debug("не удалось удалить сообщение пользователя",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}

	tgName := strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)
	username := msg.From.Username

	err := h.svc.SubmitSupportText(ctx, userID, sess.SupportReason, msg.Text, tgName, username)
	if err != nil {
		h.log.Error("не удалось сохранить текстовое обращение",
			zap.Int64("user_id", userID),
			zap.String("reason", sess.SupportReason),
			zap.Error(err),
		)
		h.askSupportAgain(ctx, b, userID, chatID, sess,
			"😕 <b>Что-то пошло не так</b>\n\nПопробуй ещё раз чуть позже.")
		return
	}

	h.showSupportSuccess(ctx, b, chatID, sess.MessageID, sess.SupportReason)
}

// handleSupportFile обрабатывает файл-обращение.
func (h *Handler) handleSupportFile(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	msg := update.Message
	if msg == nil || msg.Document == nil {
		return
	}

	userID := msg.From.ID
	chatID := msg.Chat.ID

	sess, ok := h.states.TakeSupport(userID)
	if !ok {
		return
	}

	if err := h.deleteMessage(ctx, b, chatID, msg.ID); err != nil {
		h.log.Debug("не удалось удалить сообщение пользователя",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}

	doc := msg.Document

	if !h.svc.ValidateExt(doc.FileName) {
		h.askSupportAgain(ctx, b, userID, chatID, sess,
			fmt.Sprintf("❌ <b>Неверный формат файла</b>\n\n"+
				"Разрешены: %s.\n"+
				"<b>Пришли файл ещё раз или напиши текстом.</b>", h.svc.AllowedExtsDisplay()))
		return
	}

	if int64(doc.FileSize) > h.svc.MaxFileSize() {
		h.askSupportAgain(ctx, b, userID, chatID, sess,
			fmt.Sprintf("❌ <b>Файл слишком большой</b>\n\nМаксимум — %d МБ.", h.svc.MaxFileSizeMB()))
		return
	}

	data, err := h.downloadFile(ctx, b, doc.FileID)
	if err != nil {
		h.log.Error("не удалось скачать файл обращения",
			zap.Int64("user_id", userID),
			zap.String("file_name", doc.FileName),
			zap.Error(err),
		)
		h.askSupportAgain(ctx, b, userID, chatID, sess,
			"😕 <b>Не удалось скачать файл</b>\n\nПопробуй ещё раз чуть позже.")
		return
	}

	// Telegram иногда занижает FileSize в метаданных (или отдаёт 0).
	// Проверяем фактический размер скачанных данных: иначе в поддержку
	// уйдёт файл больше лимита.
	if int64(len(data)) > h.svc.MaxFileSize() {
		h.askSupportAgain(ctx, b, userID, chatID, sess,
			fmt.Sprintf("❌ <b>Файл слишком большой</b>\n\nМаксимум — %d МБ.", h.svc.MaxFileSizeMB()))
		return
	}

	tgName := strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)
	username := msg.From.Username
	caption := strings.TrimSpace(msg.Caption)

	err = h.svc.SubmitSupportFile(ctx, userID, sess.SupportReason, doc.FileName, caption, data, tgName, username)
	if err != nil {
		h.log.Error("не удалось сохранить файл обращения",
			zap.Int64("user_id", userID),
			zap.String("reason", sess.SupportReason),
			zap.Error(err),
		)
		h.askSupportAgain(ctx, b, userID, chatID, sess,
			"😕 <b>Что-то пошло не так</b>\n\nПопробуй ещё раз чуть позже.")
		return
	}

	h.showSupportSuccess(ctx, b, chatID, sess.MessageID, sess.SupportReason)
}

// askSupportAgain возвращает в состояние ожидания обращения.
func (h *Handler) askSupportAgain(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	sess session.Session,
	text string,
) {
	h.states.WaitSupport(userID, sess.SupportReason, sess.MessageID)
	markup := keyboard.AddBackButton(keyboard.Empty(), "support")
	if err := h.editMessage(ctx, b, chatID, sess.MessageID, text, markup, models.ParseModeHTML); err != nil {
		h.log.Warn("не удалось обновить экран поддержки",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}
}

// showSupportSuccess редактирует экран успешного сохранения.
func (h *Handler) showSupportSuccess(
	ctx context.Context,
	b *bot.Bot,
	chatID int64,
	messageID int,
	arg string,
) {
	reasonTitle := domain.SupportReasonTitle(arg)

	text := fmt.Sprintf("✅ <b> Заявка принята </b>\n\n"+
		"Спасибо! Обращение по теме <b>«%s»</b> было получено.\n\n"+
		"Если понадобится — с тобой свяжутся.", reasonTitle)
	markup := keyboard.AddBackButton(keyboard.Empty(), "main_menu")
	if err := h.editMessage(ctx, b, chatID, messageID, text, markup, models.ParseModeHTML); err != nil {
		h.log.Warn("не удалось показать экран успешной заявки",
			zap.String("reason", arg),
			zap.Error(err),
		)
	}
}

// handleSupportPhoto обрабатывает фото-обращение. Telegram отдаёт фото
// массивом PhotoSize от меньшего к большему — берём последний, он самый
// крупный. Имени файла у фото нет, поэтому генерим своё.
func (h *Handler) handleSupportPhoto(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	msg := update.Message
	if msg == nil || len(msg.Photo) == 0 {
		return
	}

	userID := msg.From.ID
	chatID := msg.Chat.ID

	sess, ok := h.states.TakeSupport(userID)
	if !ok {
		return
	}

	if err := h.deleteMessage(ctx, b, chatID, msg.ID); err != nil {
		h.log.Debug("не удалось удалить сообщение пользователя",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}

	photo := msg.Photo[len(msg.Photo)-1]

	if int64(photo.FileSize) > h.svc.MaxFileSize() {
		h.askSupportAgain(ctx, b, userID, chatID, sess,
			fmt.Sprintf("❌ <b>Фото слишком большое</b>\n\nМаксимум — %d МБ.", h.svc.MaxFileSizeMB()))
		return
	}

	data, err := h.downloadFile(ctx, b, photo.FileID)
	if err != nil {
		h.log.Error("не удалось скачать фото обращения",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
		h.askSupportAgain(ctx, b, userID, chatID, sess,
			"😕 <b>Не удалось скачать фото</b>\n\nПопробуй ещё раз чуть позже.")
		return
	}

	// Telegram иногда занижает FileSize в метаданных. Проверяем
	// фактический размер скачанных данных.
	if int64(len(data)) > h.svc.MaxFileSize() {
		h.askSupportAgain(ctx, b, userID, chatID, sess,
			fmt.Sprintf("❌ <b>Фото слишком большое</b>\n\nМаксимум — %d МБ.", h.svc.MaxFileSizeMB()))
		return
	}

	// Telegram хранит фото в JPEG — даём осмысленное имя с датой.
	originalName := fmt.Sprintf("photo_%s.jpg", time.Now().Format("20060102_150405"))
	caption := strings.TrimSpace(msg.Caption)
	tgName := strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)
	username := msg.From.Username

	err = h.svc.SubmitSupportFile(ctx, userID, sess.SupportReason, originalName, caption, data, tgName, username)
	if err != nil {
		h.log.Error("не удалось сохранить фото обращения",
			zap.Int64("user_id", userID),
			zap.String("reason", sess.SupportReason),
			zap.Error(err),
		)
		h.askSupportAgain(ctx, b, userID, chatID, sess,
			"😕 <b>Что-то пошло не так</b>\n\nПопробуй ещё раз чуть позже.")
		return
	}

	h.showSupportSuccess(ctx, b, chatID, sess.MessageID, sess.SupportReason)
}
