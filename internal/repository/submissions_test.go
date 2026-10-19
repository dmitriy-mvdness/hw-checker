package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

func TestSubmissions_CountByTheme_Zero(t *testing.T) {
	db := newStorageDB(t)
	r := NewSubmissions(db)

	n, err := r.CountByTheme(newTestCtx(), 42, "T-1")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestSubmissions_CountByTheme_N(t *testing.T) {
	db := newStorageDB(t)
	insertSubmission(t, db, 42, "T-1", 1, "path1", "a.pdf")
	insertSubmission(t, db, 42, "T-1", 2, "path2", "b.pdf")
	// Другая тема — не должна попасть в счёт.
	insertSubmission(t, db, 42, "T-2", 1, "path3", "c.pdf")
	// Другой студент — тоже мимо.
	insertSubmission(t, db, 43, "T-1", 1, "path4", "d.pdf")

	r := NewSubmissions(db)
	n, err := r.CountByTheme(newTestCtx(), 42, "T-1")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestSubmissions_Create_Success(t *testing.T) {
	db := newStorageDB(t)
	r := NewSubmissions(db)

	sub := domain.Submission{
		StudentCode:  42,
		ThemeCode:    "T-1",
		Attempt:      1,
		FilePath:     "homework/G/T-1/file.pdf",
		OriginalName: "file.pdf",
	}
	id, err := r.Create(newTestCtx(), sub)

	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestSubmissions_Create_UniqueViolation(t *testing.T) {
	// UNIQUE(student_code, theme_code, attempt): попытка дважды записать
	// одну и ту же попытку — ошибка. В сервисе это подстраховка: если
	// два SubmitHomework одновременно посчитают count и оба получат
	// attempt=2, второй Create упадёт, файл будет откачен.
	db := newStorageDB(t)
	insertSubmission(t, db, 42, "T-1", 1, "p1", "a.pdf")

	r := NewSubmissions(db)
	_, err := r.Create(newTestCtx(), domain.Submission{
		StudentCode: 42, ThemeCode: "T-1", Attempt: 1,
		FilePath: "p2", OriginalName: "b.pdf",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "insert submission")
}

func TestSubmissions_SubmittedThemeCodes_Empty(t *testing.T) {
	// Пустой список тем — не должен порождать SQL с пустым IN ().
	db := newStorageDB(t)
	r := NewSubmissions(db)

	got, err := r.SubmittedThemeCodes(newTestCtx(), 42, nil)

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSubmissions_SubmittedThemeCodes_Filter(t *testing.T) {
	db := newStorageDB(t)

	insertSubmission(t, db, 42, "T-1", 1, "p1", "a.pdf")
	insertSubmission(t, db, 42, "T-3", 1, "p3", "c.pdf")
	// Другой студент со своими темами — не должен влиять.
	insertSubmission(t, db, 43, "T-2", 1, "p2", "b.pdf")

	r := NewSubmissions(db)
	got, err := r.SubmittedThemeCodes(newTestCtx(), 42, []string{"T-1", "T-2", "T-3"})
	require.NoError(t, err)

	// Только T-1 и T-3 — их сдавал студент 42. T-2 не сдавал никто из 42.
	assert.True(t, got["T-1"])
	assert.False(t, got["T-2"])
	assert.True(t, got["T-3"])
}

func TestSubmissions_AttemptsByThemeCodes_Empty(t *testing.T) {
	db := newStorageDB(t)
	r := NewSubmissions(db)

	got, err := r.AttemptsByThemeCodes(newTestCtx(), 42, nil)

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSubmissions_AttemptsByThemeCodes_Grouping(t *testing.T) {
	db := newStorageDB(t)

	insertSubmission(t, db, 42, "T-1", 1, "p1", "a.pdf")
	insertSubmission(t, db, 42, "T-1", 2, "p2", "b.pdf")
	insertSubmission(t, db, 42, "T-3", 1, "p3", "c.pdf")

	r := NewSubmissions(db)
	got, err := r.AttemptsByThemeCodes(newTestCtx(), 42, []string{"T-1", "T-2", "T-3"})
	require.NoError(t, err)

	assert.Equal(t, 2, got["T-1"])
	assert.Equal(t, 0, got["T-2"]) // нет записи — 0
	assert.Equal(t, 1, got["T-3"])
}
