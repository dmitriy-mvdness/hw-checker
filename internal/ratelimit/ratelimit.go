// Package ratelimit — простой in-memory ограничитель частоты действий по
// ключу (обычно userID). Реализация — fixed window: окно длиной window,
// в нём разрешено не больше limit попыток. На 11-й попытке — блок до
// конца текущего окна.
//
// Не для безопасности — для защиты от случайного флуда: если кто-то в
// цикле долбит Login или спамит кнопками, лимитер отдаёт false и сервис
// не идёт в БД лишний раз.

package ratelimit

import (
	"sync"
	"time"
)

const (
	// defaultWindow — длина окна. Минута — компромисс: достаточно,
	// чтобы отсечь спам, и не раздражает нормального пользователя.
	defaultWindow = time.Minute

	// cleanupInterval — как часто фоновый воркер проходит по map и
	// удаляет мёртвые записи.
	cleanupInterval = 5 * time.Minute

	// entryTTL — через сколько после конца окна запись считается
	// мёртвой. Чистим только то, что давно не трогали: если пользователь
	// вернётся в пределах TTL, окно уже сбросилось, но запись ещё есть.
	entryTTL = 10 * time.Minute
)

// entry — состояние окна одного ключа. count — сколько попыток уже
// сделано в текущем окне, resetAt — когда окно закрывается. Когда
// наступает resetAt, следующая попытка начинает новое окно с нулём.
type entry struct {
	count   int
	resetAt time.Time
}

// Limiter — потокобезопасный набор окон по ключу (userID).
type Limiter struct {
	mu      sync.Mutex
	entries map[int64]*entry

	limit  int           // сколько попыток за окно
	window time.Duration // длина окна

	stopCh   chan struct{}
	stopOnce sync.Once
}

// New создаёт лимитер с заданным числом попыток за минуту и запускает
// фоновую очистку. attemptsPerMinute должно быть > 0.
//
// Пример: New(10) — 10 попыток с момента первой, дальше блок до конца
// минуты.
func New(attemptsPerMinute int) *Limiter {
	return newWithWindow(attemptsPerMinute, defaultWindow)
}

// newWithWindow — конструктор с явным окном. Неэкспортирован и нужен
// только тестам: чтобы не ждать минуту в тесте «окно истекло»,
// подставляем маленькое окно (например, 50 мс).
func newWithWindow(attemptsPerMinute int, window time.Duration) *Limiter {
	if attemptsPerMinute <= 0 {
		attemptsPerMinute = 1
	}
	l := &Limiter{
		entries: make(map[int64]*entry),
		limit:   attemptsPerMinute,
		window:  window,
		stopCh:  make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// Allow сообщает, разрешена ли попытка для ключа. Если разрешена —
// увеличивает счётчик текущего окна. Если окно истекло — начинает новое.
//
// Возвращает false, если попытки в окне исчерпаны. В этом случае
// RetryAfter(key) даст время до конца окна.
func (l *Limiter) Allow(key int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e, ok := l.entries[key]

	// Нет записи или окно истекло — стартуем новое.
	if !ok || now.After(e.resetAt) {
		l.entries[key] = &entry{
			count:   1,
			resetAt: now.Add(l.window),
		}
		return true
	}

	// Окно активно, попытки ещё есть.
	if e.count < l.limit {
		e.count++
		return true
	}

	// Окно активно, попытки исчерпаны.
	return false
}

// RetryAfter возвращает, сколько осталось до конца текущего окна, если
// ключ сейчас заблокирован. Если ключ не заблокирован (или записи нет),
// возвращает 0.
//
// Используется в текстах: «Подожди N сек.», а не абстрактное «подожди
// минуту».
func (l *Limiter) RetryAfter(key int64) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[key]
	if !ok {
		return 0
	}
	if e.count < l.limit {
		return 0
	}
	remaining := time.Until(e.resetAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Reset сбрасывает состояние ключа. Используется при успешном входе:
// после логина остатки лимита не нужны, а при следующем logout
// пользователь должен получить свежее окно.
func (l *Limiter) Reset(key int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// Close останавливает фоновую очистку. Идемпотентен.
func (l *Limiter) Close() {
	l.stopOnce.Do(func() {
		close(l.stopCh)
	})
}

// cleanupLoop — фоновый воркер, тикает по таймеру и зовёт cleanup,
// пока Close не закроет stopCh.
func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.cleanup()
		}
	}
}

// cleanup удаляет записи, чьё окно закончилось давно (дольше entryTTL
// назад). Без этого map рос бы бесконечно: ~50 юзеров в день — это
// ~18k записей за год, не критично, но принцип «не течёт» соблюсти
// дёшево.
func (l *Limiter) cleanup() {
	cutoff := time.Now().Add(-entryTTL)

	l.mu.Lock()
	defer l.mu.Unlock()

	for k, e := range l.entries {
		if e.resetAt.Before(cutoff) {
			delete(l.entries, k)
		}
	}
}
