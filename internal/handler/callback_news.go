// callback_news.go — экран «Что нового».
package handler

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/dmitriy-mvdness/hw-checker/internal/keyboard"
	"github.com/dmitriy-mvdness/hw-checker/internal/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// cbNews показывает последние 3 новые темы, доступные студенту.
func (h *Handler) cbNews(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	entries, err := h.svc.News(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return h.editMessage(ctx, b, chatID, messageID, notLoggedText, keyboard.AuthKeyboard(), models.ParseModeHTML)
	}
	if err != nil {
		return fmt.Errorf("news: %w", err)
	}

	markup := keyboard.AddBackButton(keyboard.Empty(), "main_menu")

	if len(entries) == 0 {
		text := "✨ <b>Что нового</b>\n\n" +
			"Пока ничего — как только появится новое ДЗ, оно будет здесь."
		return h.editMessage(ctx, b, chatID, messageID, text, markup, models.ParseModeHTML)
	}

	const divider = "━━━━━━━━━━━"

	const indent = "\u00A0\u00A0\u00A0"

	var sb strings.Builder
	sb.WriteString("✨ <b>Что нового</b>\n\n")

	var (
		currentDate    string
		currentSubject string
	)

	for _, e := range entries {
		date := e.CreatedAt.Format("02.01.2006")
		subject := utils.CleanSubjectName(e.SubjectName)

		// Новая дата — разделитель, заголовок дня и сброс предмета.
		if date != currentDate {
			if currentDate != "" {
				sb.WriteString("\n" + divider + "\n\n")
			}
			sb.WriteString(fmt.Sprintf("📅 <b>%s</b>\n\n", date))
			currentDate = date
			currentSubject = ""
		}

		// Новый предмет внутри дня — заголовок предмета.
		if subject != currentSubject {
			if currentSubject != "" {
				sb.WriteString("\n")
			}
			sb.WriteString(fmt.Sprintf("📘 <b>%s</b>\n", html.EscapeString(subject)))
			currentSubject = subject
		}

		sb.WriteString(fmt.Sprintf("%s• %s\n",
			indent,
			html.EscapeString(utils.CapitalizeAfterThemePrefix(e.ThemeName))))
	}

	return h.editMessage(ctx, b, chatID, messageID, sb.String(), markup, models.ParseModeHTML)
}
