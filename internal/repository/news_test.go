package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNews_MarkSeen_And_ListSeen(t *testing.T) {
	db := newStorageDB(t)
	r := NewNews(db)

	// Пусто.
	seen, err := r.ListSeen(newTestCtx())
	require.NoError(t, err)
	assert.Empty(t, seen)

	// Отмечаем и проверяем.
	require.NoError(t, r.MarkSeen(newTestCtx(), []string{"T-1", "T-2"}))
	seen, err = r.ListSeen(newTestCtx())
	require.NoError(t, err)
	assert.True(t, seen["T-1"])
	assert.True(t, seen["T-2"])
	assert.False(t, seen["T-3"])

	// Повторный MarkSeen тех же кодов — идемпотентно (INSERT OR IGNORE).
	require.NoError(t, r.MarkSeen(newTestCtx(), []string{"T-1"}))
	seen, err = r.ListSeen(newTestCtx())
	require.NoError(t, err)
	assert.Len(t, seen, 2)
}

func TestNews_MarkSeen_Empty(t *testing.T) {
	// Пустой список — no-op, не ошибка.
	db := newStorageDB(t)
	r := NewNews(db)

	assert.NoError(t, r.MarkSeen(newTestCtx(), nil))
	assert.NoError(t, r.MarkSeen(newTestCtx(), []string{}))
}

func TestNews_AddTopics(t *testing.T) {
	db := newStorageDB(t)
	r := NewNews(db)

	require.NoError(t, r.AddTopics(newTestCtx(), []string{"T-1", "T-2"}))

	// INSERT OR IGNORE — повтор не падает.
	require.NoError(t, r.AddTopics(newTestCtx(), []string{"T-2", "T-3"}))

	items, err := r.ListLatest(newTestCtx(), 10)
	require.NoError(t, err)
	assert.Len(t, items, 3)
}

func TestNews_RemoveTopics(t *testing.T) {
	db := newStorageDB(t)
	r := NewNews(db)

	require.NoError(t, r.MarkSeen(newTestCtx(), []string{"T-1", "T-2", "T-3"}))
	require.NoError(t, r.AddTopics(newTestCtx(), []string{"T-1", "T-2", "T-3"}))

	require.NoError(t, r.RemoveTopics(newTestCtx(), []string{"T-1", "T-3"}))

	// Из news удалены T-1 и T-3.
	items, err := r.ListLatest(newTestCtx(), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "T-2", items[0].ThemeCode)

	// Из seen_themes тоже.
	seen, err := r.ListSeen(newTestCtx())
	require.NoError(t, err)
	assert.False(t, seen["T-1"])
	assert.True(t, seen["T-2"])
	assert.False(t, seen["T-3"])
}

func TestNews_ListLatest_OrderAndLimit(t *testing.T) {
	// Сортировка по created_at DESC. Чтобы не зависеть от точности
	// таймстампа SQLite, вставляем записи с явными created_at.
	db := newStorageDB(t)
	r := NewNews(db)

	// Вставляем через прямой SQL, чтобы задать время.
	_, err := db.Exec(`INSERT INTO news (theme_code, created_at) VALUES
		('T-old',    '2024-01-01 10:00:00'),
		('T-mid',    '2024-01-02 10:00:00'),
		('T-newest', '2024-01-03 10:00:00')`)
	require.NoError(t, err)

	items, err := r.ListLatest(newTestCtx(), 2)
	require.NoError(t, err)
	require.Len(t, items, 2)

	// Новые сверху.
	assert.Equal(t, "T-newest", items[0].ThemeCode)
	assert.Equal(t, "T-mid", items[1].ThemeCode)

	// Проверим, что время реально приехало.
	assert.True(t, items[0].CreatedAt.After(items[1].CreatedAt))
	_ = time.Now // защита от unused-import, если уберём Assert с временем
}

func TestNews_IsInitialized_False(t *testing.T) {
	db := newStorageDB(t)
	r := NewNews(db)

	got, err := r.IsInitialized(newTestCtx())
	require.NoError(t, err)
	assert.False(t, got)
}

func TestNews_SetInitialized_And_IsInitialized(t *testing.T) {
	db := newStorageDB(t)
	r := NewNews(db)

	require.NoError(t, r.SetInitialized(newTestCtx()))

	got, err := r.IsInitialized(newTestCtx())
	require.NoError(t, err)
	assert.True(t, got)

	// Повторный вызов — INSERT OR REPLACE, не падает.
	require.NoError(t, r.SetInitialized(newTestCtx()))
}
