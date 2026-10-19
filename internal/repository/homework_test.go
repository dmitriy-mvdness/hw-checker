package repository

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// seedHomework — общий сетап для тестов Homework.
//
// Поток 1: студент 42, группа 10, предметы 100 и 101.
// Поток 2: студент 43, группа 20, предмет 200 (чужой, не должен
//
//	попадать в выборки студента 42).
func seedHomework(t *testing.T, db *sql.DB) {
	t.Helper()

	insertStream(t, db, 1, "Поток А")
	insertStream(t, db, 2, "Поток Б")

	insertGroup(t, db, 10, "ПИ-21", 1)
	insertGroup(t, db, 20, "ПИ-22", 2)

	insertStudent(t, db, 42, "Иванов", "CODE-1", sql.NullInt64{Int64: 10, Valid: true})
	insertStudent(t, db, 43, "Петров", "CODE-2", sql.NullInt64{Int64: 20, Valid: true})

	insertSubject(t, db, 100, "Математика")
	insertSubject(t, db, 101, "Физика")
	insertSubject(t, db, 200, "Чужой предмет")

	insertStreamSubject(t, db, 1, 100)
	insertStreamSubject(t, db, 1, 101)
	insertStreamSubject(t, db, 2, 200)

	// Темы потока 1 (предмет 100): одна с ДЗ, одна без.
	insertTheme(t, db, "T-100-1", "Тема 1", 100, true)
	insertTheme(t, db, "T-100-2", "Тема 2", 100, false)
	// Тема потока 1 (предмет 101).
	insertTheme(t, db, "T-101-1", "Тема 3", 101, true)
	// Тема потока 2 (чужой предмет).
	insertTheme(t, db, "T-200-1", "Чужая тема", 200, true)
}

func TestHomework_ListSubjects(t *testing.T) {
	db := newSnapshotDB(t)
	seedHomework(t, db)

	r := NewHomework(db)
	subjects, err := r.ListSubjects(newTestCtx(), 42)

	require.NoError(t, err)
	// Только два предмета потока 1, отсортированные по имени.
	require.Len(t, subjects, 2)
	assert.Equal(t, int64(100), subjects[0].Code) // Математика
	assert.Equal(t, int64(101), subjects[1].Code) // Физика
}

func TestHomework_ListSubjects_OtherStream(t *testing.T) {
	db := newSnapshotDB(t)
	seedHomework(t, db)

	r := NewHomework(db)
	subjects, err := r.ListSubjects(newTestCtx(), 43)

	require.NoError(t, err)
	require.Len(t, subjects, 1)
	assert.Equal(t, int64(200), subjects[0].Code) // Чужой предмет (для 42-го)
}

func TestHomework_ListSubjects_UnknownStudent(t *testing.T) {
	db := newSnapshotDB(t)
	seedHomework(t, db)

	r := NewHomework(db)
	subjects, err := r.ListSubjects(newTestCtx(), 999)

	require.NoError(t, err)
	assert.Empty(t, subjects)
}

func TestHomework_ListThemes(t *testing.T) {
	db := newSnapshotDB(t)
	seedHomework(t, db)

	r := NewHomework(db)
	themes, err := r.ListThemes(newTestCtx(), 42, 100)

	require.NoError(t, err)
	// Только тема с has_homework = 1. T-100-2 отфильтрована.
	require.Len(t, themes, 1)
	assert.Equal(t, "T-100-1", themes[0].Code)
	assert.Equal(t, int64(100), themes[0].SubjectCode)
	assert.True(t, themes[0].HasHomework)
}

func TestHomework_ListThemes_ForeignSubject(t *testing.T) {
	// Студент 42 не учится на потоке, где есть предмет 200.
	// Даже если передать subjectCode=200 в callback — темы не отдаём.
	db := newSnapshotDB(t)
	seedHomework(t, db)

	r := NewHomework(db)
	themes, err := r.ListThemes(newTestCtx(), 42, 200)

	require.NoError(t, err)
	assert.Empty(t, themes)
}

func TestHomework_GetTheme_Success(t *testing.T) {
	db := newSnapshotDB(t)
	seedHomework(t, db)

	r := NewHomework(db)
	theme, err := r.GetTheme(newTestCtx(), 42, "T-100-1")

	require.NoError(t, err)
	assert.Equal(t, "T-100-1", theme.Code)
	assert.Equal(t, int64(100), theme.SubjectCode)
	assert.True(t, theme.HasHomework)
}

func TestHomework_GetTheme_NoHomework(t *testing.T) {
	// Тема есть, но has_homework = 0. Не должна открываться.
	db := newSnapshotDB(t)
	seedHomework(t, db)

	r := NewHomework(db)
	_, err := r.GetTheme(newTestCtx(), 42, "T-100-2")

	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestHomework_GetTheme_ForeignStudent(t *testing.T) {
	// Студент 42 пытается открыть тему потока 2 — недоступно.
	db := newSnapshotDB(t)
	seedHomework(t, db)

	r := NewHomework(db)
	_, err := r.GetTheme(newTestCtx(), 42, "T-200-1")

	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestHomework_ListAllThemesByStudent(t *testing.T) {
	db := newSnapshotDB(t)
	seedHomework(t, db)

	r := NewHomework(db)
	themes, err := r.ListAllThemesByStudent(newTestCtx(), 42)

	require.NoError(t, err)
	// Студент 42 учится на потоке 1, значит видит темы предметов 100 и 101
	// (только has_homework = 1): T-100-1 и T-101-1. Сортировка
	// по (subject.name, theme.name).
	require.Len(t, themes, 2)
	assert.Equal(t, "T-100-1", themes[0].Code)
	assert.Equal(t, "T-101-1", themes[1].Code)
}

func TestHomework_ListAllHomeworkThemes(t *testing.T) {
	db := newSnapshotDB(t)
	seedHomework(t, db)

	r := NewHomework(db)
	codes, err := r.ListAllHomeworkThemes(newTestCtx())

	require.NoError(t, err)
	// Все темы с has_homework = 1 независимо от потока.
	assert.ElementsMatch(t, []string{"T-100-1", "T-101-1", "T-200-1"}, codes)
}
