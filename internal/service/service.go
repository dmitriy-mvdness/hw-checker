// Package service содержит бизнес-логику бота: вход по коду, сдача ДЗ,
// обращения в поддержку, синхронизация новостей. Сервис работает через
// интерфейсы репозиториев и файлового хранилища.
package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// maxCodeLen отсекает заведомо неверный ввод, не обращаясь к базе.
const maxCodeLen = 32

// ServiceConfig — лимиты и правила, которые сервис получает из конфига.
type ServiceConfig struct {
	MaxSubmissions int
	MaxFileSize    int64
	AllowedExts    []string
}

// Service связывает репозитории, файловое хранилище и конфиг.
type Service struct {
	accounts    AccountRepository
	students    StudentRepository
	homework    HomeworkRepository
	legacy      LegacySubmissionRepository
	submissions SubmissionRepository
	news        NewsRepository
	files       FileStorage

	// loginLimiter — защита от перебора кодов: fixed window, счётчик
	// попыток на пользователя. Если кто-то в цикле дёргает Login,
	// лимитер перестаёт пускать до конца окна.
	loginLimiter LoginLimiter

	// log используется только для некритичных операций, которые не
	// должны прерывать основной поток: удаление файла-сироты после
	// неудачной записи в БД, best-effort очистка заявки на код,
	// fallback на legacy-сдачи при недоступной homework.
	// Критичные ошибки возвращаются вызывающему как error.
	log *zap.Logger

	maxSubmissions int
	maxFileSize    int64

	allowedExts map[string]bool
}

// New собирает сервис и нормализует список разрешённых расширений.
func New(
	accounts AccountRepository,
	students StudentRepository,
	homework HomeworkRepository,
	legacy LegacySubmissionRepository,
	submissions SubmissionRepository,
	news NewsRepository,
	files FileStorage,
	loginLimiter LoginLimiter,
	log *zap.Logger,
	cfg ServiceConfig,
) *Service {
	exts := make(map[string]bool, len(cfg.AllowedExts))
	for _, e := range cfg.AllowedExts {
		exts[strings.ToLower(strings.TrimPrefix(e, "."))] = true
	}
	return &Service{
		accounts:       accounts,
		students:       students,
		homework:       homework,
		legacy:         legacy,
		submissions:    submissions,
		news:           news,
		files:          files,
		loginLimiter:   loginLimiter,
		log:            log,
		maxSubmissions: cfg.MaxSubmissions,
		maxFileSize:    cfg.MaxFileSize,
		allowedExts:    exts,
	}
}

// MaxFileSize возвращает размер одного файла в байтах.
func (s *Service) MaxFileSize() int64 { return s.maxFileSize }

// MaxFileSizeMB возвращает лимит размера файла в мегабайтах для
// отображения в текстах.
func (s *Service) MaxFileSizeMB() int64 {
	return s.maxFileSize / (1024 * 1024)
}

