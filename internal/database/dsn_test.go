package database

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Тесты buildDSN — чистая функция сборки строки подключения SQLite.
// Хрупкое место: как склеиваются `?` и `&`, что делать, если путь уже
// начинается с file:, как нормализуются разделители пути на Windows.
func TestBuildDSN(t *testing.T) {
	// Ожидание с filepath.ToSlash — на Linux это no-op, на Windows
	// заменит \ на /. Обе платформы проходят одинаково.
	plain := filepath.ToSlash("/tmp/db.sqlite")

	tests := []struct {
		name   string
		path   string
		params []string
		want   string
	}{
		{
			name:   "простой путь без параметров",
			path:   "/tmp/db.sqlite",
			params: nil,
			want:   "file:" + plain + "?",
		},
		{
			name:   "простой путь с одним параметром",
			path:   "/tmp/db.sqlite",
			params: []string{"mode=ro"},
			want:   "file:" + plain + "?mode=ro",
		},
		{
			name:   "несколько параметров",
			path:   "/tmp/db.sqlite",
			params: []string{"mode=ro", "cache=shared"},
			want:   "file:" + plain + "?mode=ro&cache=shared",
		},
		{
			name:   "путь уже с file:",
			path:   "file:/tmp/db.sqlite",
			params: []string{"mode=ro"},
			want:   "file:/tmp/db.sqlite?mode=ro",
		},
		{
			name:   "путь file: с уже существующим ?",
			path:   "file:/tmp/db.sqlite?cache=shared",
			params: []string{"mode=ro"},
			want:   "file:/tmp/db.sqlite?cache=shared&mode=ro",
		},
		{
			name:   "file: без параметров и без ?",
			path:   "file:/tmp/db.sqlite",
			params: nil,
			want:   "file:/tmp/db.sqlite?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDSN(tt.path, tt.params...)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildDSN_WindowsBackslash(t *testing.T) {
	// Проверяем инвариант: buildDSN должен применить filepath.ToSlash
	// к переданному пути. Конкретный результат ToSlash разный на разных
	// ОС (на Linux бэкслеш не разделитель, на Windows — разделитель),
	// поэтому сравниваем не с жёсткой строкой, а с тем, что реально
	// возвращает ToSlash на текущей платформе.
	//
	// Что это гарантирует: если buildDSN однажды перестанет звать
	// ToSlash (например, при рефакторинге), тест упадёт на любой ОС.
	// Что это НЕ гарантирует: что на Windows путь будет выглядеть как
	// C:/data/test.db — это специфика конкретной ОС, её проверяет
	// сам filepath.ToSlash, а не наш код.
	const winPath = `C:\data\test.db`

	got := buildDSN(winPath, "mode=ro")
	want := "file:" + filepath.ToSlash(winPath) + "?mode=ro"

	assert.Equal(t, want, got)
}
