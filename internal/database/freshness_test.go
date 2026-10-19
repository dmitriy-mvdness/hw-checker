package database

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// Тесты freshness — про то, как мы различаем свежий и устаревший файл.
// Логи перехватываем через zaptest/observer: важно проверить именно
// Warn с нужными полями, а не только отсутствие паники.
//
// Функция вызывается один раз при старте, фоновой горутины нет —
// тестов на жизненный цикл тоже нет, они были бы не о чём.

// newObservedLogger — zap-логгер, чьи записи можно прочитать в тесте.
// Возвращает логгер и объект для разбора записанных записей.
// Уровень Warn: Info нам не интересен, а Error тут не пишется.
func newObservedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.WarnLevel)
	return zap.New(core), logs
}

// writeFileWithAge — создаёт файл и выставляет ему mtime в прошлое.
// Проще, чем реально ждать трое суток.
func writeFileWithAge(t *testing.T, path string, age time.Duration) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte("dummy"), 0o644))

	mtime := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

func TestCheckSnapshotFreshness_FreshFile(t *testing.T) {
	// Файл свежий — молчим. Info про возраст пишется в Open, тут Warn
	// не нужен, иначе оператор привыкнет игнорировать этот лог.
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.db")
	writeFileWithAge(t, path, time.Hour)

	log, logs := newObservedLogger()
	CheckSnapshotFreshness(log, path, 48*time.Hour)

	assert.Zero(t, logs.Len(), "свежий файл не должен давать записей")
}

func TestCheckSnapshotFreshness_StaleFile(t *testing.T) {
	// Файл старше порога — Warn с возрастом и подсказкой.
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.db")
	writeFileWithAge(t, path, 72*time.Hour)

	log, logs := newObservedLogger()
	CheckSnapshotFreshness(log, path, 48*time.Hour)

	require.Equal(t, 1, logs.Len(), "устаревший файл должен дать ровно один Warn")

	entry := logs.All()[0]
	assert.Equal(t, "snapshot.db устарел — проверьте выгрузку из 1С", entry.Message)

	fields := entry.ContextMap()
	assert.Equal(t, path, fields["path"])
	assert.Equal(t, 48*time.Hour, fields["max_age"])

	// age округляется до часа; 72 часа ± запас на время теста.
	age, ok := fields["age"].(time.Duration)
	require.True(t, ok, "age должен быть time.Duration")
	assert.GreaterOrEqual(t, age, 71*time.Hour)
	assert.LessOrEqual(t, age, 73*time.Hour)
}

func TestCheckSnapshotFreshness_MissingFile(t *testing.T) {
	// Файла нет — Warn, но не паника. 1С могла перекладывать выгрузку,
	// это не повод считать бота сломанным.
	log, logs := newObservedLogger()
	CheckSnapshotFreshness(log, filepath.Join(t.TempDir(), "missing.db"), 48*time.Hour)

	require.Equal(t, 1, logs.Len())
	assert.Contains(t, logs.All()[0].Message, "не удалось проверить свежесть")
}

func TestCheckSnapshotFreshness_DisabledOnZero(t *testing.T) {
	// maxAge=0 — проверка отключена. Info не виден на Warn-уровне
	// перехватчика, важен сам факт: не падаем и не пишем Warn.
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.db")
	writeFileWithAge(t, path, 72*time.Hour)

	log, logs := newObservedLogger()
	CheckSnapshotFreshness(log, path, 0)

	assert.Zero(t, logs.Len())
}
