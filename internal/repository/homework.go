// homework.go — чтение предметов и тем из snapshot.db (read-only).
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// Homework читает предметы и темы из snapshot.db для экранов сдачи ДЗ.
type Homework struct {
	db *sql.DB
}

// NewHomework создаёт репозиторий домашних заданий.
func NewHomework(db *sql.DB) *Homework {
	return &Homework{db: db}
}

// ListSubjects возвращает предметы потока, в котором учится студент.
func (r *Homework) ListSubjects(ctx context.Context, studentCode int64) ([]domain.Subject, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT subj.code, subj.name
		 FROM students s
		 JOIN "groups" g        ON g.code = s.group_code
		 JOIN stream_subjects ss ON ss.stream_code = g.stream_code
		 JOIN subjects subj      ON subj.code = ss.subject_code
		 WHERE s.code = ?
		 ORDER BY subj.name`, studentCode,
	)
	if err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}
	defer rows.Close()

	var subjects []domain.Subject
	for rows.Next() {
		var s domain.Subject
		if err := rows.Scan(&s.Code, &s.Name); err != nil {
			return nil, fmt.Errorf("scan subject: %w", err)
		}
		subjects = append(subjects, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subjects: %w", err)
	}
	return subjects, nil
}

// ListThemes возвращает темы предмета, доступные студенту, с has_homework = 1.
// Проверка потока повторяется, чтобы бот не доверял коду предмета из callback.
func (r *Homework) ListThemes(ctx context.Context, studentCode, subjectCode int64) ([]domain.Theme, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.code, t.name, COALESCE(subj.name, ''), t.subject_code, COALESCE(t.has_homework, 0)
	 FROM themes t
	 LEFT JOIN subjects subj ON subj.code = t.subject_code
	 WHERE t.subject_code = ?
	   AND COALESCE(t.has_homework, 0) = 1
	   AND EXISTS (
	       SELECT 1
	       FROM students s
	       JOIN "groups" g         ON g.code = s.group_code
	       JOIN stream_subjects ss ON ss.stream_code = g.stream_code
	       WHERE s.code = ? AND ss.subject_code = t.subject_code
	   )
	 ORDER BY t.name`, subjectCode, studentCode,
	)
	if err != nil {
		return nil, fmt.Errorf("list themes: %w", err)
	}
	defer rows.Close()

	var themes []domain.Theme
	for rows.Next() {
		var t domain.Theme
		if err := rows.Scan(&t.Code, &t.Name, &t.SubjectName, &t.SubjectCode, &t.HasHomework); err != nil {
			return nil, fmt.Errorf("scan theme: %w", err)
		}
		themes = append(themes, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate themes: %w", err)
	}
	return themes, nil
}

// GetTheme возвращает одну тему, если она доступна студенту и имеет ДЗ.
// Используется при нажатии на кнопку темы.
//
// Возвращает:
//
//   - domain.ErrNotFound — тема не найдена или недоступна студенту.
func (r *Homework) GetTheme(ctx context.Context, studentCode int64, themeCode string) (domain.Theme, error) {
	var t domain.Theme
	err := r.db.QueryRowContext(ctx,
		`SELECT t.code, t.name, COALESCE(subj.name, ''), t.subject_code, COALESCE(t.has_homework, 0)
	 FROM themes t
	 LEFT JOIN subjects subj ON subj.code = t.subject_code
	 WHERE t.code = ?
	   AND COALESCE(t.has_homework, 0) = 1
	   AND EXISTS (
	       SELECT 1
	       FROM students s
	       JOIN "groups" g         ON g.code = s.group_code
	       JOIN stream_subjects ss ON ss.stream_code = g.stream_code
	       WHERE s.code = ? AND ss.subject_code = t.subject_code
	   )`, themeCode, studentCode,
	).Scan(&t.Code, &t.Name, &t.SubjectName, &t.SubjectCode, &t.HasHomework)

	if errors.Is(err, sql.ErrNoRows) {
		return domain.Theme{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Theme{}, fmt.Errorf("get theme %s: %w", themeCode, err)
	}
	return t, nil
}

// ListAllThemesByStudent возвращает все темы с ДЗ по всем предметам
// студента, отсортированные по (subject.name, theme.name). Используется
// экраном «Мои сдачи»: не нужен отдельный запрос на каждый предмет.
func (r *Homework) ListAllThemesByStudent(ctx context.Context, studentCode int64) ([]domain.Theme, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.code, t.name, COALESCE(subj.name, ''), t.subject_code, COALESCE(t.has_homework, 0)
		 FROM themes t
		 LEFT JOIN subjects subj ON subj.code = t.subject_code
		 WHERE COALESCE(t.has_homework, 0) = 1
		   AND t.subject_code IN (
		       SELECT ss.subject_code
		       FROM students s
		       JOIN "groups" g         ON g.code = s.group_code
		       JOIN stream_subjects ss ON ss.stream_code = g.stream_code
		       WHERE s.code = ?
		   )
		 ORDER BY subj.name, t.name`, studentCode,
	)
	if err != nil {
		return nil, fmt.Errorf("list all themes by student: %w", err)
	}
	defer rows.Close()

	var themes []domain.Theme
	for rows.Next() {
		var t domain.Theme
		if err := rows.Scan(&t.Code, &t.Name, &t.SubjectName, &t.SubjectCode, &t.HasHomework); err != nil {
			return nil, fmt.Errorf("scan theme: %w", err)
		}
		themes = append(themes, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate themes: %w", err)
	}
	return themes, nil
}

// ListAllHomeworkThemes возвращает все темы с has_homework = 1
// (без фильтра по студенту). Используется при синхронизации новостей.
func (r *Homework) ListAllHomeworkThemes(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT code FROM themes WHERE COALESCE(has_homework, 0) = 1`)
	if err != nil {
		return nil, fmt.Errorf("list homework themes: %w", err)
	}
	defer rows.Close()

	var codes []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan theme code: %w", err)
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}
