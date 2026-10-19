package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Здесь проверяем Load и Validate. Каждый тест изолирован: t.Setenv
// восстанавливает переменные после завершения. Все ключи, которые Load
// читает, ставим явно — иначе тест подхватит переменные из окружения
// разработчика (например, экспортированный .env).
//
// t.Setenv нельзя вызывать в параллельных тестах, поэтому у нас всё
// последовательно.

// unsetAll — сбрасывает все переменные, которые читает Load, в пустые
// значения. Пустая строка в env == «не задана» для getEnv-хелперов.
func unsetAll(t *testing.T) {
	t.Helper()
	keys := []string{
		"TELEGRAM_TOKEN", "PROXY_URL",
		"APP_ENV", "LOG_LEVEL", "LOG_ENABLED", "LOG_DIR",
		"LOG_MAX_SIZE_MB", "LOG_MAX_FILES", "LOG_MAX_AGE_DAYS", "LOG_COMPRESS",
		"SNAPSHOT_DB_PATH", "STORAGE_DB_PATH",
		"UPLOADS_DIR", "MAX_FILE_SIZE_MB", "ALLOWED_FILE_EXTS",
		"MAX_SUBMISSIONS_PER_THEME",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	unsetAll(t)
	cfg := Load()
	require.NotNil(t, cfg)

	// Telegram: дефолт — пустая строка (валидация отдельно).
	assert.Equal(t, "", cfg.TelegramToken)
	assert.Equal(t, "", cfg.ProxyURL)

	// Logger.
	assert.Equal(t, "production", cfg.Logger.Environment)
	assert.Equal(t, "info", cfg.Logger.Level)
	assert.True(t, cfg.Logger.Enabled)
	assert.Equal(t, "data/logs", cfg.Logger.Dir)
	assert.Equal(t, 10, cfg.Logger.MaxSizeMB)
	assert.Equal(t, 7, cfg.Logger.MaxFiles)
	assert.Equal(t, 30, cfg.Logger.MaxAgeDays)
	assert.True(t, cfg.Logger.Compress)

	// Database.
	assert.Equal(t, "data/snapshot.db", cfg.Database.SnapshotPath)
	assert.Equal(t, "data/storage.db", cfg.Database.StoragePath)

	// Files.
	assert.Equal(t, "data/uploads/", cfg.Files.HomeworkDir)
	assert.EqualValues(t, 20*1024*1024, cfg.Files.MaxFileSizeBytes)
	assert.Equal(t,
		[]string{"pdf", "doc", "docx", "xls", "xlsx", "zip", "rar", "7z"},
		cfg.Files.AllowedExts,
	)
	assert.Equal(t, 3, cfg.Files.MaxSubmissionsPerTheme)
}

func TestLoad_FromEnv(t *testing.T) {
	unsetAll(t)

	t.Setenv("TELEGRAM_TOKEN", "abc:123")
	t.Setenv("PROXY_URL", "socks5://127.0.0.1:9150")

	t.Setenv("APP_ENV", "development")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_ENABLED", "false")
	t.Setenv("LOG_DIR", "/tmp/logs")
	t.Setenv("LOG_MAX_SIZE_MB", "5")
	t.Setenv("LOG_MAX_FILES", "3")
	t.Setenv("LOG_MAX_AGE_DAYS", "14")
	t.Setenv("LOG_COMPRESS", "no")

	t.Setenv("SNAPSHOT_DB_PATH", "/var/data/snap.db")
	t.Setenv("STORAGE_DB_PATH", "/var/data/stor.db")

	t.Setenv("UPLOADS_DIR", "/var/uploads")
	t.Setenv("MAX_FILE_SIZE_MB", "5")
	t.Setenv("ALLOWED_FILE_EXTS", "PDF, DocX")
	t.Setenv("MAX_SUBMISSIONS_PER_THEME", "10")

	cfg := Load()

	assert.Equal(t, "abc:123", cfg.TelegramToken)
	assert.Equal(t, "socks5://127.0.0.1:9150", cfg.ProxyURL)

	assert.Equal(t, "development", cfg.Logger.Environment)
	assert.Equal(t, "debug", cfg.Logger.Level)
	assert.False(t, cfg.Logger.Enabled)
	assert.Equal(t, "/tmp/logs", cfg.Logger.Dir)
	assert.Equal(t, 5, cfg.Logger.MaxSizeMB)
	assert.Equal(t, 3, cfg.Logger.MaxFiles)
	assert.Equal(t, 14, cfg.Logger.MaxAgeDays)
	assert.False(t, cfg.Logger.Compress)

	assert.Equal(t, "/var/data/snap.db", cfg.Database.SnapshotPath)
	assert.Equal(t, "/var/data/stor.db", cfg.Database.StoragePath)

	assert.Equal(t, "/var/uploads", cfg.Files.HomeworkDir)
	// 5 МБ в байтах: int64.
	assert.EqualValues(t, 5*1024*1024, cfg.Files.MaxFileSizeBytes)
	// Нормализация: lowercase, trim.
	assert.Equal(t, []string{"pdf", "docx"}, cfg.Files.AllowedExts)
	assert.Equal(t, 10, cfg.Files.MaxSubmissionsPerTheme)
}

func TestLoad_FallbackOnGarbage(t *testing.T) {
	unsetAll(t)

	t.Setenv("LOG_MAX_FILES", "abc")  // не число
	t.Setenv("LOG_MAX_SIZE_MB", "-1") // отрицательное
	t.Setenv("LOG_ENABLED", "maybe")  // непонятный bool
	t.Setenv("MAX_FILE_SIZE_MB", "0") // ноль
	t.Setenv("MAX_SUBMISSIONS_PER_THEME", "xx")

	cfg := Load()

	// Все «мусорные» значения должны откатиться к дефолтам.
	assert.Equal(t, 7, cfg.Logger.MaxFiles)
	assert.Equal(t, 10, cfg.Logger.MaxSizeMB)
	assert.True(t, cfg.Logger.Enabled)
	assert.EqualValues(t, 20*1024*1024, cfg.Files.MaxFileSizeBytes)
	assert.Equal(t, 3, cfg.Files.MaxSubmissionsPerTheme)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(cfg *Config)
		wantSub string // подстрока ожидаемой ошибки; "" == ошибки быть не должно
	}{
		{
			name:    "всё задано",
			mutate:  func(cfg *Config) {}, // дефолты Load уже валидны
			wantSub: "",
		},
		{
			name: "нет токена",
			mutate: func(cfg *Config) {
				cfg.TelegramToken = ""
			},
			wantSub: "TELEGRAM_TOKEN",
		},
		{
			name: "нет snapshot",
			mutate: func(cfg *Config) {
				cfg.Database.SnapshotPath = ""
			},
			wantSub: "пути к базам",
		},
		{
			name: "нет storage",
			mutate: func(cfg *Config) {
				cfg.Database.StoragePath = ""
			},
			wantSub: "пути к базам",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetAll(t)
			t.Setenv("TELEGRAM_TOKEN", "tok")

			cfg := Load()
			tt.mutate(cfg)

			err := cfg.Validate()
			if tt.wantSub == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSub)
		})
	}
}
