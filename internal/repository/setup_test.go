package repository

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// setup_test.go — общая инфраструктура тестов repository.
//
// Каждый тест получает свежую in-memory SQLite. Два принципа:
//
//  1. SetMaxOpenConns(1) — :memory: привязан к конкретному соединению.
//     Если в пуле их будет два, второй получит ДРУГУЮ пустую базу,
//     и тест развалится на ровном месте. Одного соединения хватает:
//     тесты последовательные.
//
//  2. Схема пишется в тестах руками, а не через golang-migrate.
//     Причины: (а) для snapshot-базы миграций всё равно нет — её схему
//     задаёт 1С; (б) если миграция поменяется, тест упадёт и это правильно:
//     значит, repository надо проверять под новую схему, а не молча
//     продолжать работать на старой.

// schemaStorage — схема storage.db, конкатенация 000001..000003.up.sql.
// Хранится отдельно от migrationsFS, потому что тесты не должны
// зависеть от работоспособности golang-migrate.
const schemaStorage = `
CREATE TABLE accounts (
    account_id      INTEGER  PRIMARY KEY,
    user_id         INTEGER  NOT NULL UNIQUE,
    personal_code   TEXT     NOT NULL UNIQUE,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE submissions (
    id            INTEGER  PRIMARY KEY,
    student_code  INTEGER  NOT NULL,
    theme_code    TEXT     NOT NULL,
    attempt       INTEGER  NOT NULL,
    file_path     TEXT     NOT NULL,
    original_name TEXT     NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (student_code, theme_code, attempt)
);

CREATE INDEX idx_submissions_student_theme ON submissions(student_code, theme_code);

CREATE TABLE news (
    id         INTEGER  PRIMARY KEY,
    theme_code TEXT     NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_news_created_at ON news(created_at DESC);

CREATE TABLE seen_themes (
    theme_code TEXT     PRIMARY KEY,
    seen_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// schemaSnapshot — схема snapshot.db из 1С. Минимально нужные таблицы
// и колонки, чтобы запросы Homework/Students работали. Полная схема
// 1С шире, но здесь только то, что используется в repository.
const schemaSnapshot = `
CREATE TABLE students (
    code          INTEGER PRIMARY KEY,
    name          TEXT    NOT NULL,
    personal_code TEXT    NOT NULL UNIQUE,
    group_code    INTEGER
);

CREATE TABLE "groups" (
    code        INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    stream_code INTEGER
);

CREATE TABLE streams (
    code INTEGER PRIMARY KEY,
    name TEXT    NOT NULL
);

CREATE TABLE subjects (
    code INTEGER PRIMARY KEY,
    name TEXT    NOT NULL
);

CREATE TABLE stream_subjects (
    stream_code  INTEGER NOT NULL,
    subject_code INTEGER NOT NULL,
    PRIMARY KEY (stream_code, subject_code)
);

CREATE TABLE themes (
    code         TEXT    PRIMARY KEY,
    name         TEXT    NOT NULL,
    subject_code INTEGER NOT NULL,
    has_homework INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE homework (
    id            INTEGER  PRIMARY KEY AUTOINCREMENT,
    personal_code TEXT,
    subject_code  INTEGER,
    theme_code    TEXT,
    filename      TEXT,
    received_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// newDB — открывает свежую in-memory базу со схемой schema.
// Возвращает *sql.DB и регистрирует Close через t.Cleanup.
func newDB(t *testing.T, schema string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Одно соединение — см. комментарий в шапке файла.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		t.Fatalf("apply schema: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newStorageDB — открывает storage.db со схемой миграций.
func newStorageDB(t *testing.T) *sql.DB {
	t.Helper()
	return newDB(t, schemaStorage)
}

// newSnapshotDB — открывает snapshot.db со схемой 1С.
func newSnapshotDB(t *testing.T) *sql.DB {
	t.Helper()
	return newDB(t, schemaSnapshot)
}

// --- Хелперы-инсерты ---------------------------------------------------
//
// Чтобы в тестах не писать километровые INSERT'ы руками, держим здесь
// маленькие функции. Они кидают t.Fatalf при ошибке — тест падает на
// setup, а не на невнятном "no rows".

func insertStudent(t *testing.T, db *sql.DB, code int64, name, personalCode string, groupCode sql.NullInt64) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO students (code, name, personal_code, group_code) VALUES (?, ?, ?, ?)`,
		code, name, personalCode, groupCode,
	)
	if err != nil {
		t.Fatalf("insert student: %v", err)
	}
}

func insertGroup(t *testing.T, db *sql.DB, code int64, name string, streamCode int64) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO "groups" (code, name, stream_code) VALUES (?, ?, ?)`,
		code, name, streamCode,
	)
	if err != nil {
		t.Fatalf("insert group: %v", err)
	}
}

func insertStream(t *testing.T, db *sql.DB, code int64, name string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO streams (code, name) VALUES (?, ?)`, code, name)
	if err != nil {
		t.Fatalf("insert stream: %v", err)
	}
}

func insertSubject(t *testing.T, db *sql.DB, code int64, name string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO subjects (code, name) VALUES (?, ?)`, code, name)
	if err != nil {
		t.Fatalf("insert subject: %v", err)
	}
}

func insertStreamSubject(t *testing.T, db *sql.DB, streamCode, subjectCode int64) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO stream_subjects (stream_code, subject_code) VALUES (?, ?)`,
		streamCode, subjectCode,
	)
	if err != nil {
		t.Fatalf("insert stream_subject: %v", err)
	}
}

func insertTheme(t *testing.T, db *sql.DB, code, name string, subjectCode int64, hasHomework bool) {
	t.Helper()
	hw := 0
	if hasHomework {
		hw = 1
	}
	_, err := db.Exec(
		`INSERT INTO themes (code, name, subject_code, has_homework) VALUES (?, ?, ?, ?)`,
		code, name, subjectCode, hw,
	)
	if err != nil {
		t.Fatalf("insert theme: %v", err)
	}
}

func insertAccount(t *testing.T, db *sql.DB, telegramID int64, personalCode string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO accounts (user_id, personal_code) VALUES (?, ?)`,
		telegramID, personalCode,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
}

func insertSubmission(t *testing.T, db *sql.DB, studentCode int64, themeCode string, attempt int, filePath, originalName string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO submissions (student_code, theme_code, attempt, file_path, original_name)
		 VALUES (?, ?, ?, ?, ?)`,
		studentCode, themeCode, attempt, filePath, originalName,
	)
	if err != nil {
		t.Fatalf("insert submission: %v", err)
	}
}

// newTestCtx — короткий ctx для тестов. Пока просто context.Background,
// но обёртка даёт единое место, если завтра понадобится WithTimeout.
func newTestCtx() context.Context {
	return context.Background()
}

func insertLegacySubmission(t *testing.T, db *sql.DB, personalCode string, subjectCode int64, themeCode, filename string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO homework (personal_code, subject_code, theme_code, filename)
		 VALUES (?, ?, ?, ?)`,
		personalCode, subjectCode, themeCode, filename,
	)
	if err != nil {
		t.Fatalf("insert legacy submission: %v", err)
	}
}
