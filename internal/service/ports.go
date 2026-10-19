// ports.go — интерфейсы репозиториев и файлового хранилища, которые
// нужны сервису. Объявлены у потребителя: репозитории подходят под них
// автоматически, без слова "implements".
package service

import (
	"context"
	"time"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// AccountRepository — доступ к аккаунтам в storage.db.
type AccountRepository interface {
	GetByTelegramID(ctx context.Context, telegramID int64) (domain.Account, error)
	Create(ctx context.Context, telegramID int64, personalCode string) error
	DeleteByTelegramID(ctx context.Context, telegramID int64) error
}

// StudentRepository — доступ к студентам из snapshot.db.
type StudentRepository interface {
	GetByPersonalCode(ctx context.Context, personalCode string) (domain.Student, error)
}

// HomeworkRepository — предметы и темы из snapshot.db.
type HomeworkRepository interface {
	ListSubjects(ctx context.Context, studentCode int64) ([]domain.Subject, error)
	ListThemes(ctx context.Context, studentCode, subjectCode int64) ([]domain.Theme, error)
	GetTheme(ctx context.Context, studentCode int64, themeCode string) (domain.Theme, error)
	ListAllThemesByStudent(ctx context.Context, studentCode int64) ([]domain.Theme, error)
	ListAllHomeworkThemes(ctx context.Context) ([]string, error)
}

// LegacySubmissionRepository — исторические сдачи из старого бота.
// Живут в snapshot.db, поэтому открыты read-only; пишем туда не мы.
type LegacySubmissionRepository interface {
	SubmittedThemeCodes(ctx context.Context, personalCode string, themeCodes []string) (map[string]bool, error)
}

// SubmissionRepository — сдачи из storage.db.
type SubmissionRepository interface {
	CountByTheme(ctx context.Context, studentCode int64, themeCode string) (int, error)
	Create(ctx context.Context, sub domain.Submission) (int64, error)
	SubmittedThemeCodes(ctx context.Context, studentCode int64, themeCodes []string) (map[string]bool, error)
	AttemptsByThemeCodes(ctx context.Context, studentCode int64, themeCodes []string) (map[string]int, error)
}

// FileStorage — файловое хранилище сдач и обращений в поддержку.
type FileStorage interface {
	SaveHomework(student domain.Student, theme domain.Theme, attempt int, originalName string, data []byte) (string, error)
	Remove(relPath string) error

	LoginRequestExists(telegramID int64) (bool, error)
	SaveLoginRequest(telegramID int64, name, username, link string) (string, error)
	RemoveLoginRequest(telegramID int64) error

	SaveSupportText(req domain.SupportRequest) (string, error)
	SaveSupportFile(req domain.SupportRequest, originalName string, data []byte) (string, error)
}

// NewsRepository — новости о темах в storage.db.
type NewsRepository interface {
	ListSeen(ctx context.Context) (map[string]bool, error)
	MarkSeen(ctx context.Context, codes []string) error
	AddTopics(ctx context.Context, codes []string) error
	RemoveTopics(ctx context.Context, codes []string) error
	ListLatest(ctx context.Context, limit int) ([]domain.NewsItem, error)
	IsInitialized(ctx context.Context) (bool, error)
	SetInitialized(ctx context.Context) error
}

// LoginLimiter — ограничитель частоты попыток входа по пользователю.
// Реализация — internal/ratelimit
type LoginLimiter interface {
	Allow(userID int64) bool
	RetryAfter(userID int64) time.Duration
	Reset(userID int64)
}
