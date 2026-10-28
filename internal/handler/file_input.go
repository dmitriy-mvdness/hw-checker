// file_input.go — приём документов от пользователя: сдача ДЗ,
// реакция на текст в режиме ожидания файла, скачивание.
package handler

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
	"github.com/dmitriy-mvdness/hw-checker/internal/keyboard"
	"github.com/dmitriy-mvdness/hw-checker/internal/session"
	"github.com/dmitriy-mvdness/hw-checker/internal/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

// handleDocument принимает документы от пользователя. Если пользователь
// ждёт файл для сдачи ДЗ — принимает его; иначе игнорирует.
func (h *Handler) handleDocument(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	msg := update.Message
	if msg == nil || msg.From == nil || msg.Document == nil {
		return
	}

	userID := msg.From.ID
	chatID := msg.Chat.ID

	// Атомарно забираем состояние: защита от повторной обработки
	// одинаковых апдейтов (например, пользователь дважды нажал «отправить»).
	sess, ok := h.states.TakeFile(userID)
	if !ok {
		return
	}

	// Входящее сообщение убираем из чата, чтобы он оставался чистым.
	// Косметика — Debug.
	if err := h.deleteMessage(ctx, b, chatID, msg.ID); err != nil {
		h.log.Debug("не удалось удалить сообщение пользователя",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}

	doc := msg.Document

	if !h.svc.ValidateExt(doc.FileName) {
		h.showThemeWithWarning(ctx, b, userID, chatID, sess,
			"❌ <b>Неверный формат файла!</b> Пришли файл одного из разрешённых типов")
		return
	}

	if int64(doc.FileSize) > h.svc.MaxFileSize() {
		h.askFileAgain(ctx, b, userID, chatID, sess,
			fmt.Sprintf("❌ <b>Файл слишком большой</b>\n\n"+
				"Максимум — %d МБ. Сожми файл и отправь ещё раз.", h.svc.MaxFileSizeMB()))
		return
	}

	// Проверяем тему ДО скачивания: если её убрали из выгрузки или
	// доступ потерян, покажем экран недоступной темы и не будем
	// тратить трафик на 20 МБ. Без этой проверки ErrNotFound после
	// SubmitHomework не отличить от не "залогинен": студент увидит
	// "Сначала войди в аккаунт", хотя он уже авторизован.
	theme, terr := h.svc.Theme(ctx, userID, sess.ThemeCode)
	if errors.Is(terr, domain.ErrNotFound) {
		h.states.Clear(userID)
		text := "⚠️ <b>Тема недоступна</b>\n\n" +
			"Возможно, её убрали из выгрузки или у неё нет ДЗ."
		markup := keyboard.AddBackButton(keyboard.Empty(), keyboard.CbHomework)
		if eerr := h.editMessage(ctx, b, chatID, sess.MessageID, text, markup, models.ParseModeHTML); eerr != nil {
			h.log.Warn("не удалось показать экран недоступной темы",
				zap.Int64("user_id", userID),
				zap.String("theme", sess.ThemeCode),
				zap.Error(eerr),
			)
		}
		return
	}
	if terr != nil {
		h.log.Error("не удалось получить тему для экрана загрузки",
			zap.Int64("user_id", userID),
			zap.String("theme", sess.ThemeCode),
			zap.Error(terr),
		)
	}

	// Файл прошёл проверки — показываем прогресс на том же сообщении,
	// что и экран темы. Пока downloadFile тянет 20 МБ из Telegram
	// (5–30 секунд), студент видит, что бот реагирует, и не отправляет
	// файл второй раз. Клавиатуру снимаем на время: keyboard.Empty()
	// сериализуется в {"inline_keyboard":[]} и Telegram убирает кнопки.
	// Если бы передали nil, клавиатура осталась бы как есть, и по «Назад»
	// можно было бы уйти с экрана, пока идёт загрузка.
	//
	// Дополнительный запрос к snapshot оправдан: SQLite, ~1мс, зато
	// экран прогресса визуально совпадает с экраном темы — студент
	// видит те же предмет и тему, плавный переход без скачков.
	// Если тема не тянется (удалили из выгрузки) — показываем прогресс
	// без украшений, следом ошибка из SubmitHomework всё равно сработает.
	var themePtr *domain.Theme
	if terr == nil {
		themePtr = &theme
	}
	if err := h.editMessage(ctx, b, chatID, sess.MessageID,
		progressText(doc.FileName, int64(doc.FileSize), themePtr),
		keyboard.Empty(), models.ParseModeHTML); err != nil {
		h.log.Debug("не удалось показать экран загрузки файла",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}

	data, err := h.downloadFile(ctx, b, doc.FileID)
	if err != nil {
		h.log.Error("не удалось скачать файл из Telegram",
			zap.Int64("user_id", userID),
			zap.String("file_name", doc.FileName),
			zap.Error(err),
		)
		h.askFileAgain(ctx, b, userID, chatID, sess,
			"😕 <b>Не удалось скачать файл</b>\n\nПопробуй ещё раз чуть позже.")
		return
	}

	sub, err := h.svc.SubmitHomework(ctx, userID, sess.ThemeCode, doc.FileName, data)
	switch {
	case err == nil:
		h.states.ClearLastTheme(userID) // забываем тему, чтобы старый экран не всплывал при orphan-апдейте
		h.showSubmitSuccess(ctx, b, chatID, sess, sub)
		h.log.Info("работа сдана",
			zap.Int64("user_id", userID),
			zap.String("theme", sess.ThemeCode),
			zap.Int("attempt", sub.Attempt),
			zap.String("path", sub.FilePath),
		)

	case errors.Is(err, domain.ErrTooManyAttempts):
		text := fmt.Sprintf("🚫 <b>Лимит попыток исчерпан</b>\n\n"+
			"По этой теме уже %d сдачи. Если нужно исправить — напиши в поддержку.",
			h.svc.MaxSubmissions())
		markup := keyboard.AddBackButton(keyboard.Empty(), keyboard.CbHomework)
		if eerr := h.editMessage(ctx, b, chatID, sess.MessageID, text, markup, models.ParseModeHTML); eerr != nil {
			h.log.Warn("не удалось показать экран лимита попыток",
				zap.Int64("user_id", userID),
				zap.Error(eerr),
			)
		}

	case errors.Is(err, domain.ErrNotFound):
		// ErrNotFound значит либо «аккаунт/студент пропал», либо
		// «тема пропала» (если проверка до скачивания не сработала
		// из-за гонки с выгрузкой 1С). Различаем через CurrentStudent:
		// если аккаунт на месте — это не «не залогинен», а «нет темы».
		if _, serr := h.svc.CurrentStudent(ctx, userID); errors.Is(serr, domain.ErrNotFound) {
			if eerr := h.editMessage(ctx, b, chatID, sess.MessageID, notLoggedText, keyboard.AuthKeyboard(), models.ParseModeHTML); eerr != nil {
				h.log.Warn("не удалось показать экран входа",
					zap.Int64("user_id", userID),
					zap.Error(eerr),
				)
			}
			return
		}
		text := "⚠️ <b>Тема недоступна</b>\n\n" +
			"Возможно, её убрали из выгрузки или у неё нет ДЗ."
		markup := keyboard.AddBackButton(keyboard.Empty(), keyboard.CbHomework)
		if eerr := h.editMessage(ctx, b, chatID, sess.MessageID, text, markup, models.ParseModeHTML); eerr != nil {
			h.log.Warn("не удалось показать экран недоступной темы",
				zap.Int64("user_id", userID),
				zap.String("theme", sess.ThemeCode),
				zap.Error(eerr),
			)
		}

	default:
		h.log.Error("ошибка сдачи работы",
			zap.Int64("user_id", userID),
			zap.String("theme", sess.ThemeCode),
			zap.Error(err),
		)
		h.askFileAgain(ctx, b, userID, chatID, sess,
			"😕 <b>Что-то пошло не так</b>\n\nПопробуй ещё раз чуть позже.")
	}
}

// progressText собирает промежуточный экран на время скачивания файла.
// Формат совпадает с themeScreenText: та же иконка 📤, тот же HTML, те же
// эмодзи-префиксы — студент видит плавный переход, а не скачок на другой
// экран. theme == nil, если данные о теме не удалось получить (например,
// её убрали из выгрузки) — тогда прогресс показывается без шапки.
//
// Размер показываем только если он известен: Telegram иногда отдаёт
// FileSize = 0, и «0.0 МБ» выглядит как ошибка. Мелкие файлы показываем
// в КБ — «0.3 МБ» для скриншота выглядит странно.
func progressText(fileName string, sizeBytes int64, theme *domain.Theme) string {
	var sb strings.Builder

	sb.WriteString("📤 <b>Сдача</b>\n\n")

	if theme != nil {
		sb.WriteString(fmt.Sprintf("📘 <b>Предмет:</b> %s\n",
			html.EscapeString(utils.CleanSubjectName(theme.SubjectName))))
		sb.WriteString(fmt.Sprintf("📌 <b>Тема:</b> %s\n\n",
			html.EscapeString(utils.CleanThemeName(theme.Name))))
	}

	sb.WriteString("⏳ <b>Скачиваю твой файл...</b>\n\n")
	sb.WriteString(fmt.Sprintf(
		"Это может занять до минуты, если файл большой. "+
			"Можешь пока сходить за %s.\n\n", randomErrand()))
	sb.WriteString(fmt.Sprintf("📎 <b>Имя:</b> <code>%s</code>",
		html.EscapeString(disarmURL(fileName))))

	if sizeBytes > 0 {
		const (
			kb = 1024.0
			mb = 1024.0 * 1024.0
		)
		if sizeBytes < int64(mb) {
			sb.WriteString(fmt.Sprintf("\n📦 <b>Размер:</b> %.0f КБ", float64(sizeBytes)/kb))
		} else {
			sb.WriteString(fmt.Sprintf("\n📦 <b>Размер:</b> %.1f МБ", float64(sizeBytes)/mb))
		}
	}

	return sb.String()
}

// randomErrand возвращает случайный элемент randomErrands. Math/rand/v2
// в глобальном варианте безопасен для конкурентного использования и
// автоматически сеется, никаких rand.Seed не нужно.
func randomErrand() string {
	return randomErrands[rand.IntN(len(randomErrands))]
}

// disarmURL разрывает в имени файла паттерн «имя.расширение» для
// автоссылок Telegram.
func disarmURL(name string) string {
	return strings.ReplaceAll(name, ".", ".\u200B")
}

// handleTextWhileWaitingFile — реакция на текст, пока пользователь в
// режиме отправки файла. Сообщение удаляется, экран темы обновляется
// подсказкой. Состояние сохраняется: пользователь может прислать файл.
func (h *Handler) handleTextWhileWaitingFile(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	msg := update.Message
	if err := h.deleteMessage(ctx, b, msg.Chat.ID, msg.ID); err != nil {
		h.log.Debug("не удалось удалить сообщение пользователя",
			zap.Int64("user_id", msg.From.ID),
			zap.Error(err),
		)
	}

	sess := h.states.Get(msg.From.ID)
	if sess.State != session.StateWaitingFile {
		return
	}

	h.showThemeWithWarning(ctx, b, msg.From.ID, msg.Chat.ID, sess,
		"⚠️ <b>Неверный формат.</b> Пришли файл одного из разрешённых типов")
}

// askFileAgain возвращает пользователя в режим ожидания файла и
// показывает подсказку на экране темы.
func (h *Handler) askFileAgain(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	sess session.Session,
	text string,
) {
	h.states.WaitFile(userID, sess.ThemeCode, sess.MessageID)
	markup := keyboard.AddBackButton(keyboard.Empty(), keyboard.CbHomework)
	if err := h.editMessage(ctx, b, chatID, sess.MessageID, text, markup, models.ParseModeHTML); err != nil {
		h.log.Warn("не удалось обновить экран темы",
			zap.Int64("user_id", userID),
			zap.String("theme", sess.ThemeCode),
			zap.Error(err),
		)
	}
}

// showThemeWithWarning перерисовывает экран темы с предупреждением
// сверху. Используется, когда пользователь прислал не то, что нужно,
// но состояние сдачи сохраняется — он может попробовать снова.
func (h *Handler) showThemeWithWarning(
	ctx context.Context,
	b *bot.Bot,
	userID, chatID int64,
	sess session.Session,
	warning string,
) {
	theme, err := h.svc.Theme(ctx, userID, sess.ThemeCode)
	if errors.Is(err, domain.ErrNotFound) {
		h.states.Clear(userID)
		text := "⚠️ <b>Тема недоступна</b>\n\n" +
			"Возможно, её убрали из выгрузки."
		markup := keyboard.AddBackButton(keyboard.Empty(), keyboard.CbHomework)
		if eerr := h.editMessage(ctx, b, chatID, sess.MessageID, text, markup, models.ParseModeHTML); eerr != nil {
			h.log.Warn("не удалось показать экран недоступной темы",
				zap.Int64("user_id", userID),
				zap.Error(eerr),
			)
		}
		return
	}
	if err != nil {
		h.log.Error("не удалось получить тему",
			zap.Int64("user_id", userID),
			zap.String("theme", sess.ThemeCode),
			zap.Error(err),
		)
		return
	}

	count, err := h.svc.SubmissionCount(ctx, userID, sess.ThemeCode)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		h.log.Error("не удалось посчитать попытки",
			zap.Int64("user_id", userID),
			zap.String("theme", sess.ThemeCode),
			zap.Error(err),
		)
		return
	}

	text := h.themeScreenText(theme, count, h.svc.MaxSubmissions(), warning)
	markup := keyboard.AddBackButton(keyboard.Empty(), keyboard.CallbackSubject(theme.SubjectCode))

	// Лимит мог закончиться между прошлым экраном и этим апдейтом —
	// например, пока юзер отправлял файлы. Тогда в режим ожидания
	// не возвращаемся, экран уже объяснил ситуацию. Состояние чистим:
	// если юзер висел в StateWaitingFile по другой теме, он не должен
	// залипнуть в старом режиме.
	if count >= h.svc.MaxSubmissions() {
		h.states.Clear(userID)
		if eerr := h.editMessage(ctx, b, chatID, sess.MessageID, text, markup, models.ParseModeHTML); eerr != nil {
			h.log.Warn("не удалось показать экран лимита",
				zap.Int64("user_id", userID),
				zap.Error(eerr),
			)
		}
		return
	}

	h.states.WaitFile(userID, sess.ThemeCode, sess.MessageID)

	if eerr := h.editMessage(ctx, b, chatID, sess.MessageID, text, markup, models.ParseModeHTML); eerr != nil {
		h.log.Warn("не удалось обновить экран темы",
			zap.Int64("user_id", userID),
			zap.String("theme", sess.ThemeCode),
			zap.Error(eerr),
		)
	}
}

// showSubmitSuccess редактирует экран темы: подтверждение + статус.
// Между фактом сдачи и подсказкой — короткая тёплая фраза из
// submitKindWords: текст не должен превращаться в сухой чек.
//
// Если остались попытки — показываем кнопку «Сдать ещё», которая
// возвращает в меню предметов. Если это была последняя попытка —
// кнопку не показываем, отправляем в поддержку.
func (h *Handler) showSubmitSuccess(
	ctx context.Context,
	b *bot.Bot,
	chatID int64,
	sess session.Session,
	sub domain.Submission,
) {
	tail := "Есть что сдать ещё? Жми «Сдать ещё» 👇"

	kb := keyboard.Empty()

	// Лимит попыток исчерпан — кнопку «Сдать ещё» не даём, это было бы враньё.
	if sub.Attempt >= h.svc.MaxSubmissions() {
		tail = "Это была последняя попытка. Если что-то пошло не так — напиши в поддержку."
	} else {
		// Ведём пользователя в общий список предметов, чтобы он мог
		// выбрать другую тему или предмет для сдачи.
		kb.InlineKeyboard = append(kb.InlineKeyboard, []models.InlineKeyboardButton{
			{Text: "📤 Сдать ещё", CallbackData: keyboard.CbHomework},
		})
	}

	text := fmt.Sprintf(
		"✅ <b>Работа принята</b>\n\n"+
			"<b>Попытка:</b> %d/%d\n"+
			"<b>Файл:</b> <code>%s</code>\n\n"+
			"🎉 %s\n\n"+
			"%s",
		sub.Attempt, h.svc.MaxSubmissions(),
		html.EscapeString(disarmURL(sub.OriginalName)),
		pickKindWord(submitKindWords),
		tail,
	)

	// Кнопка «Назад» теперь ведёт в главное меню, чтобы не дублировать «Сдать ещё».
	markup := keyboard.AddBackButton(kb, keyboard.CbMainMenu)

	if err := h.editMessage(ctx, b, chatID, sess.MessageID, text, markup, models.ParseModeHTML); err != nil {
		h.log.Warn("не удалось показать экран успешной сдачи",
			zap.Int64("student_code", sub.StudentCode),
			zap.String("theme", sub.ThemeCode),
			zap.Error(err),
		)
	}
}

// downloadFile скачивает файл из Telegram. Читает не больше maxFileSize+1
// байт, чтобы не тянуть в память больше, чем нужно.
func (h *Handler) downloadFile(ctx context.Context, b *bot.Bot, fileID string) ([]byte, error) {
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("get file: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.FileDownloadLink(file), nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, h.svc.MaxFileSize()+1))
}

// handlePhotoWhileWaitingFile — реакция на фото, пока пользователь в
// режиме отправки файла ДЗ. Фото в ДЗ запрещены: принимаем только
// документом или архивом. Сообщение удаляем, экран обновляем подсказкой.
// Состояние сохраняется: пользователь может прислать документ.
func (h *Handler) handlePhotoWhileWaitingFile(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	msg := update.Message
	if err := h.deleteMessage(ctx, b, msg.Chat.ID, msg.ID); err != nil {
		h.log.Debug("не удалось удалить сообщение пользователя",
			zap.Int64("user_id", msg.From.ID),
			zap.Error(err),
		)
	}

	sess := h.states.Get(msg.From.ID)
	if sess.State != session.StateWaitingFile {
		return
	}

	h.showThemeWithWarning(ctx, b, msg.From.ID, msg.Chat.ID, sess,
		"⚠️ <b>Фото не принимаются.</b> Пришли ДЗ файлом(документом) одного из разрешённых форматов")
}

// handleOrphanUpdate реагирует на апдейт, который пришёл, когда бот
// не ждёт файл, код, или обращение в поддержку. Если у юзера есть
// свежая «последняя открытая тема» (кликнул, ушёл, WaitingFile
// протух, вернулся и что-то прислал) — редактируем экран темы в
// подсказку «Продолжить сдачу?». Иначе тихо игнорируем.
//
// Принимает любой апдейт: текст, фото, документ, стикер, голосовое,
// видео и т.п. Сообщение юзера удаляем — оно не пойдёт в дело, а
// висящее вложение без ответа сбивает с толку. Подсказку показываем
// один раз на одну тему: серия апдейтов (альбом, повторные файлы)
// не плодит дубликатов.
func (h *Handler) handleOrphanUpdate(
	ctx context.Context,
	b *bot.Bot,
	update *models.Update,
) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}

	userID := msg.From.ID
	chatID := msg.Chat.ID

	themeCode, msgID, hintShown, ok := h.states.LastTheme(userID)
	if !ok {
		return
	}

	// Сообщение юзера убираем — у стикеров, голосовых, видео тоже есть
	// msg.ID, так что удаление работает единообразно.
	if err := h.deleteMessage(ctx, b, chatID, msg.ID); err != nil {
		h.log.Debug("не удалось удалить orphan-сообщение юзера",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}

	// Подсказка уже висит в чате — вторую не показываем.
	if hintShown {
		return
	}

	theme, err := h.svc.Theme(ctx, userID, themeCode)
	if errors.Is(err, domain.ErrNotFound) {
		h.states.ClearLastTheme(userID)
		return
	}
	if err != nil {
		h.log.Error("не удалось получить тему для orphan-апдейта",
			zap.Int64("user_id", userID),
			zap.String("theme", themeCode),
			zap.Error(err),
		)
		return
	}

	// Лимит мог упереться, пока юзер гулял. Предлагать «Продолжить
	// сдачу?» бессмысленно — сдавать нечего. Забываем тему, экран
	// будет ждать следующего действия (юзер откроет тему заново и
	// увидит «лимит исчерпан»).
	count, cerr := h.svc.SubmissionCount(ctx, userID, theme.Code)
	if cerr != nil && !errors.Is(cerr, domain.ErrNotFound) {
		h.log.Error("не удалось посчитать попытки для orphan-апдейта",
			zap.Int64("user_id", userID),
			zap.String("theme", themeCode),
			zap.Error(cerr),
		)
		return
	}
	if count >= h.svc.MaxSubmissions() {
		h.states.ClearLastTheme(userID)
		return
	}

	text := fmt.Sprintf(
		"📤 <b>Продолжить сдачу?</b>\n\n"+
			"📘 <b>Предмет:</b> %s\n"+
			"📌 <b>Тема:</b> %s\n\n"+
			"Ты открывал эту тему раньше — но сессия сдачи сбросилась.",
		html.EscapeString(utils.CleanSubjectName(theme.SubjectName)),
		html.EscapeString(utils.CleanThemeName(theme.Name)),
	)

	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "▶️ Продолжить сдачу", CallbackData: keyboard.CallbackResume(theme.Code)},
			},
			{
				{Text: "⬅️ Отмена", CallbackData: keyboard.CbCancelResume},
			},
		},
	}

	if err := h.editMessage(ctx, b, chatID, msgID, text, kb, models.ParseModeHTML); err != nil {
		h.log.Warn("не удалось превратить экран темы в подсказку о продолжении",
			zap.Int64("user_id", userID),
			zap.Int("msg_id", msgID),
			zap.Error(err),
		)
		return
	}
	h.states.MarkThemeHintShown(userID)
}

