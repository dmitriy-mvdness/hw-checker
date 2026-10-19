// news.go — новости о новых темах: news / seen_themes / meta в storage.db.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// News работает с таблицами news / seen_themes / meta в storage.db.
type News struct {
	db *sql.DB
}

// NewNews создаёт репозиторий новостей.
func NewNews(db *sql.DB) *News {
	return &News{db: db}
}

// ListSeen возвращает коды тем, которые бот уже видел в выгрузке.
func (r *News) ListSeen(ctx context.Context) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT theme_code FROM seen_themes`)
	if err != nil {
		return nil, fmt.Errorf("list seen themes: %w", err)
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan seen: %w", err)
		}
		out[code] = true
	}
	return out, rows.Err()
}

// MarkSeen помечает темы как виденные. Используется и при первом запуске,
// и при появлении новых тем.
func (r *News) MarkSeen(ctx context.Context, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	values := strings.Repeat("(?),", len(codes))
	values = values[:len(values)-1]

	args := make([]any, len(codes))
	for i, c := range codes {
		args[i] = c
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO seen_themes (theme_code) VALUES `+values, args...)
	if err != nil {
		return fmt.Errorf("mark seen: %w", err)
	}
	return nil
}

// AddTopics добавляет темы в news (для показа студентам).
func (r *News) AddTopics(ctx context.Context, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	values := strings.Repeat("(?),", len(codes))
	values = values[:len(values)-1]

	args := make([]any, len(codes))
	for i, c := range codes {
		args[i] = c
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO news (theme_code) VALUES `+values, args...)
	if err != nil {
		return fmt.Errorf("add news: %w", err)
	}
	return nil
}

// RemoveTopics удаляет темы из news и seen_themes.
func (r *News) RemoveTopics(ctx context.Context, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	values := strings.Repeat("?,", len(codes))
	values = values[:len(values)-1]

	args := make([]any, len(codes))
	for i, c := range codes {
		args[i] = c
	}

	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM news WHERE theme_code IN (`+values+`)`, args...); err != nil {
		return fmt.Errorf("remove from news: %w", err)
	}
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM seen_themes WHERE theme_code IN (`+values+`)`, args...); err != nil {
		return fmt.Errorf("remove from seen: %w", err)
	}
	return nil
}

// ListLatest возвращает последние N записей из news.
func (r *News) ListLatest(ctx context.Context, limit int) ([]domain.NewsItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT theme_code, created_at FROM news
		 ORDER BY created_at DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list news: %w", err)
	}
	defer rows.Close()

	var items []domain.NewsItem
	for rows.Next() {
		var n domain.NewsItem
		if err := rows.Scan(&n.ThemeCode, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan news: %w", err)
		}
		items = append(items, n)
	}
	return items, rows.Err()
}

// IsInitialized сообщает, был ли уже первый запуск синхронизации.
func (r *News) IsInitialized(ctx context.Context) (bool, error) {
	var v string
	err := r.db.QueryRowContext(ctx,
		`SELECT value FROM meta WHERE key = 'news_initialized'`).Scan(&v)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check initialized: %w", err)
	}
	return v == "1", nil
}

// SetInitialized помечает, что первый запуск прошёл.
func (r *News) SetInitialized(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO meta (key, value) VALUES ('news_initialized', '1')`)
	if err != nil {
		return fmt.Errorf("set initialized: %w", err)
	}
	return nil
}