// AllowedExtsDisplay возвращает список разрешённых расширений,
// отсортированный по алфавиту и разделённый запятыми.
func (s *Service) AllowedExtsDisplay() string {
	exts := make([]string, 0, len(s.allowedExts))
	for ext := range s.allowedExts {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return strings.Join(exts, ", ")
}

// MaxSubmissions возвращает лимит попыток на тему.
func (s *Service) MaxSubmissions() int { return s.maxSubmissions }

// ValidateExt проверяет расширение файла по белому списку.
func (s *Service) ValidateExt(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	ext = strings.TrimPrefix(ext, ".")
	return s.allowedExts[ext]
}

// SubmissionCount возвращает, сколько попыток студент уже сделал по теме.
func (s *Service) SubmissionCount(ctx context.Context, userID int64, themeCode string) (int, error) {
	student, err := s.CurrentStudent(ctx, userID)
	if err != nil {
		return 0, err
	}
	return s.submissions.CountByTheme(ctx, student.Code, themeCode)
}

// SubmitHomework сохраняет файл и записывает сдачу.
//
// Возвращаемые ошибки:
//
//	domain.ErrNotFound        — нет аккаунта, студент пропал, темы нет;
//	domain.ErrBadFileType     — недопустимое расширение;
//	domain.ErrFileTooLarge    — файл превышает лимит;
//	domain.ErrTooManyAttempts — лимит попыток исчерпан.
//
// Legacy-сдачи (homework старого бота) в лимит попыток НЕ входят:
// там колонки attempt нет, лимита не было, значит и в новом боте
// студент начинает с чистого счётчика.
func (s *Service) SubmitHomework(
	ctx context.Context,
	userID int64,
	themeCode, originalName string,
	data []byte,
) (domain.Submission, error) {
	student, err := s.CurrentStudent(ctx, userID)
	if err != nil {
		return domain.Submission{}, err
	}

	// Повторная проверка доступа: не доверяем коду темы из callback.
	theme, err := s.homework.GetTheme(ctx, student.Code, themeCode)
	if err != nil {
		return domain.Submission{}, err
	}

	if !s.ValidateExt(originalName) {
		return domain.Submission{}, domain.ErrBadFileType
	}
	if int64(len(data)) > s.maxFileSize {
		return domain.Submission{}, domain.ErrFileTooLarge
	}

	count, err := s.submissions.CountByTheme(ctx, student.Code, themeCode)
	if err != nil {
		return domain.Submission{}, err
	}
	if count >= s.maxSubmissions {
		return domain.Submission{}, domain.ErrTooManyAttempts
	}
	attempt := count + 1

	// Порядок: проверки → файл → БД. Если БД не приняла запись, файл удаляем.
	relPath, err := s.files.SaveHomework(student, theme, attempt, originalName, data)
	if err != nil {
		return domain.Submission{}, err
	}

	sub := domain.Submission{
		StudentCode:  student.Code,
		ThemeCode:    themeCode,
		Attempt:      attempt,
		FilePath:     relPath,
		OriginalName: originalName,
	}
	id, err := s.submissions.Create(ctx, sub)
	if err != nil {
		// Откат файла — best-effort. Если Remove упадёт, на диске
		// останется файл-сирота: в БД записи нет, никто его не удалит.
		// Логируем Error, чтобы инцидент был виден при разборе.
		if rerr := s.files.Remove(relPath); rerr != nil {
			s.log.Error("не удалось удалить файл после ошибки БД",
				zap.Int64("user_id", userID),
				zap.String("path", relPath),
				zap.NamedError("remove_error", rerr),
				zap.NamedError("db_error", err),
			)
		}
		return domain.Submission{}, err
	}
	sub.ID = id

	return sub, nil
}

// Login проверяет персональный код и привязывает его к Telegram-аккаунту.
// Первым делом списывает попытку у лимитера — флуд отсекается до БД.
// Успешный вход сбрасывает окно лимитера: остатки попыток больше не нужны.
//
// Возвращаемые ошибки:
//
//	domain.ErrTooManyAttempts — лимит попыток исчерпан, подожди;
//	domain.ErrNotFound        — студент с таким кодом не найден;
//	domain.ErrCodeTaken       — код уже привязан к другому аккаунту;
//	domain.ErrAccountExists   — у пользователя уже есть аккаунт.
func (s *Service) Login(ctx context.Context, userID int64, code string) (domain.Student, error) {
	// Лимит проверяем ДО всего остального: смысл в том, чтобы не
	// нагружать БД и студенческий snapshot на флуде. Неудачные попытки
	// тоже тратят токен — иначе перебор не остановить.
	if !s.loginLimiter.Allow(userID) {
		return domain.Student{}, domain.ErrTooManyAttempts
	}

	code = strings.TrimSpace(code)
	if code == "" || len(code) > maxCodeLen {
		return domain.Student{}, domain.ErrNotFound
	}

	student, err := s.students.GetByPersonalCode(ctx, code)
	if err != nil {
		return domain.Student{}, err
	}

	if err := s.accounts.Create(ctx, userID, code); err != nil {
		return domain.Student{}, err
	}

	// Заявка на код больше не нужна — удаляем, чтобы не висела у админа.
	// Best-effort: если не удалить, ущерба нет, пользователь уже вошёл.
	// Debug — потому что при систематическом сбое (права на папку) это
	// полезно видеть, но Warn'ом шуметь не за что.
	if err := s.files.RemoveLoginRequest(userID); err != nil {
		s.log.Debug("не удалось удалить заявку на код после входа",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}

	// Успешный вход — сбрасываем окно: после logout пользователь должен
	// получить свежие попытки, а не остатки от прошлого окна.
	s.loginLimiter.Reset(userID)

	return student, nil
}

// CurrentStudent возвращает студента, к которому привязан аккаунт.
//
// Возвращает:
//
//   - domain.ErrNotFound — аккаунта нет или студент пропал из выгрузки.
func (s *Service) CurrentStudent(ctx context.Context, userID int64) (domain.Student, error) {
	acc, err := s.accounts.GetByTelegramID(ctx, userID)
	if err != nil {
		return domain.Student{}, err
	}

	student, err := s.students.GetByPersonalCode(ctx, acc.PersonalCode)
	if errors.Is(err, domain.ErrNotFound) {
		if derr := s.accounts.DeleteByTelegramID(ctx, userID); derr != nil {
			return domain.Student{}, fmt.Errorf("reset stale account: %w", derr)
		}
		return domain.Student{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Student{}, err
	}

	return student, nil
}

// Logout удаляет аккаунт пользователя.
func (s *Service) Logout(ctx context.Context, userID int64) error {
	return s.accounts.DeleteByTelegramID(ctx, userID)
}

// mergeLegacySubmitted доливает в map[theme]bool факты сдач из старого
// бота (homework в snapshot.db). Ошибка legacy-репозитория НЕ возвращается:
// историческая сдача — приятный бонус, без неё бот работает. Warn в лог,
// карта остаётся как была (только new-сдачи).
func (s *Service) mergeLegacySubmitted(
	ctx context.Context,
	userID int64,
	personalCode string,
	submitted map[string]bool,
	themeCodes []string,
) {
	if s.legacy == nil || len(themeCodes) == 0 {
		return
	}
	legacy, err := s.legacy.SubmittedThemeCodes(ctx, personalCode, themeCodes)
	if err != nil {
		s.log.Warn("не удалось прочитать исторические сдачи",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
		return
	}
	for code := range legacy {
		submitted[code] = true
	}
}

// Subjects возвращает предметы студента, привязанного к аккаунту.
func (s *Service) Subjects(ctx context.Context, userID int64) ([]domain.Subject, error) {
	student, err := s.CurrentStudent(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.homework.ListSubjects(ctx, student.Code)
}

// Themes возвращает темы предмета, доступные студенту.
// Каждой теме проставляется флаг Submitted, если по ней есть сдача
// в новом боте ИЛИ в старом (homework snapshot.db).
func (s *Service) Themes(ctx context.Context, userID int64, subjectCode int64) ([]domain.Theme, error) {
	student, err := s.CurrentStudent(ctx, userID)
	if err != nil {
		return nil, err
	}

	themes, err := s.homework.ListThemes(ctx, student.Code, subjectCode)
	if err != nil {
		return nil, err
	}
	if len(themes) == 0 {
		return themes, nil
	}

	codes := make([]string, len(themes))
	for i, t := range themes {
		codes[i] = t.Code
	}

	submitted, err := s.submissions.SubmittedThemeCodes(ctx, student.Code, codes)
	if err != nil {
		return nil, err
	}
	s.mergeLegacySubmitted(ctx, userID, student.PersonalCode, submitted, codes)

	for i := range themes {
		themes[i].Submitted = submitted[themes[i].Code]
	}

	return themes, nil
}

// Theme возвращает одну тему с повторной проверкой доступа.
// Флаг Submitted выставляется по union нового бота и legacy-сдач.
func (s *Service) Theme(ctx context.Context, userID int64, themeCode string) (domain.Theme, error) {
	student, err := s.CurrentStudent(ctx, userID)
	if err != nil {
		return domain.Theme{}, err
	}
	theme, err := s.homework.GetTheme(ctx, student.Code, themeCode)
	if err != nil {
		return domain.Theme{}, err
	}

	submitted := map[string]bool{}
	newSub, err := s.submissions.SubmittedThemeCodes(ctx, student.Code, []string{theme.Code})
	if err == nil {
		for code := range newSub {
			submitted[code] = true
		}
	}
	s.mergeLegacySubmitted(ctx, userID, student.PersonalCode, submitted, []string{theme.Code})
	theme.Submitted = submitted[theme.Code]

	return theme, nil
}

// SubmissionSummaries возвращает все темы студента (с ДЗ) с количеством
// попыток по каждой. Источники: snapshot.db (темы и предметы),
// storage.db (сдачи нового бота) и snapshot.db.homework (legacy-сдачи).
//
// Attempts — попытки в новом боте (лимит). Submitted — сдано в новом
// боте ИЛИ в старом (для иконки 🟢 на экране «Мои сдачи»).
func (s *Service) SubmissionSummaries(ctx context.Context, userID int64) ([]domain.SubmissionSummary, error) {
	student, err := s.CurrentStudent(ctx, userID)
	if err != nil {
		return nil, err
	}

	themes, err := s.homework.ListAllThemesByStudent(ctx, student.Code)
	if err != nil {
		return nil, err
	}
	if len(themes) == 0 {
		return nil, nil
	}

	codes := make([]string, len(themes))
	for i, t := range themes {
		codes[i] = t.Code
	}

	attempts, err := s.submissions.AttemptsByThemeCodes(ctx, student.Code, codes)
	if err != nil {
		return nil, err
	}

	submitted := make(map[string]bool, len(codes))
	for code, n := range attempts {
		if n > 0 {
			submitted[code] = true
		}
	}
	s.mergeLegacySubmitted(ctx, userID, student.PersonalCode, submitted, codes)

	result := make([]domain.SubmissionSummary, 0, len(themes))
	for _, t := range themes {
		result = append(result, domain.SubmissionSummary{
			SubjectCode: t.SubjectCode,
			SubjectName: t.SubjectName,
			ThemeCode:   t.Code,
			ThemeName:   t.Name,
			Attempts:    attempts[t.Code],
			Submitted:   submitted[t.Code],
		})
	}
	return result, nil
}

// RequestCode сохраняет запрос персонального кода.
//
// Возвращает:
//
//   - domain.ErrSupportRequestExists — запрос уже был отправлен.
func (s *Service) RequestCode(ctx context.Context, userID int64, name, username string) error {
	exists, err := s.files.LoginRequestExists(userID)
	if err != nil {
		return err
	}
	if exists {
		return domain.ErrSupportRequestExists
	}
	link := telegramLink(userID, username)
	_, err = s.files.SaveLoginRequest(userID, name, username, link)
	return err
}

// SubmitSupportText сохраняет текстовое обращение.
func (s *Service) SubmitSupportText(
	ctx context.Context,
	userID int64,
	reason, text, tgName, username string,
) error {
	req := s.buildSupportRequest(ctx, userID, reason, text, tgName, username)
	_, err := s.files.SaveSupportText(req)
	return err
}

// SubmitSupportFile сохраняет файл-обращение (плюс .txt с метаданными).
func (s *Service) SubmitSupportFile(
	ctx context.Context,
	userID int64,
	reason, originalName, text string,
	data []byte,
	tgName, username string,
) error {
	req := s.buildSupportRequest(ctx, userID, reason, text, tgName, username)
	_, err := s.files.SaveSupportFile(req, originalName, data)
	return err
}

// buildSupportRequest собирает данные для файла обращения. Если студент
// не авторизован, поля студента остаются нулевыми — это допустимо.
func (s *Service) buildSupportRequest(
	ctx context.Context,
	userID int64,
	reason, text, tgName, username string,
) domain.SupportRequest {
	req := domain.SupportRequest{
		Reason:       reason,
		Text:         text,
		TelegramID:   userID,
		TelegramName: tgName,
		Username:     username,
		Link:         telegramLink(userID, username),
	}

	student, err := s.CurrentStudent(ctx, userID)
	if err == nil {
		req.StudentName = student.Name
		req.GroupName = student.GroupName
		req.StreamName = student.StreamName
	}
	return req
}

// telegramLink строит ссылку на пользователя: через username, а если
// его нет — через tg://user?id=<ID>.
func telegramLink(userID int64, username string) string {
	if username != "" {
		return "https://t.me/" + username
	}
	return fmt.Sprintf("tg://user?id=%d", userID)
}

// SyncNews синхронизирует темы из snapshot.db с seen_themes в storage.db:
// новые добавляет в news, исчезнувшие — убирает.
func (s *Service) SyncNews(ctx context.Context) error {
	current, err := s.homework.ListAllHomeworkThemes(ctx)
	if err != nil {
		return err
	}
	currentSet := make(map[string]bool, len(current))
	for _, c := range current {
		currentSet[c] = true
	}

	initialized, err := s.news.IsInitialized(ctx)
	if err != nil {
		return err
	}

	// Первый запуск: запоминаем всё, что есть, но не пишем в news.
	if !initialized {
		if err := s.news.MarkSeen(ctx, current); err != nil {
			return err
		}
		return s.news.SetInitialized(ctx)
	}

	seen, err := s.news.ListSeen(ctx)
	if err != nil {
		return err
	}

	var added, removed []string
	for _, c := range current {
		if !seen[c] {
			added = append(added, c)
		}
	}
	for c := range seen {
		if !currentSet[c] {
			removed = append(removed, c)
		}
	}

	if len(added) > 0 {
		if err := s.news.AddTopics(ctx, added); err != nil {
			return err
		}
		if err := s.news.MarkSeen(ctx, added); err != nil {
			return err
		}
	}
	if len(removed) > 0 {
		if err := s.news.RemoveTopics(ctx, removed); err != nil {
			return err
		}
	}
	return nil
}

// News возвращает последние новости о новых темах, доступных студенту.
func (s *Service) News(ctx context.Context, userID int64) ([]domain.NewsEntry, error) {
	student, err := s.CurrentStudent(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Берём с запасом — после фильтра по потоку может остаться меньше 3.
	latest, err := s.news.ListLatest(ctx, 10)
	if err != nil {
		return nil, err
	}
	if len(latest) == 0 {
		return nil, nil
	}

	themes, err := s.homework.ListAllThemesByStudent(ctx, student.Code)
	if err != nil {
		return nil, err
	}
	themeByCode := make(map[string]domain.Theme, len(themes))
	for _, t := range themes {
		themeByCode[t.Code] = t
	}

	var result []domain.NewsEntry
	for _, n := range latest {
		t, ok := themeByCode[n.ThemeCode]
		if !ok {
			continue
		}
		result = append(result, domain.NewsEntry{
			ThemeCode:   t.Code,
			ThemeName:   t.Name,
			SubjectName: t.SubjectName,
			CreatedAt:   n.CreatedAt,
		})
		if len(result) == 3 {
			break
		}
	}
	return result, nil
}

// LoginRetryAfter возвращает, сколько осталось до конца окна лимитера,
// если пользователь сейчас заблокирован. Если не заблокирован — 0.
// Используется handler'ом, чтобы показать точное время в сообщении.
func (s *Service) LoginRetryAfter(userID int64) time.Duration {
	return s.loginLimiter.RetryAfter(userID)
}
