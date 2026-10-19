// legacy_submissions.go — исторические сдачи из таблицы homework snapshot.db
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// LegacySubmissions читает homework из snapshot.db — журнал сдач
// старого бота. Нужен только на чтение: интерфейс не даёт менять.
type LegacySubmissions struct {
	db *sql.DB
}

// NewLegacySubmissions создаёт репозиторий исторических сдач.
func NewLegacySubmissions(db *sql.DB) *LegacySubmissions {
	return &LegacySubmissions{db: db}
}

// SubmittedThemeCodes возвращает map[theme_code]bool по переданному
// списку тем: какие из них студент уже сдал в старом боте. Пустой
// список → пустая map без запроса. Дубли в homework (пересдачи)
// схлопываются через DISTINCT.
func (r *LegacySubmissions) SubmittedThemeCodes(
	ctx context.Context,
	personalCode string,
	themeCodes []string,
) (map[string]bool, error) {
	if len(themeCodes) == 0 || personalCode == "" {
		return map[string]bool{}, nil
	}

	// Плейсхолдеры "?, ?, ..." — как в Submissions.SubmittedThemeCodes:
	// database/sql не умеет разворачивать слайс в IN.
	placeholders := strings.Repeat("?,", len(themeCodes))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, 0, len(themeCodes)+1)
	args = append(args, personalCode)
	for _, c := range themeCodes {
		args = append(args, c)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT theme_code FROM homework
		 WHERE personal_code = ? AND theme_code IN (`+placeholders+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("legacy submitted theme codes: %w", err)
	}
	defer rows.Close()

	out := make(map[string]bool, len(themeCodes))
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan legacy theme code: %w", err)
		}
		out[code] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate legacy theme codes: %w", err)
	}
	return out, nil
}
