// callback_homework.go — экраны сдачи ДЗ: предметы, темы, тема, «Мои сдачи».
package handler

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/dmitriy-mvdness/hw-checker/internal/keyboard"
	"github.com/dmitriy-mvdness/hw-checker/internal/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// cbHomework показывает список предметов студента.
func (h *Handler) cbHomework(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	subjects, err := h.svc.Subjects(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return h.editMessage(ctx, b, chatID, messageID, notLoggedText, keyboard.AuthKeyboard(), models.ParseModeHTML)
	}
	if err != nil {
		return fmt.Errorf("list subjects: %w", err)
	}

	if len(subjects) == 0 {
		text := "📚 <b>Домашнее задание</b>\n\nПока нет предметов с домашними заданиями."
		markup := keyboard.AddBackButton(keyboard.Empty(), "main_menu")
		return h.editMessage(ctx, b, chatID, messageID, text, markup, models.ParseModeHTML)
	}

	text := "📚 <b>Домашнее задание</b>\n\n" +
		"Здесь ты можешь сдать домашнее задание по своим предметам.\n\n" +
		"<b>Выбери нужный предмет:</b>"
	return h.editMessage(ctx, b, chatID, messageID, text, keyboard.SubjectsKeyboard(subjects), models.ParseModeHTML)
}

// cbSubject показывает темы предмета. arg — код предмета и страница ("5:0").
func (h *Handler) cbSubject(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
	arg string,
) error {
	parts := strings.Split(arg, ":")

	subjectCode, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return fmt.Errorf("parse subject code %q: %w", parts[0], err)
	}

	page := 0
	if len(parts) > 1 {
		if p, err := strconv.Atoi(parts[1]); err == nil {
			page = p
		}
	}

	themes, err := h.svc.Themes(ctx, userID, subjectCode)
	if errors.Is(err, domain.ErrNotFound) {
		return h.editMessage(ctx, b, chatID, messageID, notLoggedText, keyboard.AuthKeyboard(), models.ParseModeHTML)
	}
	if err != nil {
		return fmt.Errorf("list themes: %w", err)
	}

	var text string
	if len(themes) == 0 {
		text = "📚 <b>Домашнее задание</b>\n\n" +
			"По этому предмету пока нет тем с домашним заданием."
	} else {
		text = fmt.Sprintf(
			"📚 <b>Домашнее задание</b>\n\n"+
				"📘 <b>Предмет:</b> %s\n\n"+
				"<b>Статусы:</b>\n"+
				"🔴 — не сдано\n🟢 — сдано\n\n"+
				"<b>Выбери нужный раздел:</b>",
			html.EscapeString(utils.CleanSubjectName(themes[0].SubjectName)),
		)
	}

	return h.editMessage(ctx, b, chatID, messageID, text, keyboard.ThemesKeyboard(themes, subjectCode, page), models.ParseModeHTML)
}

// cbTheme показывает экран темы и переводит пользователя в режим
// ожидания файла. arg — код темы.
func (h *Handler) cbTheme(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
	arg string,
) error {
	theme, err := h.svc.Theme(ctx, userID, arg)
	if errors.Is(err, domain.ErrNotFound) {
		text := "⚠️ <b>Тема недоступна</b>\n\n" +
			"Возможно, её убрали из выгрузки или у неё нет ДЗ."
		markup := keyboard.AddBackButton(keyboard.Empty(), "homework")
		return h.editMessage(ctx, b, chatID, messageID, text, markup, models.ParseModeHTML)
	}
	if err != nil {
		return fmt.Errorf("get theme: %w", err)
	}

	count, err := h.svc.SubmissionCount(ctx, userID, theme.Code)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("submission count: %w", err)
	}

	text := h.themeScreenText(theme, count, h.svc.MaxSubmissions(), "")

	// Возвращаемся на 0-ю страницу предмета
	markup := keyboard.AddBackButton(keyboard.Empty(), keyboard.CallbackSubject(theme.SubjectCode, 0))

	if err := h.editMessage(ctx, b, chatID, messageID, text, markup, models.ParseModeHTML); err != nil {
		return err
	}

	// Лимит исчерпан — в режим ожидания файла не переводим. Заодно
	// чистим состояние: если пользователь висел в StateWaitingFile
	// по другой теме, он не должен залипнуть в старом режиме.
	if count >= h.svc.MaxSubmissions() {
		h.states.Clear(userID)
		return nil
	}

	// Переводим в режим ожидания файла только после показа экрана:
	// при ошибке редактирования пользователь не застрянет в режиме без подсказки.
	// Параллельно запоминаем тему как последнюю открытую — если юзер
	// уйдёт и вернётся с файлом вне контекста, предложим продолжить.
	h.states.WaitFile(userID, theme.Code, messageID)
	h.states.RememberTheme(userID, theme.Code, messageID)
	return nil
}

