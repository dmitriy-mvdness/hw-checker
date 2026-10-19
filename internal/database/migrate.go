// migrate.go — применение миграций storage.db через golang-migrate.
package database

import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"go.uber.org/zap"
)

// Миграции вшиты в бинарник: не зависят от рабочего каталога,
// достаточно одного .exe/бинарника без папки migrations рядом.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations применяет миграции только к storage.db.
// snapshot.db не мигрируется: его схему задаёт выгрузка из 1С.
//
// Функция открывает собственное соединение, потому что m.Close() закрывает
// переданный *sql.DB. Общий пул db.Storage так передавать нельзя.
// Вызывать нужно ДО Open.
func RunMigrations(log *zap.Logger, storagePath string) error {
	log.Info("применение миграций storage")

	db, err := openStorage(storagePath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	driver, err := sqlite.WithInstance(db, &sqlite.Config{})
	if err != nil {
		db.Close()
		return fmt.Errorf("sqlite driver: %w", err)
	}

	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		db.Close()
		return fmt.Errorf("migrations source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "sqlite", driver)
	if err != nil {
		src.Close()
		db.Close()
		return fmt.Errorf("migrate instance: %w", err)
	}
	defer m.Close() // закрывает и источник, и соединение с БД

	switch err := m.Up(); {
	case errors.Is(err, migrate.ErrNoChange):
		log.Info("новых миграций нет")
	case err != nil:
		return fmt.Errorf("migrate up: %w", err)
	default:
		log.Info("миграции применены")
	}

	if v, dirty, err := m.Version(); err == nil {
		log.Info("версия схемы storage", zap.Uint("version", v), zap.Bool("dirty", dirty))
	}

	return nil
}
