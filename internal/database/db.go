// Package database открывает два соединения SQLite: snapshot.db
// (read-only, выгрузка из 1С) и storage.db (read-write, данные бота),
// а также прогоняет миграции storage.db при старте.
package database

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

// DB объединяет две независимые базы.
type DB struct {
	Snapshot *sql.DB // только чтение, выгрузка из 1С
	Storage  *sql.DB // чтение и запись, данные бота
}

// Open открывает обе базы и логирует возраст snapshot.
// При ошибке открытия storage закрывает уже открытый snapshot.
func Open(log *zap.Logger, snapshotPath, storagePath string) (*DB, error) {
	snap, err := openSnapshot(snapshotPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}

	// Полезно видеть, насколько свежие данные из 1С.
	if fi, err := os.Stat(snapshotPath); err == nil {
		log.Info("snapshot открыт (read-only)",
			zap.String("path", snapshotPath),
			zap.Time("updated_at", fi.ModTime()),
			zap.Duration("age", time.Since(fi.ModTime()).Round(time.Minute)),
		)
	}

	stor, err := openStorage(storagePath)
	if err != nil {
		snap.Close()
		return nil, fmt.Errorf("storage: %w", err)
	}
	log.Info("storage открыт (read-write)", zap.String("path", storagePath))

	return &DB{Snapshot: snap, Storage: stor}, nil
}

// Close закрывает обе базы, объединяя возможные ошибки.
func (d *DB) Close() error {
	return errors.Join(d.Snapshot.Close(), d.Storage.Close())
}

// openSnapshot открывает snapshot.db строго read-only. Файл создаёт не
// бот, а выгрузка из 1С, поэтому если файла нет — это ошибка, а не
// повод создать пустую базу.
func openSnapshot(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("файл не найден (сделайте выгрузку из 1С): %w", err)
	}

	dsn := buildDSN(path,
		"mode=ro",
		"_pragma=busy_timeout(5000)",
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// openStorage открывает storage.db в режиме чтения-записи, создаёт
// папку при необходимости и включает WAL.
func openStorage(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	dsn := buildDSN(path,
		"_pragma=foreign_keys(1)",
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=busy_timeout(5000)",
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// buildDSN собирает DSN для modernc.org/sqlite из пути и pragma-параметров.
func buildDSN(path string, params ...string) string {
	extra := strings.Join(params, "&")

	if strings.HasPrefix(path, "file:") {
		if strings.Contains(path, "?") {
			return path + "&" + extra
		}
		return path + "?" + extra
	}

	return "file:" + filepath.ToSlash(path) + "?" + extra
}
