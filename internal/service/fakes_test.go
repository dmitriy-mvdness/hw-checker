package service

import (
	"context"
	"sync"
	"time"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// Фейки репозиториев и файлового хранилища для тестов сервиса.
//
// Каждый фейк построен по одной схеме: поля-fn для переопределения
// поведения (если nil — дефолт) и поля-журналы для проверки вызовов.
// Это позволяет тесту задать ровно то поведение, которое нужно,
// и не тащить за собой mock-фреймворк.
//
// Все фейки потокобезопасны (sync.Mutex внутри), хотя большинство
// тестов однопоточные — на случай, если позже понадобится параллельный
// тест.

// --- AccountRepository --------------------------------------------------

type fakeAccounts struct {
	mu sync.Mutex

	getFn    func(ctx context.Context, id int64) (domain.Account, error)
	createFn func(ctx context.Context, id int64, code string) error
	deleteFn func(ctx context.Context, id int64) error

	// Журналы вызовов.
	created    []createAccountCall
	deletedIDs []int64
}

type createAccountCall struct {
	ID   int64
	Code string
}

func (f *fakeAccounts) GetByTelegramID(ctx context.Context, id int64) (domain.Account, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return domain.Account{}, domain.ErrNotFound
}

func (f *fakeAccounts) Create(ctx context.Context, id int64, code string) error {
	f.mu.Lock()
	f.created = append(f.created, createAccountCall{ID: id, Code: code})
	f.mu.Unlock()

	if f.createFn != nil {
		return f.createFn(ctx, id, code)
	}
	return nil
}

func (f *fakeAccounts) DeleteByTelegramID(ctx context.Context, id int64) error {
	f.mu.Lock()
	f.deletedIDs = append(f.deletedIDs, id)
	f.mu.Unlock()

	if f.deleteFn != nil {
		return f.deleteFn(ctx, id)
	}
	return nil
}

// --- StudentRepository --------------------------------------------------

type fakeStudents struct {
	getFn func(ctx context.Context, code string) (domain.Student, error)
}

func (f *fakeStudents) GetByPersonalCode(ctx context.Context, code string) (domain.Student, error) {
	if f.getFn != nil {
		return f.getFn(ctx, code)
	}
	return domain.Student{}, domain.ErrNotFound
}

// --- HomeworkRepository -------------------------------------------------

type fakeHomework struct {
	listSubjectsFn          func(ctx context.Context, studentCode int64) ([]domain.Subject, error)
	listThemesFn            func(ctx context.Context, studentCode, subjectCode int64) ([]domain.Theme, error)
	getThemeFn              func(ctx context.Context, studentCode int64, themeCode string) (domain.Theme, error)
	listAllThemesFn         func(ctx context.Context, studentCode int64) ([]domain.Theme, error)
	listAllHomeworkThemesFn func(ctx context.Context) ([]string, error)
}

func (f *fakeHomework) ListSubjects(ctx context.Context, studentCode int64) ([]domain.Subject, error) {
	if f.listSubjectsFn != nil {
		return f.listSubjectsFn(ctx, studentCode)
	}
	return nil, nil
}

func (f *fakeHomework) ListThemes(ctx context.Context, studentCode, subjectCode int64) ([]domain.Theme, error) {
	if f.listThemesFn != nil {
		return f.listThemesFn(ctx, studentCode, subjectCode)
	}
	return nil, nil
}

func (f *fakeHomework) GetTheme(ctx context.Context, studentCode int64, themeCode string) (domain.Theme, error) {
	if f.getThemeFn != nil {
		return f.getThemeFn(ctx, studentCode, themeCode)
	}
	return domain.Theme{}, domain.ErrNotFound
}

func (f *fakeHomework) ListAllThemesByStudent(ctx context.Context, studentCode int64) ([]domain.Theme, error) {
	if f.listAllThemesFn != nil {
		return f.listAllThemesFn(ctx, studentCode)
	}
	return nil, nil
}

func (f *fakeHomework) ListAllHomeworkThemes(ctx context.Context) ([]string, error) {
	if f.listAllHomeworkThemesFn != nil {
		return f.listAllHomeworkThemesFn(ctx)
	}
	return nil, nil
}

// --- LegacySubmissionRepository ----------------------------------------

// fakeLegacy — фейк чтения historical-сдач из homework старого бота.
// По умолчанию возвращает пустую map: исторических сдач нет.
// Тесты, которым нужны сдачи, переопределяют submittedThemeCodesFn.
type fakeLegacy struct {
	mu sync.Mutex

	submittedThemeCodesFn func(ctx context.Context, personalCode string, themeCodes []string) (map[string]bool, error)

	// Журнал вызовов — чтобы тесты могли проверить, что legacy спросили
	// с правильным personal_code и правильными темами.
	calls []legacyCall
}

type legacyCall struct {
	PersonalCode string
	ThemeCodes   []string
}

func (f *fakeLegacy) SubmittedThemeCodes(ctx context.Context, personalCode string, themeCodes []string) (map[string]bool, error) {
	f.mu.Lock()
	// Копируем слайс: вызывающий может его мутировать после вызова.
	codes := make([]string, len(themeCodes))
	copy(codes, themeCodes)
	f.calls = append(f.calls, legacyCall{PersonalCode: personalCode, ThemeCodes: codes})
	f.mu.Unlock()

	if f.submittedThemeCodesFn != nil {
		return f.submittedThemeCodesFn(ctx, personalCode, themeCodes)
	}
	return map[string]bool{}, nil
}

// --- SubmissionRepository -----------------------------------------------

type fakeSubmissions struct {
	mu sync.Mutex

	countFn                func(ctx context.Context, studentCode int64, themeCode string) (int, error)
	createFn               func(ctx context.Context, sub domain.Submission) (int64, error)
	submittedThemeCodesFn  func(ctx context.Context, studentCode int64, themeCodes []string) (map[string]bool, error)
	attemptsByThemeCodesFn func(ctx context.Context, studentCode int64, themeCodes []string) (map[string]int, error)

	// Журнал созданных сдач.
	createdSubs []domain.Submission
}

func (f *fakeSubmissions) CountByTheme(ctx context.Context, studentCode int64, themeCode string) (int, error) {
	if f.countFn != nil {
		return f.countFn(ctx, studentCode, themeCode)
	}
	return 0, nil
}

func (f *fakeSubmissions) Create(ctx context.Context, sub domain.Submission) (int64, error) {
	f.mu.Lock()
	f.createdSubs = append(f.createdSubs, sub)
	f.mu.Unlock()

	if f.createFn != nil {
		return f.createFn(ctx, sub)
	}
	return 1, nil
}

func (f *fakeSubmissions) SubmittedThemeCodes(ctx context.Context, studentCode int64, themeCodes []string) (map[string]bool, error) {
	if f.submittedThemeCodesFn != nil {
		return f.submittedThemeCodesFn(ctx, studentCode, themeCodes)
	}
	return map[string]bool{}, nil
}

func (f *fakeSubmissions) AttemptsByThemeCodes(ctx context.Context, studentCode int64, themeCodes []string) (map[string]int, error) {
	if f.attemptsByThemeCodesFn != nil {
		return f.attemptsByThemeCodesFn(ctx, studentCode, themeCodes)
	}
	return map[string]int{}, nil
}

// --- NewsRepository -----------------------------------------------------

type fakeNews struct {
	mu sync.Mutex

	listSeenFn       func(ctx context.Context) (map[string]bool, error)
	markSeenFn       func(ctx context.Context, codes []string) error
	addTopicsFn      func(ctx context.Context, codes []string) error
	removeTopicsFn   func(ctx context.Context, codes []string) error
	listLatestFn     func(ctx context.Context, limit int) ([]domain.NewsItem, error)
	isInitializedFn  func(ctx context.Context) (bool, error)
	setInitializedFn func(ctx context.Context) error

	// Журналы.
	markedSeen    [][]string
	addedTopics   [][]string
	removedTopics [][]string
	initialized   bool
}

func (f *fakeNews) ListSeen(ctx context.Context) (map[string]bool, error) {
	if f.listSeenFn != nil {
		return f.listSeenFn(ctx)
	}
	return map[string]bool{}, nil
}

func (f *fakeNews) MarkSeen(ctx context.Context, codes []string) error {
	f.mu.Lock()
	f.markedSeen = append(f.markedSeen, codes)
	f.mu.Unlock()

	if f.markSeenFn != nil {
		return f.markSeenFn(ctx, codes)
	}
	return nil
}

func (f *fakeNews) AddTopics(ctx context.Context, codes []string) error {
	f.mu.Lock()
	f.addedTopics = append(f.addedTopics, codes)
	f.mu.Unlock()

	if f.addTopicsFn != nil {
		return f.addTopicsFn(ctx, codes)
	}
	return nil
}

func (f *fakeNews) RemoveTopics(ctx context.Context, codes []string) error {
	f.mu.Lock()
	f.removedTopics = append(f.removedTopics, codes)
	f.mu.Unlock()

	if f.removeTopicsFn != nil {
		return f.removeTopicsFn(ctx, codes)
	}
	return nil
}

func (f *fakeNews) ListLatest(ctx context.Context, limit int) ([]domain.NewsItem, error) {
	if f.listLatestFn != nil {
		return f.listLatestFn(ctx, limit)
	}
	return nil, nil
}

func (f *fakeNews) IsInitialized(ctx context.Context) (bool, error) {
	if f.isInitializedFn != nil {
		return f.isInitializedFn(ctx)
	}
	return f.initialized, nil
}

func (f *fakeNews) SetInitialized(ctx context.Context) error {
	f.mu.Lock()
	f.initialized = true
	f.mu.Unlock()

	if f.setInitializedFn != nil {
		return f.setInitializedFn(ctx)
	}
	return nil
}

// --- FileStorage --------------------------------------------------------

type fakeFiles struct {
	mu sync.Mutex

	saveHomeworkFn       func(student domain.Student, theme domain.Theme, attempt int, originalName string, data []byte) (string, error)
	removeFn             func(relPath string) error
	loginRequestExistsFn func(telegramID int64) (bool, error)
	saveLoginRequestFn   func(telegramID int64, name, username, link string) (string, error)
	removeLoginRequestFn func(telegramID int64) error
	saveSupportTextFn    func(req domain.SupportRequest) (string, error)
	saveSupportFileFn    func(req domain.SupportRequest, originalName string, data []byte) (string, error)

	// Журналы.
	removedPaths    []string
	removedLoginIDs []int64
}

func (f *fakeFiles) SaveHomework(student domain.Student, theme domain.Theme, attempt int, originalName string, data []byte) (string, error) {
	if f.saveHomeworkFn != nil {
		return f.saveHomeworkFn(student, theme, attempt, originalName, data)
	}
	return "homework/test/file.pdf", nil
}

func (f *fakeFiles) Remove(relPath string) error {
	f.mu.Lock()
	f.removedPaths = append(f.removedPaths, relPath)
	f.mu.Unlock()

	if f.removeFn != nil {
		return f.removeFn(relPath)
	}
	return nil
}

func (f *fakeFiles) LoginRequestExists(telegramID int64) (bool, error) {
	if f.loginRequestExistsFn != nil {
		return f.loginRequestExistsFn(telegramID)
	}
	return false, nil
}

func (f *fakeFiles) SaveLoginRequest(telegramID int64, name, username, link string) (string, error) {
	if f.saveLoginRequestFn != nil {
		return f.saveLoginRequestFn(telegramID, name, username, link)
	}
	return "support/login/2024-01-01/tg1.txt", nil
}

func (f *fakeFiles) RemoveLoginRequest(telegramID int64) error {
	f.mu.Lock()
	f.removedLoginIDs = append(f.removedLoginIDs, telegramID)
	f.mu.Unlock()

	if f.removeLoginRequestFn != nil {
		return f.removeLoginRequestFn(telegramID)
	}
	return nil
}

func (f *fakeFiles) SaveSupportText(req domain.SupportRequest) (string, error) {
	if f.saveSupportTextFn != nil {
		return f.saveSupportTextFn(req)
	}
	return "support/bug/info.txt", nil
}

func (f *fakeFiles) SaveSupportFile(req domain.SupportRequest, originalName string, data []byte) (string, error) {
	if f.saveSupportFileFn != nil {
		return f.saveSupportFileFn(req, originalName, data)
	}
	return "support/bug/info.txt", nil
}

// --- LoginLimiter -------------------------------------------------------

type fakeLoginLimiter struct {
	mu sync.Mutex

	allowFn      func(userID int64) bool
	retryAfterFn func(userID int64) time.Duration

	// Журналы.
	allowed  []int64
	resetIDs []int64
}

func (f *fakeLoginLimiter) Allow(userID int64) bool {
	f.mu.Lock()
	f.allowed = append(f.allowed, userID)
	f.mu.Unlock()

	if f.allowFn != nil {
		return f.allowFn(userID)
	}
	return true
}

func (f *fakeLoginLimiter) RetryAfter(userID int64) time.Duration {
	if f.retryAfterFn != nil {
		return f.retryAfterFn(userID)
	}
	return 0
}

func (f *fakeLoginLimiter) Reset(userID int64) {
	f.mu.Lock()
	f.resetIDs = append(f.resetIDs, userID)
	f.mu.Unlock()
}

// --- Хелпер сборки сервиса ---------------------------------------------

// fakeBundle держит все восемь фейков, чтобы тесты могли дотянуться
// до любого из них одной переменной.
type fakeBundle struct {
	accounts    *fakeAccounts
	students    *fakeStudents
	homework    *fakeHomework
	legacy      *fakeLegacy
	submissions *fakeSubmissions
	news        *fakeNews
	files       *fakeFiles
	limiter     *fakeLoginLimiter
}

// newTestService собирает Service со всеми фейками и zap.NewNop().
// Все фейки работают по дефолтам (пустые/ErrNotFound) — тест
// переопределяет только нужное через *.xxxFn = func(...).
func newTestService(cfg ServiceConfig) (*Service, *fakeBundle) {
	b := &fakeBundle{
		accounts:    &fakeAccounts{},
		students:    &fakeStudents{},
		homework:    &fakeHomework{},
		legacy:      &fakeLegacy{},
		submissions: &fakeSubmissions{},
		news:        &fakeNews{},
		files:       &fakeFiles{},
		limiter:     &fakeLoginLimiter{},
	}

	svc := New(
		b.accounts,
		b.students,
		b.homework,
		b.legacy,
		b.submissions,
		b.news,
		b.files,
		b.limiter,
		zapNop(),
		cfg,
	)

	return svc, b
}
