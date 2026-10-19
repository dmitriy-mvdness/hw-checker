package repository

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

func TestStudents_GetByPersonalCode_Success(t *testing.T) {
	db := newSnapshotDB(t)

	insertStream(t, db, 1, "Поток А")
	insertGroup(t, db, 10, "ПИ-21", 1)
	insertStudent(t, db, 42, "Иванов Иван", "CODE-1", sql.NullInt64{Int64: 10, Valid: true})

	r := NewStudents(db)
	student, err := r.GetByPersonalCode(newTestCtx(), "CODE-1")

	require.NoError(t, err)
	assert.Equal(t, int64(42), student.Code)
	assert.Equal(t, "Иванов Иван", student.Name)
	assert.Equal(t, "CODE-1", student.PersonalCode)
	assert.Equal(t, "ПИ-21", student.GroupName)
	assert.Equal(t, "Поток А", student.StreamName)
}

func TestStudents_GetByPersonalCode_NotFound(t *testing.T) {
	db := newSnapshotDB(t)
	r := NewStudents(db)

	_, err := r.GetByPersonalCode(newTestCtx(), "MISSING")

	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestStudents_GetByPersonalCode_NoGroup(t *testing.T) {
	// Студент без группы: LEFT JOIN должен вернуть пустые строки,
	// а не упасть. Это важно — в выгрузке 1С group_code может быть NULL.
	db := newSnapshotDB(t)
	insertStudent(t, db, 42, "Без Группы", "CODE-1", sql.NullInt64{})

	r := NewStudents(db)
	student, err := r.GetByPersonalCode(newTestCtx(), "CODE-1")

	require.NoError(t, err)
	assert.Equal(t, int64(42), student.Code)
	assert.Empty(t, student.GroupName)
	assert.Empty(t, student.StreamName)
}

func TestStudents_GetByPersonalCode_GroupWithoutStream(t *testing.T) {
	// Группа есть, но stream_code NULL. LEFT JOIN streams даст пустое имя.
	db := newSnapshotDB(t)

	_, err := db.Exec(
		`INSERT INTO "groups" (code, name, stream_code) VALUES (?, ?, NULL)`,
		10, "ПИ-21",
	)
	require.NoError(t, err)
	insertStudent(t, db, 42, "Иванов", "CODE-1", sql.NullInt64{Int64: 10, Valid: true})

	r := NewStudents(db)
	student, err := r.GetByPersonalCode(newTestCtx(), "CODE-1")

	require.NoError(t, err)
	assert.Equal(t, "ПИ-21", student.GroupName)
	assert.Empty(t, student.StreamName)
}
