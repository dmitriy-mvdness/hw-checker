// homework.go — клавиатуры экранов сдачи ДЗ.
package keyboard

import (
	"fmt"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/dmitriy-mvdness/hw-checker/internal/utils"
	"github.com/go-telegram/bot/models"
)

// Тексты кнопок домашнего задания.
const (
	BtnHomework = "📚 Домашнее задание"
)

// themesPerPage — сколько тем показываем на одной странице.
const themesPerPage = 5

// SubjectsKeyboard строит список предметов студента.
func SubjectsKeyboard(subjects []domain.Subject) *models.InlineKeyboardMarkup {
	kb := Empty()
	for _, s := range subjects {
		kb.InlineKeyboard = append(kb.InlineKeyboard, []models.InlineKeyboardButton{
			// При клике на предмет всегда открываем 0-ю страницу
			{Text: utils.CleanSubjectName(s.Name), CallbackData: CallbackSubject(s.Code, 0)},
		})
	}
	return AddBackButton(kb, CbMainMenu)
}

// ThemesKeyboard строит список тем предмета. У каждой темы слева иконка:
// 🟢 если по теме уже есть сдача, 🔴 если ещё нет.
// Добавляет кнопки пагинации, если тем больше, чем themesPerPage.
func ThemesKeyboard(themes []domain.Theme, subjectCode int64, page int) *models.InlineKeyboardMarkup {
	kb := Empty()

	// Считаем общее количество страниц
	totalPages := (len(themes) + themesPerPage - 1) / themesPerPage
	if totalPages == 0 {
		totalPages = 1
	}

	// Защита от выхода за границы
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	// Вырезаем кусок тем для текущей страницы
	start := page * themesPerPage
	end := start + themesPerPage
	if end > len(themes) {
		end = len(themes)
	}
	pageThemes := themes[start:end]

	// Рисуем кнопки тем
	for _, t := range pageThemes {
		icon := "🔴"
		if t.Submitted {
			icon = "🟢"
		}
		kb.InlineKeyboard = append(kb.InlineKeyboard, []models.InlineKeyboardButton{
			{Text: icon + " " + utils.CapitalizeAfterThemePrefix(t.Name), CallbackData: CallbackTheme(t.Code)},
		})
	}

	// Если страниц больше одной — добавляем навигацию
	if totalPages > 1 {
		var navRow []models.InlineKeyboardButton

		// Кнопка "Назад" (влево)
		if page > 0 {
			navRow = append(navRow, models.InlineKeyboardButton{
				Text: "⬅️", CallbackData: CallbackSubject(subjectCode, page-1),
			})
		} else {
			// Пустая заглушка, чтобы кнопки не ездили по ширине
			navRow = append(navRow, models.InlineKeyboardButton{Text: " ", CallbackData: CbIgnore})
		}

		// Индикатор текущей страницы
		navRow = append(navRow, models.InlineKeyboardButton{
			Text: fmt.Sprintf("%d/%d", page+1, totalPages), CallbackData: CbIgnore,
		})

		// Кнопка "Вперед" (вправо)
		if page < totalPages-1 {
			navRow = append(navRow, models.InlineKeyboardButton{
				Text: "➡️", CallbackData: CallbackSubject(subjectCode, page+1),
			})
		} else {
			// Пустая заглушка для симметрии
			navRow = append(navRow, models.InlineKeyboardButton{Text: " ", CallbackData: CbIgnore})
		}

		kb.InlineKeyboard = append(kb.InlineKeyboard, navRow)
	}

	return AddBackButton(kb, CbHomework)
}