// randomErrands — чем можно заняться, пока бот тащит файл. Некая пасхалочка.
// Фраза выбирается случайно при каждом показе экрана загрузки.
//
// Строки живут в rodata, копий не создаётся. rand.IntN — это пара
// наносекунд и ноль аллокаций, так что пасхалко бесплатна.
var randomErrands = []string{
	"чаем",
	"кофе",
	"какао",
	"соком",
	"компотом",
	"лимонадом",
	"смузи",
	"матэ",
	"капучино",
	"латте",
	"рафом на",
	"эспрессо",
	"печеньем",
	"пряником",
	"круассаном",
	"пончиком",
	"вафлей",
	"эклером",
	"чизкейком",
	"тирамису",
	"конфетой",
	"мармеладом",
	"зефиром",
	"мороженым",
	"пирожным",
	"яблоком",
	"бананом",
	"апельсином",
	"мандарином",
	"грушей",
	"персиком",
	"виноградом",
	"киви",
	"манго",
	"арбузом",
	"клубникой",
	"помело (что такое)",
	"бутербродом",
	"сэндвичем",
	"пирожком",
	"шаурмой",
	"бургером",
	"пиццей",
	"суши",
	"пельменями",
	"блинчиками",
	"сырниками",
	"дошираком",
	"борщом",
	"окрошкой",
	"наггетсами",
	"картошкой фри",
	"чизбургером",
	"орешкамимими",
	"семечками",
	"чипсами",
	"сухариками",
	"попкорном",
	"сушкой",
	"фисташками",
	"кешью",
	"счастьем",
	"вдохновением",
	"смыслом",
	"свежими новостями",
	"лунной пылью",
	"звёздочкой с неба",
	"чашкой хорошего настроения",
	"порцией позитива",
	"пледом и сериалом",
	"котом в мешке",
	"единорогом",
	"драконом",
	"миллионом долларов",
	"билетом на Маяс",
	"волшебной палочкой",
	"машиной времени",
	"шапкой-невидимкой",
	"левым носком",
	"правым тапком",
	"синей изолентой",
	"WD-40",
	"скотчем",
	"гайкой",
	"болтиком",
	"шурупом",
	"паяльником",
	"роутером",
	"павербанком",
	"наушниками",
	"мышкой",
	"клавиатурой",
	"монитором",
	"видеокартой",
	"оперативкой",
	"флешкой",
	"батарейкой",
	"лампочкой",
	"ИИ",
	"кринжем",
	"мемом",
	"хайпом",
	"лайками",
	"стримом",
	"тиктоком",
	"рилсом",
	"сторис",
	"лайфхаком",
	"советом",
	"рецептом",
	"анекдотом",
	"шуткой",
	"приколом",
	"испанским стыдом",
	"чувством юмора",
	"сарказмом",
	"абсурдом",
	"чёрной дырой",
	"бесконечностью",
	"хаосом",
	"сахаром",
	"солью",
	"перцем",
	"кетчупом",
	"майонезом",
	"горчицей",
	"васаби",
	"корицей",
	"ванилью",
	"трюфелем",
	"икрой",
	"медитацией",
	"йогой",
	"дзеном",
	"кармой",
	"чакрой",
	"аурой",
	"гороскопом",
	"таро",
	"рунами",
	"гаданием",
	"заклинанием",
	"зельем",
	"грифоном",
	"фениксом",
	"йети",
	"пришельцем",
	"НЛО",
	"плоской Землёй",
	"5G",
	"красной таблеткой",
	"синей таблеткой",
	"кроличьей норой",
	"Хогвартсом",
	"Средиземьем",
	"Нарнией",
	"Вестеросом",
	"золотой рыбкой",
	"щукой",
	"Колобком",
	"GTA VI",
	"питцей",
}
