// submissions.go — сдачи: таблица submissions в storage.db (чтение и запись).
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// Submissions работает с таблицей submissions в storage.db.
type Submissions struct {
	db *sql.DB
}

// NewSubmissions создаёт репозиторий сдач.
func NewSubmissions(db *sql.DB) *Submissions {
	return &Submissions{db: db}
}

// CountByTheme возвращает, сколько попыток уже сделано студентом по теме.
func (r *Submissions) CountByTheme(ctx context.Context, studentCode int64, themeCode string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM submissions
		 WHERE student_code = ? AND theme_code = ?`,
		studentCode, themeCode,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count submissions: %w", err)
	}
	return n, nil
}

// Create вставляет запись о сдаче и возвращает её id.
func (r *Submissions) Create(ctx context.Context, sub domain.Submission) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO submissions (student_code, theme_code, attempt, file_path, original_name)
		 VALUES (?, ?, ?, ?, ?)`,
		sub.StudentCode, sub.ThemeCode, sub.Attempt, sub.FilePath, sub.OriginalName,
	)
	if err != nil {
		return 0, fmt.Errorf("insert submission: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

// AttemptsByThemeCodes возвращает map theme_code → количество попыток
// у студента по каждой теме. Одним запросом, чтобы не дёргать базу
// в цикле.
func (r *Submissions) AttemptsByThemeCodes(ctx context.Context, studentCode int64, themeCodes []string) (map[string]int, error) {
	if len(themeCodes) == 0 {
		return map[string]int{}, nil
	}

	placeholders := strings.Repeat("?,", len(themeCodes))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, 0, len(themeCodes)+1)
	args = append(args, studentCode)
	for _, c := range themeCodes {
		args = append(args, c)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT theme_code, COUNT(*) FROM submissions
		 WHERE student_code = ? AND theme_code IN (`+placeholders+`)
		 GROUP BY theme_code`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("attempts by theme codes: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int, len(themeCodes))
	for rows.Next() {
		var code string
		var n int
		if err := rows.Scan(&code, &n); err != nil {
			return nil, fmt.Errorf("scan attempts: %w", err)
		}
		out[code] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attempts: %w", err)
	}
	return out, nil
}

// SubmittedThemeCodes возвращает коды тем, по которым у студента есть
// хотя бы одна сдача. Одним запросом, чтобы не дёргать базу в цикле.
func (r *Submissions) SubmittedThemeCodes(ctx context.Context, studentCode int64, themeCodes []string) (map[string]bool, error) {
	if len(themeCodes) == 0 {
		return map[string]bool{}, nil
	}

	// Плейсхолдеры "?, ?, ..." подставляются вручную: database/sql
	// не умеет разворачивать слайс в IN.
	placeholders := strings.Repeat("?,", len(themeCodes))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, 0, len(themeCodes)+1)
	args = append(args, studentCode)
	for _, c := range themeCodes {
		args = append(args, c)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT theme_code FROM submissions
		 WHERE student_code = ? AND theme_code IN (`+placeholders+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("submitted theme codes: %w", err)
	}
	defer rows.Close()

	out := make(map[string]bool, len(themeCodes))
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan theme code: %w", err)
		}
		out[code] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate theme codes: %w", err)
	}
	return out, nil
}
