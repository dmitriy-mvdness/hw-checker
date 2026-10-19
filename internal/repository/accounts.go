// accounts.go — доступ к таблице accounts в storage.db (чтение и запись).
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// Accounts работает с таблицей accounts в storage.db.
type Accounts struct {
	db *sql.DB
}

// NewAccounts создаёт репозиторий аккаунтов.
func NewAccounts(db *sql.DB) *Accounts {
	return &Accounts{db: db}
}

// GetByTelegramID возвращает аккаунт по Telegram ID.
//
// Возвращает:
//
//   - domain.ErrNotFound — аккаунт не найден.
func (r *Accounts) GetByTelegramID(ctx context.Context, telegramID int64) (domain.Account, error) {
	var a domain.Account

	err := r.db.QueryRowContext(ctx,
		`SELECT user_id, personal_code, created_at
		 FROM accounts
		 WHERE user_id = ?`, telegramID,
	).Scan(&a.TelegramID, &a.PersonalCode, &a.CreatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("get account %d: %w", telegramID, err)
	}

	return a, nil
}

// Create создаёт аккаунт, различая нарушение UNIQUE по имени колонки.
//
// Возвращает:
//
//   - domain.ErrCodeTaken — код уже привязан к другому аккаунту;
//   - domain.ErrAccountExists — у пользователя уже есть аккаунт.
func (r *Accounts) Create(ctx context.Context, telegramID int64, personalCode string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO accounts (user_id, personal_code) VALUES (?, ?)`,
		telegramID, personalCode,
	)
	if err == nil {
		return nil
	}

	var se *sqlite.Error
	if errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		msg := se.Error()
		switch {
		case strings.Contains(msg, "accounts.personal_code"):
			return domain.ErrCodeTaken
		case strings.Contains(msg, "accounts.user_id"):
			return domain.ErrAccountExists
		}
	}

	return fmt.Errorf("create account %d: %w", telegramID, err)
}

// DeleteByTelegramID удаляет аккаунт по Telegram ID.
// Идемпотентна: если аккаунта нет, ошибки не будет.
func (r *Accounts) DeleteByTelegramID(ctx context.Context, telegramID int64) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM accounts WHERE user_id = ?`, telegramID,
	)
	if err != nil {
		return fmt.Errorf("delete account %d: %w", telegramID, err)
	}
	return nil
}
