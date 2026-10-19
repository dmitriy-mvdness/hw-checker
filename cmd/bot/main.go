// Package main — точка входа Telegram-бота: читает конфиг, собирает
// зависимости (БД, репозитории, сервис, handler) и запускает long-polling.
// Логика запуска вынесена в run, чтобы корректно обрабатывать ошибки.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-telegram/bot"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/dmitriy-mvdness/hw-checker/internal/config"
	"github.com/dmitriy-mvdness/hw-checker/internal/database"
	"github.com/dmitriy-mvdness/hw-checker/internal/handler"
	"github.com/dmitriy-mvdness/hw-checker/internal/logger"
	"github.com/dmitriy-mvdness/hw-checker/internal/network"
	"github.com/dmitriy-mvdness/hw-checker/internal/ratelimit"
	"github.com/dmitriy-mvdness/hw-checker/internal/repository"
	"github.com/dmitriy-mvdness/hw-checker/internal/service"
	"github.com/dmitriy-mvdness/hw-checker/internal/session"
	"github.com/dmitriy-mvdness/hw-checker/internal/storage"
)

// loginAttemptsPerMinute — сколько попыток ввода кода разрешено одному
// пользователю за минуту. Защита не от взлома, а от флуда: если кто-то
// случайно (или скриптом) долбит код в цикле, сервис не идёт в БД на
// каждое сообщение.
const loginAttemptsPerMinute = 10

// main читает конфиг, инициализирует логгер и запускает run.
func main() {
	// Читаем .env из текущей рабочей папки. Если файла нет — тихо
	// игнорируем: переменные могут приходить из окружения (systemd,
	// Docker, ручной export). Ошибку не логируем — это норма.
	_ = godotenv.Load()

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	log, err := logger.New(cfg.Logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(1)
	}

	err = run(cfg, log)
	if err != nil {
		log.Error("приложение завершилось с ошибкой", zap.Error(err))
	}
	_ = log.Sync()

	if err != nil {
		os.Exit(1)
	}
}

// run собирает зависимости и запускает бота. Возвращает ошибку, чтобы
// main мог залогировать её и выставить код выхода.
func run(cfg *config.Config, log *zap.Logger) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// HTTP-клиент: общий для Telegram API и скачивания файлов.
	// Создаём первым — если PROXY_URL кривой, упадём до открытия БД.
	httpClient, err := network.NewHTTPClient(cfg.ProxyURL, log)
	if err != nil {
		return fmt.Errorf("http client: %w", err)
	}
	if cfg.ProxyURL == "" {
		log.Info("прокси не задан, идём напрямую")
	} else {
		log.Info("используем прокси", zap.String("url", cfg.ProxyURL))
	}

	// Базы: сначала миграции storage, потом открываем обе.
	if err := database.RunMigrations(log, cfg.Database.StoragePath); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	db, err := database.Open(log, cfg.Database.SnapshotPath, cfg.Database.StoragePath)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	// Проверка свежести snapshot: один раз при старте. Не падаем,
	// только Warn в лог. Отключается SNAPSHOT_MAX_AGE_HOURS=0.
	database.CheckSnapshotFreshness(log, cfg.Database.SnapshotPath,
		time.Duration(cfg.Database.SnapshotMaxAgeHours)*time.Hour)

	// Репозитории: по одному на каждую базу.
	accounts := repository.NewAccounts(db.Storage)         // storage.db, чтение и запись
	students := repository.NewStudents(db.Snapshot)        // snapshot.db, только чтение
	homework := repository.NewHomework(db.Snapshot)        // snapshot.db, только чтение
	legacy := repository.NewLegacySubmissions(db.Snapshot) // snapshot.db, homework старого бота, только чтение
	submissions := repository.NewSubmissions(db.Storage)   // storage.db, чтение и запись
	news := repository.NewNews(db.Storage)                 // storage.db, чтение и запись

	files := storage.NewFiles(cfg.Files.HomeworkDir)

	// Состояния входа (в памяти): кто сейчас вводит код.
	authStates := session.NewStates()

	// Лимитер попыток входа: in-memory, 10 попыток в минуту на юзера.
	// Close() останавливает фоновую очистку map.
	loginLimiter := ratelimit.New(loginAttemptsPerMinute)
	defer loginLimiter.Close()

	// Сервис, обработчики, бот.
	svc := service.New(accounts, students, homework, legacy, submissions, news, files,
		loginLimiter, log, service.ServiceConfig{
			MaxSubmissions: cfg.Files.MaxSubmissionsPerTheme,
			MaxFileSize:    cfg.Files.MaxFileSizeBytes,
			AllowedExts:    cfg.Files.AllowedExts,
		})
	h := handler.New(svc, authStates, log, httpClient)

	// Таймаут 120с — для long-polling API-запросов бота. Библиотека
	// оборачивает каждый вызов в context.WithTimeout, включая
	// getUpdates (long-poll до 50с), поэтому 120с — с двойным запасом.
	b, err := bot.New(cfg.TelegramToken,
		bot.WithHTTPClient(120*time.Second, httpClient),
		bot.WithErrorsHandler(logger.BotErrorsHandler(log)),
		bot.WithDebugHandler(logger.BotDebugHandler(log)),
	)
	if err != nil {
		return fmt.Errorf("telegram bot: %w", err)
	}
	h.Register(b)

	// Не критично: без команд бот работает, просто не будет списка в кнопке "Меню".
	if err := h.SetCommands(ctx, b); err != nil {
		log.Warn("не удалось зарегистрировать команды", zap.Error(err))
	}

	// Синхронизация новостей: что нового появилось в snapshot с прошлого запуска.
	// Не критично: если упадёт — бот всё равно работает, просто без новостей.
	if err := svc.SyncNews(ctx); err != nil {
		log.Warn("не удалось синхронизировать новости", zap.Error(err))
	}

	log.Info("бот запущен", zap.String("env", cfg.Logger.Environment))
	b.Start(ctx) // блокируется до Ctrl+C или SIGTERM

	// Дренаж: b.Start вернулся, ждём уже начатые обработчики.
	// 60 секунд: через Tor-мост 20 МБ идут 1–3 минуты. 60 сек — это
	// компромисс: короткие сдачи успевают, длинные обрываются, но
	// дольше ждать в shutdown нельзя (systemd ждут не вечно).
	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.Background(), 60*time.Second)
	defer cancelShutdown()

	h.Shutdown(shutdownCtx) // логирует результат сам

	httpClient.CloseIdleConnections()

	log.Info("бот остановлен")
	return nil
}
