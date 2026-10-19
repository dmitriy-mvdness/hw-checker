// support.go — клавиатуры экранов поддержки.
package keyboard

import (
	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/go-telegram/bot/models"
)

// Тексты кнопок поддержки.
const (
	BtnSupportMenu     = "🤝 Помощь"
	BtnSupportTech     = "🛠️ Техподдержка"
	BtnSupportHelp     = "📖 Справка"
	BtnSupportBug      = "🐞 Баг в боте"
	BtnSupportIdeas    = "💡 Идея для бота"
	BtnSupportHomework = "📚 Домашнее задание"
	BtnSupportOther    = "❓ Другое"
)

// HelpMenuKeyboard — промежуточный экран «Помощь» с двумя действиями:
// техподдержка и справка.
func HelpMenuKeyboard() *models.InlineKeyboardMarkup {
	kb := Empty()
	kb.InlineKeyboard = append(kb.InlineKeyboard,
		[]models.InlineKeyboardButton{
			{Text: BtnSupportTech, CallbackData: CbSupportTech},
		},
		[]models.InlineKeyboardButton{
			{Text: BtnSupportHelp, CallbackData: CbSupportHelp},
		},
	)
	return AddBackButton(kb, CbMainMenu)
}

// SupportKeyboard — список причин обращения в техподдержку.
func SupportKeyboard() *models.InlineKeyboardMarkup {
	kb := Empty()
	kb.InlineKeyboard = append(kb.InlineKeyboard,
		[]models.InlineKeyboardButton{
			{Text: BtnSupportHomework, CallbackData: CallbackSupportReason(domain.SupportReasonHomework)},
		},
		[]models.InlineKeyboardButton{
			{Text: BtnSupportBug, CallbackData: CallbackSupportReason(domain.SupportReasonBug)},
		},
		[]models.InlineKeyboardButton{
			{Text: BtnSupportIdeas, CallbackData: CallbackSupportReason(domain.SupportReasonIdeas)},
		},
		[]models.InlineKeyboardButton{
			{Text: BtnSupportOther, CallbackData: CallbackSupportReason(domain.SupportReasonOther)},
		},
	)
	return AddBackButton(kb, CbSupport)
}
