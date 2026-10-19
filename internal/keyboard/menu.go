// menu.go — клавиатуры главного меню и кнопки «Меню».
package keyboard

import "github.com/go-telegram/bot/models"

// Тексты кнопок меню.
const (
	BtnMenu          = "📋 Меню"
	BtnProfile       = "👤 Профиль"
	BtnLogout        = "🚪 Выйти из аккаунта"
	BtnMySubmissions = "📊 Мои сдачи"
	BtnNews          = "✨ Что нового"
)

// MenuButton возвращает клавиатуру с единственной кнопкой "Меню".
// Показывается после /start, если пользователь уже вошёл.
func MenuButton() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: BtnMenu, CallbackData: CbMainMenu},
			},
		},
	}
}

// MainMenuKeyboard возвращает клавиатуру главного меню.
func MainMenuKeyboard() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: BtnHomework, CallbackData: CbHomework}},
			{{Text: BtnMySubmissions, CallbackData: CbMySubmissions}},
			{{Text: BtnProfile, CallbackData: CbProfile}},
			{{Text: BtnNews, CallbackData: CbNews}},
			{{Text: BtnSupportMenu, CallbackData: CbSupport}},
		},
	}
}
