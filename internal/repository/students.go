// Package repository реализует доступ к данным: чтение из snapshot.db
// (выгрузка из 1С, read-only) и чтение-запись в storage.db (данные бота).
// Репозитории не содержат бизнес-логики — только запросы.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// Students читает студентов из snapshot.db. Методов записи здесь нет
// намеренно: база открыта с mode=ro, а интерфейс не даёт её менять.
type Students struct {
	db *sql.DB
}

// NewStudents создаёт репозиторий студентов.
func NewStudents(db *sql.DB) *Students {
	return &Students{db: db}
}

// GetByPersonalCode возвращает студента по персональному коду.
//
// Возвращает:
//
//   - domain.ErrNotFound — студент с таким кодом не найден.
func (r *Students) GetByPersonalCode(ctx context.Context, personalCode string) (domain.Student, error) {
	var s domain.Student

	// LEFT JOIN: студент найдётся, даже если группа или поток в выгрузке пустые.
	// "groups" в кавычках: в новых версиях SQLite это ключевое слово.
	err := r.db.QueryRowContext(ctx,
		`SELECT s.code, s.name, s.personal_code,
		        COALESCE(g.name, ''), COALESCE(st.name, '')
		 FROM students s
		 LEFT JOIN "groups" g ON g.code = s.group_code
		 LEFT JOIN streams st ON st.code = g.stream_code
		 WHERE s.personal_code = ?`, personalCode,
	).Scan(&s.Code, &s.Name, &s.PersonalCode, &s.GroupName, &s.StreamName)

	if errors.Is(err, sql.ErrNoRows) {
		return domain.Student{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Student{}, fmt.Errorf("get student by code: %w", err)
	}

	return s, nil
}