// themeScreenText собирает экран сдачи по теме. warning показывается
// сверху, если нужно подсветить проблему (например, прислали текст
// вместо файла). Лимиты и форматы берутся из сервиса — тексты не
// расходятся с реальной валидацией.
//
// Если у студента осталась одна попытка (count == max-1), сверху
// добавляется предупреждение: сожжённая случайно финальная попытка —
// это несданная тема. Предупреждение всегда выше warning'а, чтобы
// после ошибок формата оно не прыгало вниз.
//
// Если тема сдана только в старом боте (theme.Submitted && count == 0),
// показываем отдельную строку: студент не должен думать, что «тема не
// сдана», если он сдавал её в старом боте. Строка исчезает после первой
// сдачи в новом боте — она уже неактуальна.
func (h *Handler) themeScreenText(theme domain.Theme, count, max int, warning string) string {
	text := "📤 <b>Сдача</b>\n\n"

	// max всегда > 0 (валидируется конфигом), но подстрахуемся: сравнение
	// с max-1 на нуле дало бы -1 и «последняя попытка» на первой сдаче.
	if max > 0 && count == max-1 {
		text += "⚠️ <b>Осталась последняя попытка.</b> Отправь финальную версию.\n\n"
	}

	// Сдано раньше, но в этом ещё ни разу — говорим
	// прямо, чтобы «Попытки: 0/3» не читалось как «ничего не сдано».
	if theme.Submitted && count == 0 {
		text += "✅ <b>Сдано ранее</b>. Попытки здесь ещё не тратились.\n\n"
	}

	if warning != "" {
		text += warning + "\n\n"
	}

	text += fmt.Sprintf(
		"📘 <b>Предмет:</b> %s\n"+
			"📌 <b>Тема:</b> %s\n\n"+
			"<b>Попытки:</b> %d/%d\n\n"+
			"<b>Правила:</b>\n"+
			"• Один файл до %d МБ\n"+
			"• Форматы: %s\n\n",
		html.EscapeString(utils.CleanSubjectName(theme.SubjectName)),
		html.EscapeString(utils.CleanThemeNameKeepNumber(theme.Name)),
		count, max,
		h.svc.MaxFileSizeMB(),
		h.svc.AllowedExtsDisplay(),
	)

	// Лимит попыток исчерпан — вместо «пришли файл» объясняем, что делать.
	// Файл здесь приниматься не будет, поэтому не обещаем его ждать.
	if max > 0 && count >= max {
		text += "🚫 <b>Лимит попыток исчерпан.</b>\n"
		return text
	}

	text += "<b>Пришли файл следующим сообщением.</b>"
	return text
}

// cbMySubmissions показывает все темы студента с отметкой сдано/не сдано
// и строкой прогресса внизу. При 100 % — отдельная строка с тёплой фразой
// из fullProgressKindWords: закрытый список заслуживает акцента, а не
// сухого «Прогресс: 10 из 10».
//
// «Сдано» учитывает и исторические сдачи из старого бота (homework в
// snapshot.db): Attempts — это попытки в НОВОМ боте, Submitted — сдано
// в новом ИЛИ в старом. Иконка 🟢 и прогресс считаются по Submitted.
func (h *Handler) cbMySubmissions(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	summaries, err := h.svc.SubmissionSummaries(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return h.editMessage(ctx, b, chatID, messageID, notLoggedText, keyboard.AuthKeyboard(), models.ParseModeHTML)
	}
	if err != nil {
		return fmt.Errorf("submission summaries: %w", err)
	}

	if len(summaries) == 0 {
		text := "📊 <b>Мои сдачи</b>\n\nПока нет предметов с домашними заданиями."
		markup := keyboard.AddBackButton(keyboard.Empty(), "main_menu")
		return h.editMessage(ctx, b, chatID, messageID, text, markup, models.ParseModeHTML)
	}

	var sb strings.Builder
	sb.WriteString("📊 <b>Мои сдачи</b>\n\n")

	const indent = "\u00A0\u00A0\u00A0"

	var currentSubject int64 = -1
	total := 0
	submitted := 0

	for _, s := range summaries {
		total++
		if s.Submitted {
			submitted++
		}

		if s.SubjectCode != currentSubject {
			if currentSubject != -1 {
				sb.WriteString("\n")
			}
			sb.WriteString(fmt.Sprintf("📘 <b>%s:</b>\n", html.EscapeString(utils.CleanSubjectName(s.SubjectName))))
			currentSubject = s.SubjectCode
		}

		icon := "🔴"
		if s.Submitted {
			icon = "🟢"
		}
		sb.WriteString(fmt.Sprintf("%s%s %s\n",
			indent,
			icon,
			html.EscapeString(utils.CapitalizeAfterThemePrefix(s.ThemeName)),
		))
	}

	if submitted == total {
		sb.WriteString("\n🎉 <b>Всё сдано!</b>\n")
		sb.WriteString(html.EscapeString(pickKindWord(fullProgressKindWords)))
	} else {
		sb.WriteString(fmt.Sprintf("\n📈 <b>Прогресс:</b> %d из %d", submitted, total))
	}

	markup := keyboard.AddBackButton(keyboard.Empty(), "main_menu")
	return h.editMessage(ctx, b, chatID, messageID, sb.String(), markup, models.ParseModeHTML)
}

// cbCancelResume — реакция на «Отмена» после подсказки о продолжении
// сдачи. Забываем «последнюю тему», чтобы не напоминать снова, и
// показываем главное меню.
func (h *Handler) cbCancelResume(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	messageID int,
) error {
	h.states.ClearLastTheme(userID)
	return h.editMessage(ctx, b, chatID, messageID,
		"👍 Хорошо, не напоминаю.\n\nЕсли захочешь сдать — открой «📚 Домашнее задание».",
		keyboard.MainMenuKeyboard(), models.ParseModeHTML)
}
