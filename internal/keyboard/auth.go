// auth.go — клавиатуры экранов авторизации.
package keyboard

import "github.com/go-telegram/bot/models"

// Тексты кнопок авторизации.
const (
	BtnAuth        = "🔐 Войти"
	BtnRequestCode = "📩 Запросить код"
)

// AuthKeyboard возвращает клавиатуру меню авторизации: войти и запросить код.
func AuthKeyboard() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: BtnAuth, CallbackData: CbUserAuth}},
			{{Text: BtnRequestCode, CallbackData: CbRequestCode}},
		},
	}
}

// CodeErrorKeyboard — клавиатура после неудачной попытки ввода кода:
// просим код и даём вернуться в меню.
func CodeErrorKeyboard() *models.InlineKeyboardMarkup {
	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: BtnRequestCode, CallbackData: CbRequestCode}},
		},
	}
	return AddBackButton(kb, CbAuthMenu)
}
