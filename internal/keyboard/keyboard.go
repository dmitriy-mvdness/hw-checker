// Package keyboard собирает inline-клавиатуры для экранов бота:
// главное меню, авторизация, предметы и темы, поддержка. Кнопки
// возвращают callback-данные, которые разбирает handler.
package keyboard

import "github.com/go-telegram/bot/models"

// BackButton возвращает кнопку "Назад" с callback-данными
// "back:<callback>". Префикс объявлен в callbacks.go.
func BackButton(callback string) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{
		Text:         "⬅️ Назад",
		CallbackData: BackCallbackPrefix + callback,
	}
}

// AddBackButton добавляет к клавиатуре ряд с кнопкой "Назад".
func AddBackButton(
	kb *models.InlineKeyboardMarkup,
	callback string,
) *models.InlineKeyboardMarkup {
	kb.InlineKeyboard = append(
		kb.InlineKeyboard,
		[]models.InlineKeyboardButton{
			BackButton(callback),
		},
	)
	return kb
}

// Empty возвращает пустую inline-клавиатуру.
func Empty() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{},
	}
}
