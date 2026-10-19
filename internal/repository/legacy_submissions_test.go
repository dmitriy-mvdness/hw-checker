package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Тесты LegacySubmissions — чтение historical-сдач из homework.
// Главные риски: пустой вход (не должен ходить в БД), фильтр по чужому
// personal_code, DISTINCT по пересдачам одной темы.

func TestLegacySubmissions_SubmittedThemeCodes_EmptyList(t *testing.T) {
	// Пустой список тем — не повод идти в БД. Возвращаем пустую map.
	db := newSnapshotDB(t)
	r := NewLegacySubmissions(db)

	got, err := r.SubmittedThemeCodes(newTestCtx(), "CODE-1", nil)

	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = r.SubmittedThemeCodes(newTestCtx(), "CODE-1", []string{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestLegacySubmissions_SubmittedThemeCodes_EmptyPersonalCode(t *testing.T) {
	// Пустой personal_code — тоже не ходим в БД. Студент без кода
	// не может иметь сдач в homework.
	db := newSnapshotDB(t)
	r := NewLegacySubmissions(db)

	got, err := r.SubmittedThemeCodes(newTestCtx(), "", []string{"T-1"})

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestLegacySubmissions_SubmittedThemeCodes_Filter(t *testing.T) {
	// Три сдачи у CODE-1, одна — у чужого. Запрашиваем подмножество тем.
	db := newSnapshotDB(t)
	insertLegacySubmission(t, db, "CODE-1", 100, "T-1", "hw1.pdf")
	insertLegacySubmission(t, db, "CODE-1", 100, "T-2", "hw2.pdf")
	insertLegacySubmission(t, db, "CODE-1", 101, "T-3", "hw3.pdf")
	insertLegacySubmission(t, db, "CODE-2", 100, "T-1", "other.pdf")

	r := NewLegacySubmissions(db)
	got, err := r.SubmittedThemeCodes(newTestCtx(), "CODE-1", []string{"T-1", "T-3", "T-9"})
	require.NoError(t, err)

	assert.True(t, got["T-1"], "сдана CODE-1 — должна быть в map")
	assert.True(t, got["T-3"], "сдана CODE-1 — должна быть в map")
	assert.False(t, got["T-9"], "никем не сдана — не должна попасть")
	assert.False(t, got["T-2"], "сдана, но не запрошена — не должна попасть")

	// Чужой студент не просачивается: у CODE-2 своя T-1, но для CODE-1
	// мы её уже нашли через его собственную запись; проверим обратное —
	// запрос от CODE-2 не должен вернуть чужих тем.
	got2, err := r.SubmittedThemeCodes(newTestCtx(), "CODE-2", []string{"T-1", "T-3"})
	require.NoError(t, err)
	assert.True(t, got2["T-1"])
	assert.False(t, got2["T-3"], "T-3 сдана CODE-1, не CODE-2")
}

func TestLegacySubmissions_SubmittedThemeCodes_Distinct(t *testing.T) {
	// Одна тема сдана несколько раз (пересдачи). В map — один true,
	// DISTINCT схлопывает дубли на уровне SQL.
	db := newSnapshotDB(t)
	insertLegacySubmission(t, db, "CODE-1", 100, "T-1", "hw1.pdf")
	insertLegacySubmission(t, db, "CODE-1", 100, "T-1", "hw1_v2.pdf")
	insertLegacySubmission(t, db, "CODE-1", 100, "T-1", "hw1_v3.pdf")

	r := NewLegacySubmissions(db)
	got, err := r.SubmittedThemeCodes(newTestCtx(), "CODE-1", []string{"T-1"})
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.True(t, got["T-1"])
}

func TestLegacySubmissions_SubmittedThemeCodes_EmptyHomework(t *testing.T) {
	// Пустая homework — пустая map, не ошибка.
	db := newSnapshotDB(t)
	r := NewLegacySubmissions(db)

	got, err := r.SubmittedThemeCodes(newTestCtx(), "CODE-1", []string{"T-1"})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestLegacySubmissions_SubmittedThemeCodes_NullPersonalCode(t *testing.T) {
	// В homework personal_code может быть NULL (не должно, но схема
	// допускает). Такие строки не должны матчиться ни с каким кодом.
	db := newSnapshotDB(t)

	_, err := db.Exec(
		`INSERT INTO homework (personal_code, subject_code, theme_code, filename)
		 VALUES (NULL, 100, 'T-1', 'x.pdf')`)
	require.NoError(t, err)

	r := NewLegacySubmissions(db)
	got, err := r.SubmittedThemeCodes(newTestCtx(), "CODE-1", []string{"T-1"})
	require.NoError(t, err)
	assert.Empty(t, got)
}
