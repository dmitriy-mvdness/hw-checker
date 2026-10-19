// Package domain содержит сущности и ошибки, общие для всех слоёв.
// Он ничего не знает ни про SQL, ни про Telegram.
package domain

import (
	"errors"
	"time"
)

// Ошибки домена, общие для всех слоёв.
var (
	ErrNotFound             = errors.New("не найдено")
	ErrCodeTaken            = errors.New("код уже привязан к другому аккаунту")
	ErrAccountExists        = errors.New("аккаунт уже существует")
	ErrBadFileType          = errors.New("недопустимый тип файла")
	ErrFileTooLarge         = errors.New("файл слишком большой")
	ErrTooManyAttempts      = errors.New("достигнут лимит попыток")
	ErrSupportRequestExists = errors.New("запрос уже отправлен")
)

// Причины обращения в поддержку.
const (
	SupportReasonLogin    = "login"
	SupportReasonBug      = "bug"
	SupportReasonIdeas    = "ideas"
	SupportReasonHomework = "homework"
	SupportReasonOther    = "other"
)

// Account — привязка Telegram-аккаунта к студенту.
// Читается из storage.db, таблица accounts.
type Account struct {
	TelegramID   int64
	PersonalCode string
	CreatedAt    time.Time
}

// Student — данные студента из выгрузки 1С.
// Читается из snapshot.db, таблица students.
type Student struct {
	Code         int64
	Name         string
	PersonalCode string
	GroupName    string
	StreamName   string
}

// Subject — предмет из выгрузки 1С.
type Subject struct {
	Code int64
	Name string
}

// Theme — тема по предмету. Бот показывает только темы с has_homework = 1.
type Theme struct {
	Code        string
	Name        string
	SubjectName string
	SubjectCode int64
	HasHomework bool
	Submitted   bool
}

// Submission — запись о сдаче в storage.db, таблица submissions.
type Submission struct {
	ID           int64
	StudentCode  int64
	ThemeCode    string
	Attempt      int
	FilePath     string // относительно HOMEWORK_DIR
	OriginalName string
	CreatedAt    time.Time
}

// SubmissionSummary — тема студента и количество попыток по ней.
// Собирается в сервисе из трёх источников: список тем (snapshot.db),
// счётчики из submissions (storage.db) и факт исторической сдачи
// из homework (snapshot.db, старый бот).
type SubmissionSummary struct {
	SubjectCode int64
	SubjectName string
	ThemeCode   string
	ThemeName   string

	// Attempts — попытки в НОВОМ боте. Лимит MAX_SUBMISSIONS_PER_THEME
	// применяется именно к ним. Legacy-сдачи сюда не входят.
	Attempts int

	// Submitted — true, если тема сдана в новом боте ИЛИ в старом.
	// Используется для иконки 🟢 и прогресса на экране «Мои сдачи».
	Submitted bool
}

// SupportReasonTitle возвращает человекочитаемое название причины.
func SupportReasonTitle(code string) string {
	switch code {
	case SupportReasonLogin:
		return "Запрос кода"
	case SupportReasonBug:
		return "Баг в боте"
	case SupportReasonIdeas:
		return "Идея для бота"
	case SupportReasonHomework:
		return "Проблема с ДЗ"
	case SupportReasonOther:
		return "Другое"
	default:
		return code
	}
}

// SupportRequest — данные для файла обращения.
type SupportRequest struct {
	Reason       string
	Text         string
	StudentName  string
	GroupName    string
	StreamName   string
	TelegramID   int64
	TelegramName string
	Username     string
	Link         string
}

// NewsItem — запись о новой теме из storage.db, таблица news.
type NewsItem struct {
	ThemeCode string
	CreatedAt time.Time
}

// NewsEntry — готовая новость для отображения студенту.
type NewsEntry struct {
	ThemeCode   string
	ThemeName   string
	SubjectName string
	CreatedAt   time.Time
}

// IsSupportReason сообщает, является ли код допустимой причиной
// обращения в техподдержку.
func IsSupportReason(code string) bool {
	switch code {
	case SupportReasonBug, SupportReasonIdeas, SupportReasonHomework, SupportReasonOther:
		return true
	default:
		return false
	}
}
