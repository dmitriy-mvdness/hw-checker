// retry.go — http.RoundTripper с повтором запросов при 429 (Telegram rate limit).
// Прозрачен для go-telegram/bot и всех исходящих запросов к api.telegram.org.
package network

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// retryMaxAttempts — сколько всего попыток на один запрос (1 + повторы).
// Telegram при 429 всегда отдаёт retry_after, обычно 1–30 секунд; трёх
// попыток более чем достаточно для спама кнопками и альбомов.
const retryMaxAttempts = 3

// retryMaxWait — верхняя граница паузы. Если Telegram просит больше,
// не ждём: пусть ошибка уйдёт наверх, а бот не висит минуты в горутине.
const retryMaxWait = 30 * time.Second

// retryBodyLimit — сколько байт тела читаем, чтобы вытащить retry_after.
// Ответ Telegram с 429 всегда короткий JSON, большего не нужно.
const retryBodyLimit = 4 << 10 // 4 KiB

// skipRetryForFileDownload сообщает, является ли запрос скачиванием
// файла с CDN Telegram. Такие запросы нельзя ретраить: повторная
// загрузка 20 МБ недопустима (трафик, время, таймаут клиента).
//
// Bot API всегда отдаёт файлы с api.telegram.org и путём /file/<...>.
// Именно эту комбинацию и проверяем: запросы к тому же хосту с другими
// путями (getUpdates, sendMessage, getFile и т.д.) — обычные, их ретраить
// можно и нужно, у них маленькое тело.
func skipRetryForFileDownload(req *http.Request) bool {
	if req.URL == nil {
		return false
	}
	return req.URL.Host == "api.telegram.org" &&
		strings.HasPrefix(req.URL.Path, "/file/")
}

// RetryTransport оборачивает RoundTripper и повторяет запрос при 429.
type RetryTransport struct {
	base http.RoundTripper
	log  *zap.Logger
}

// NewRetryTransport создаёт обёртку над базовым транспортом.
func NewRetryTransport(base http.RoundTripper, log *zap.Logger) *RetryTransport {
	return &RetryTransport{base: base, log: log}
}

// RoundTrip выполняет запрос и при 429 повторяет его с паузой.
func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.base == nil {
		return nil, errors.New("retry: base transport is nil")
	}

	// Скачивание файлов не ретраим: 20 МБ повторно — недопустимо.
	if skipRetryForFileDownload(req) {
		return t.base.RoundTrip(req)
	}

	// Тело запроса можно переслать только один раз, поэтому буферизуем.
	// GetBody позволяет пересоздать body без ручного буфера.
	var bodyBytes []byte
	if req.Body != nil && req.GetBody == nil {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("retry: read request body: %w", err)
		}
		bodyBytes = b
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 1; attempt <= retryMaxAttempts; attempt++ {
		// Пересоздаём body перед каждой попыткой, кроме первой —
		// на первой body берём как есть.
		if attempt > 1 {
			if err := resetRequestBody(req, bodyBytes); err != nil {
				return nil, err
			}
		}

		resp, err := t.base.RoundTrip(req)
		if err != nil {
			lastErr = err
			return nil, err // сетевые ошибки не ретраим — этим занимается библиотека
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		lastResp = resp
		wait, hasWait := parseRetryAfter(resp)

		// Не дождались — не ждём. Отдаём 429 наверх, вызывающий залогирует.
		if !hasWait {
			return resp, nil
		}
		if wait > retryMaxWait {
			t.log.Warn("retry: 429 с большой паузой, повтор отменён",
				zap.Duration("retry_after", wait),
				zap.String("url", req.URL.String()),
			)
			return resp, nil
		}

		// Последняя попытка — не спим, отдаём ответ как есть.
		if attempt == retryMaxAttempts {
			t.log.Warn("retry: 429, попытки исчерпаны",
				zap.Int("attempts", attempt),
				zap.Duration("retry_after", wait),
				zap.String("url", req.URL.String()),
			)
			return resp, nil
		}

		t.log.Warn("retry: 429, повтор после паузы",
			zap.Int("attempt", attempt),
			zap.Duration("wait", wait),
			zap.String("url", req.URL.String()),
		)

		// Закрываем тело, чтобы не текла память на сокетах.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, retryBodyLimit))
		_ = resp.Body.Close()

		// Ждём паузу или отмену контекста.
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}

	// Сюда не должны попадать, но на всякий случай отдаём последний ответ.
	return lastResp, lastErr
}

// resetRequestBody восстанавливает тело запроса перед повтором.
func resetRequestBody(req *http.Request, buf []byte) error {
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return fmt.Errorf("retry: get body: %w", err)
		}
		req.Body = body
		return nil
	}
	if buf == nil {
		return nil
	}
	req.Body = io.NopCloser(bytes.NewReader(buf))
	return nil
}

// parseRetryAfter извлекает паузу из ответа 429: сначала из заголовка
// Retry-After, затем — из тела {"parameters":{"retry_after":N}}.
// Возвращает (0, false), если пауза не указана.
func parseRetryAfter(resp *http.Response) (time.Duration, bool) {
	if v := strings.TrimSpace(resp.Header.Get("Retry-After")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second, true
		}
	}

	// Тело одноразовое: читаем, восстанавливаем обратно для вызывающего.
	body, err := io.ReadAll(io.LimitReader(resp.Body, retryBodyLimit))
	if err != nil {
		return 0, false
	}
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))

	var payload struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, false
	}
	if payload.Parameters.RetryAfter <= 0 {
		return 0, false
	}
	return time.Duration(payload.Parameters.RetryAfter) * time.Second, true
}
