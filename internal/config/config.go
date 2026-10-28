// Package config собирает настройки бота из переменных окружения:
// токен Telegram, параметры логгера, пути к базам и лимиты файлов.
// Все значения имеют разумные значения по умолчанию.
package config

import (
	"errors"
)

// Config — корневая структура настроек приложения.
type Config struct {
	TelegramToken string

	// ProxyURL — SOCKS5/HTTP-прокси для работы через Tor или другой
	// локальный прокси. Пусто — напрямую (норма при системном VPN).
	ProxyURL string

	Logger   LoggerConfig
	Database DatabaseConfig
	Files    FilesConfig
}

// LoggerConfig настраивает zap-logger: уровень, формат, и опциональную
// запись в файл с ротацией по дате и размеру.
type LoggerConfig struct {
	Environment string
	Level       string

	// Файловые логи. Если Enabled = false или Dir пустой — только консоль.
	Enabled    bool
	Dir        string
	MaxSizeMB  int  // размер одного файла до ротации
	MaxFiles   int  // сколько файлов хранить
	MaxAgeDays int  // через сколько дней удалять
	Compress   bool // сжимать закрытые файлы gzip
}

// DatabaseConfig — пути к snapshot.db и storage.db.
type DatabaseConfig struct {
	SnapshotPath string
	StoragePath  string

	// SnapshotMaxAgeHours — через сколько часов без обновления файл
	// snapshot.db считается устаревшим.
	SnapshotMaxAgeHours int
}

// FilesConfig — папка для сдач и лимиты файлов.
type FilesConfig struct {
	HomeworkDir            string
	MaxFileSizeBytes       int64
	AllowedExts            []string
	MaxSubmissionsPerTheme int
}

// Load читает переменные окружения и возвращает готовый Config.
func Load() *Config {
	return &Config{
		TelegramToken: getEnv("TELEGRAM_TOKEN", ""),
		ProxyURL:      getEnv("PROXY_URL", ""),

		Logger: LoggerConfig{
			Environment: getEnv("APP_ENV", "production"),
			Level:       getEnv("LOG_LEVEL", "info"),

			Enabled:    getEnvBool("LOG_ENABLED", true),
			Dir:        getEnv("LOG_DIR", "data/logs"),
			MaxSizeMB:  getEnvInt("LOG_MAX_SIZE_MB", 10),
			MaxFiles:   getEnvInt("LOG_MAX_FILES", 7),
			MaxAgeDays: getEnvInt("LOG_MAX_AGE_DAYS", 30),
			Compress:   getEnvBool("LOG_COMPRESS", true),
		},

		Database: DatabaseConfig{
			SnapshotPath:        getEnv("SNAPSHOT_DB_PATH", "data/snapshot.db"),
			StoragePath:         getEnv("STORAGE_DB_PATH", "data/storage.db"),
			SnapshotMaxAgeHours: getEnvIntAllowZero("SNAPSHOT_MAX_AGE_HOURS", 72),
		},

		Files: FilesConfig{
			HomeworkDir:      getEnv("UPLOADS_DIR", "data/uploads/"),
			MaxFileSizeBytes: int64(getEnvInt("MAX_FILE_SIZE_MB", 20)) * 1024 * 1024,
			AllowedExts: getEnvList("ALLOWED_FILE_EXTS",
				[]string{"pdf", "doc", "docx", "xls", "xlsx", "zip", "rar", "7z"}),
			MaxSubmissionsPerTheme: getEnvInt("MAX_SUBMISSIONS_PER_THEME", 3),
		},
	}
}

// Validate проверяет обязательные поля.
//
// Возвращает:
//
//   - TELEGRAM_TOKEN не задан;
//   - пути к базам данных не заданы.
func (c *Config) Validate() error {
	if c.TelegramToken == "" {
		return errors.New("TELEGRAM_TOKEN не задан")
	}
	if c.Database.SnapshotPath == "" || c.Database.StoragePath == "" {
		return errors.New("пути к базам данных не заданы")
	}
	return nil
}
