// homework.go — клавиатуры экранов сдачи ДЗ.
package keyboard

import (
	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/dmitriy-mvdness/hw-checker/internal/utils"
	"github.com/go-telegram/bot/models"
)

// Тексты кнопок домашнего задания.
const (
	BtnHomework = "📚 Домашнее задание"
)

// SubjectsKeyboard строит список предметов студента.
func SubjectsKeyboard(subjects []domain.Subject) *models.InlineKeyboardMarkup {
	kb := Empty()
	for _, s := range subjects {
		kb.InlineKeyboard = append(kb.InlineKeyboard, []models.InlineKeyboardButton{
			{Text: utils.CleanSubjectName(s.Name), CallbackData: CallbackSubject(s.Code)},
		})
	}
	return AddBackButton(kb, CbMainMenu)
}

// ThemesKeyboard строит список тем предмета. У каждой темы слева иконка:
// 🟢 если по теме уже есть сдача, 🔴 если ещё нет.
func ThemesKeyboard(themes []domain.Theme) *models.InlineKeyboardMarkup {
	kb := Empty()
	for _, t := range themes {
		icon := "🔴"
		if t.Submitted {
			icon = "🟢"
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, []models.InlineKeyboardButton{
			{Text: icon + " " + utils.CapitalizeAfterThemePrefix(t.Name), CallbackData: CallbackTheme(t.Code)},
		})
	}
	return AddBackButton(kb, CbHomework)
}
