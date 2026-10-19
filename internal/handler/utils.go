// utils.go — мелкие хелперы handler'а.
package handler

import (
	"strings"

	"github.com/go-telegram/bot/models"
)

// isCommand проверяет, начинается ли текст с символа "/".
func (h *Handler) isCommand(text string) bool {
	return len(text) > 0 && text[0] == '/'
}

// isAlbum сообщает, пришло ли сообщение частью альбома (media group).
// Telegram шлёт каждый файл альбома отдельным апдейтом с общим
// media_group_id; для бота это выглядит как N независимых сообщений.
func isAlbum(msg *models.Message) bool {
	return msg != nil && msg.MediaGroupID != ""
}

// GetUserDisplayName возвращает отображаемое имя пользователя:
// "Имя Фамилия", либо "@username", либо запасное "Пользователь".
func GetUserDisplayName(update *models.Update) string {
	user := update.Message.From

	if name := strings.TrimSpace(user.FirstName + " " + user.LastName); name != "" {
		return name
	}

	if user.Username != "" {
		return "@" + user.Username
	}

	return "Пользователь"
}
