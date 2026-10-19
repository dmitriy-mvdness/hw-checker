package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TTL заявки на код ------------------------------------------------
//
// Проверяем через подменяемые часы: сдвигаем «виртуальное» время вместо
// того, чтобы спать реальные loginRequestTTL (30 дней). newFilesWithClock —
// тестовый конструктор, ставит свою функцию времени.
//
// Дополнительно синхронизируем mtime файлов с виртуальными часами
// (syncMtime): f.now подменён, но mtime ставит ОС по реальным часам.
// LoginRequestExists сравнивает mtime с cutoff — без синхронизации
// результат зависел бы от даты запуска теста.

// newFilesWithClock — Files с управляемыми часами. Указатель на
// time.Time, чтобы тест мог двигать время вперёд.
func newFilesWithClock(t *testing.T) (*Files, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	f := NewFiles(t.TempDir())
	f.now = func() time.Time { return now }
	return f, &now
}

// syncMtime выставляет mtime файла равным виртуальному времени.
// Нужно, потому что f.now подменён, а mtime ставит ОС по реальным
// часам — иначе тест зависел бы от даты запуска.
func syncMtime(t *testing.T, path string, now time.Time) {
	t.Helper()
	require.NoError(t, os.Chtimes(path, now, now))
}

func TestLoginRequestExists_ExpiresAfterTTL(t *testing.T) {
	// Свежая заявка видна, после TTL — нет. Это главный сценарий:
	// пользователь запросил код, не получил, по истечении TTL может
	// запросить повторно.
	f, now := newFilesWithClock(t)

	const tgID int64 = 12345

	// Создаём заявку в «виртуальный» день 2026-01-15.
	_, err := f.SaveLoginRequest(tgID, "Иван", "ivan", "https://t.me/ivan")
	require.NoError(t, err)

	// Синхронизируем mtime файла с виртуальными часами.
	filePath := filepath.Join(f.base, "support", "login",
		now.Format("2006-01-02"), "tg12345.txt")
	syncMtime(t, filePath, *now)

	// Сразу после создания — заявка активна.
	exists, err := f.LoginRequestExists(tgID)
	require.NoError(t, err)
	assert.True(t, exists, "свежая заявка должна быть видна")

	// Сдвигаем время на 29 дней — TTL (30 дней) ещё не истёк.
	*now = now.Add(29 * 24 * time.Hour)
	exists, err = f.LoginRequestExists(tgID)
	require.NoError(t, err)
	assert.True(t, exists, "29 дней < 30 — заявка ещё активна")

	// Сдвигаем время ещё на 2 дня (итого 31) — TTL истёк.
	*now = now.Add(2 * 24 * time.Hour)
	exists, err = f.LoginRequestExists(tgID)
	require.NoError(t, err)
	assert.False(t, exists, "31 день > 30 — заявка протухла")
}

func TestLoginRequestExists_OldFolderIgnored(t *testing.T) {
	// Папка старше cutoff — не читаем её содержимое. Проверяем, что
	// заявка из старой папки не видна, даже если файл внутри свежий
	// по mtime (маловероятно, но защищаемся).
	f, now := newFilesWithClock(t)

	const tgID int64 = 12345

	// Создаём заявку в «старый» день.
	oldDate := now.Add(-100 * 24 * time.Hour)
	oldRelDir := filepath.Join("support", "login", oldDate.Format("2006-01-02"))
	oldAbsDir := filepath.Join(f.base, oldRelDir)
	require.NoError(t, os.MkdirAll(oldAbsDir, 0o755))

	oldFile := filepath.Join(oldAbsDir, "tg12345.txt")
	require.NoError(t, os.WriteFile(oldFile, []byte("old"), 0o644))

	// Папка старая — заявка не видна, даже если файл трогали недавно.
	exists, err := f.LoginRequestExists(tgID)
	require.NoError(t, err)
	assert.False(t, exists, "папка старше TTL — заявка не активна")
}

func TestLoginRequestExists_FreshFolderOldFile(t *testing.T) {
	// Папка сегодняшняя, но mtime файла старый — заявка не активна.
	// Защита от ручных манипуляций: файл могли перенести из старой папки.
	f, now := newFilesWithClock(t)

	const tgID int64 = 12345

	// Создаём заявку «сегодня».
	_, err := f.SaveLoginRequest(tgID, "Иван", "ivan", "https://t.me/ivan")
	require.NoError(t, err)

	// Находим файл и ставим mtime в прошлое за пределы TTL.
	date := now.Format("2006-01-02")
	filePath := filepath.Join(f.base, "support", "login", date, "tg12345.txt")
	oldTime := now.Add(-100 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(filePath, oldTime, oldTime))

	// Сдвигаем «сейчас» — но папка всё ещё «сегодня» по имени.
	// Проверяем: mtime спасает от ложного «активна».
	exists, err := f.LoginRequestExists(tgID)
	require.NoError(t, err)
	assert.False(t, exists, "старый mtime должен игнорировать заявку")
}

func TestLoginRequestExists_NoDir(t *testing.T) {
	// Папки support/login/ нет — возвращаем false, не ошибку.
	base := t.TempDir()
	f := NewFiles(base)

	exists, err := f.LoginRequestExists(999)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestLoginRequestExists_MultipleDates(t *testing.T) {
	// Заявка лежит в нескольких датовых папках (пользователь запрашивал
	// код в разные дни). Должна найтись хотя бы одна активная.
	f, now := newFilesWithClock(t)

	const tgID int64 = 12345

	// Старая заявка — 100 дней назад.
	oldDate := now.Add(-100 * 24 * time.Hour)
	oldRelDir := filepath.Join("support", "login", oldDate.Format("2006-01-02"))
	oldAbsDir := filepath.Join(f.base, oldRelDir)
	require.NoError(t, os.MkdirAll(oldAbsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldAbsDir, "tg12345.txt"),
		[]byte("old"), 0o644))

	// Свежая заявка — сегодня.
	_, err := f.SaveLoginRequest(tgID, "Иван", "ivan", "https://t.me/ivan")
	require.NoError(t, err)

	// Синхронизируем mtime свежего файла с виртуальными часами.
	freshPath := filepath.Join(f.base, "support", "login",
		now.Format("2006-01-02"), "tg12345.txt")
	syncMtime(t, freshPath, *now)

	exists, err := f.LoginRequestExists(tgID)
	require.NoError(t, err)
	assert.True(t, exists, "свежая заявка перекрывает старую")
}
