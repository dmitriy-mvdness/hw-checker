package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// Accounts — самый хрупкий репозиторий: различение ErrCodeTaken vs
// ErrAccountExists делается через strings.Contains по тексту ошибки
// SQLite. Если modernc/sqlite поменяет формат сообщения, тесты упадут
// и мы узнаем об этом здесь, а не в проде.

func TestAccounts_GetByTelegramID_Success(t *testing.T) {
	db := newStorageDB(t)
	insertAccount(t, db, 100, "CODE-1")

	r := NewAccounts(db)
	acc, err := r.GetByTelegramID(newTestCtx(), 100)

	require.NoError(t, err)
	assert.Equal(t, int64(100), acc.TelegramID)
	assert.Equal(t, "CODE-1", acc.PersonalCode)
	// created_at заполнен DEFAULT'ом.
	assert.False(t, acc.CreatedAt.IsZero())
}

func TestAccounts_GetByTelegramID_NotFound(t *testing.T) {
	db := newStorageDB(t)
	r := NewAccounts(db)

	_, err := r.GetByTelegramID(newTestCtx(), 999)

	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestAccounts_Create_Success(t *testing.T) {
	db := newStorageDB(t)
	r := NewAccounts(db)

	err := r.Create(newTestCtx(), 100, "CODE-1")
	require.NoError(t, err)

	// Проверяем, что запись реально есть.
	acc, err := r.GetByTelegramID(newTestCtx(), 100)
	require.NoError(t, err)
	assert.Equal(t, "CODE-1", acc.PersonalCode)
}

func TestAccounts_Create_ErrCodeTaken(t *testing.T) {
	// Другой юзер уже привязал этот персональный код. При создании
	// нового аккаунта с тем же кодом должна вернуться ErrCodeTaken.
	db := newStorageDB(t)
	insertAccount(t, db, 100, "CODE-1")

	r := NewAccounts(db)
	err := r.Create(newTestCtx(), 200, "CODE-1")

	assert.ErrorIs(t, err, domain.ErrCodeTaken)
}

func TestAccounts_Create_ErrAccountExists(t *testing.T) {
	// У того же юзера уже есть аккаунт — какой угодно personal_code,
	// но user_id UNIQUE. Возвращается ErrAccountExists.
	db := newStorageDB(t)
	insertAccount(t, db, 100, "CODE-1")

	r := NewAccounts(db)
	err := r.Create(newTestCtx(), 100, "CODE-2")

	assert.ErrorIs(t, err, domain.ErrAccountExists)
}

func TestAccounts_DeleteByTelegramID_Success(t *testing.T) {
	db := newStorageDB(t)
	insertAccount(t, db, 100, "CODE-1")

	r := NewAccounts(db)
	require.NoError(t, r.DeleteByTelegramID(newTestCtx(), 100))

	// Аккаунт реально удалён.
	_, err := r.GetByTelegramID(newTestCtx(), 100)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestAccounts_DeleteByTelegramID_Idempotent(t *testing.T) {
	// Удаление несуществующего — не ошибка. Важно: DeleteByTelegramID
	// вызывается из CurrentStudent при сбросе стейл-аккаунта, и повторный
	// вызов (например, при гонке двух запросов) не должен падать.
	db := newStorageDB(t)
	r := NewAccounts(db)

	assert.NoError(t, r.DeleteByTelegramID(newTestCtx(), 999))
}
