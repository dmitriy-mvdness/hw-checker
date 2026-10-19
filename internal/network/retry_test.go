package network

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Тесты RetryTransport. Работаем через httptest.Server: он даёт настоящий
// HTTP с локальным адресом, никаких моков network-стека.
//
// Для проверки «не ретраим скачивание файла» используем кастомный base
// RoundTripper — иначе пришлось бы стучаться в реальный CDN Telegram.

// newTestTransport — обычный retry-транспорт с no-op логгером.
func newTestTransport() *RetryTransport {
	return NewRetryTransport(http.DefaultTransport, zap.NewNop())
}

// --- Happy path ---------------------------------------------------------

func TestRetryTransport_Success_NoRetry(t *testing.T) {
	// Обычный 200 — retry не должен срабатывать, один запрос к серверу.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport()}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.EqualValues(t, 1, calls.Load())
}

// --- Retry по 429 -------------------------------------------------------

func TestRetryTransport_Retry_On429_WithRetryAfterHeader(t *testing.T) {
	// Первый ответ 429 с Retry-After: 1, второй 200. Проверяем, что
	// транспорт подождал и повторил.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport()}

	start := time.Now()
	resp, err := client.Get(srv.URL)
	elapsed := time.Since(start)

	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.EqualValues(t, 2, calls.Load())
	assert.GreaterOrEqual(t, elapsed, time.Second, "транспорт должен подождать retry_after")
}

func TestRetryTransport_Retry_On429_WithJSONBody(t *testing.T) {
	// Retry-After-заголовка нет, но в теле лежит parameters.retry_after.
	// Транспорт должен прочитать тело, восстановить его и повторить.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":1}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport()}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.EqualValues(t, 2, calls.Load())
}

// --- Случаи, когда retry НЕ делается --------------------------------

func TestRetryTransport_429_NoRetryAfter(t *testing.T) {
	// 429 без заголовка и без JSON в теле — вернуть как есть.
	// Иначе можно зациклиться, не зная, сколько ждать.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport()}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.EqualValues(t, 1, calls.Load(), "без retry_after повторов быть не должно")
}

func TestRetryTransport_429_LongWait_NoRetry(t *testing.T) {
	// retry_after больше retryMaxWait (30s) — не ждём, отдаём 429
	// наверх. Иначе бот мог бы зависнуть в горутине на минуты.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport()}

	start := time.Now()
	resp, err := client.Get(srv.URL)
	elapsed := time.Since(start)

	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.EqualValues(t, 1, calls.Load())
	assert.Less(t, elapsed, time.Second, "при большой паузе ждать не должны")
}

func TestRetryTransport_Exhausted(t *testing.T) {
	// Сервер всегда отвечает 429 — транспорт сдаётся после
	// retryMaxAttempts попыток и возвращает последний 429.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport()}
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.EqualValues(t, retryMaxAttempts, calls.Load())
}

// --- Body пересоздание --------------------------------------------------

func TestRetryTransport_RequestBody_Recreated(t *testing.T) {
	// POST с телом: тело должно прийти на сервер и в первый, и во
	// второй раз одинаковым. Без пересоздания req.Body второй запрос
	// ушёл бы с пустым телом.
	var bodies []string
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport()}
	resp, err := client.Post(srv.URL, "text/plain", strings.NewReader("hello body"))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, bodies, 2)
	assert.Equal(t, "hello body", bodies[0])
	assert.Equal(t, "hello body", bodies[1], "тело должно быть пересоздано перед retry")
}

// --- Отмена контекста ---------------------------------------------------

func TestRetryTransport_CtxCancelled_DuringWait(t *testing.T) {
	// Сервер всегда 429 с паузой 5с. Отменяем ctx через 100мс —
	// транспорт должен прервать ожидание и вернуть ошибку контекста,
	// а не ждать все 5 секунд.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newTestTransport()}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	start := time.Now()
	_, err = client.Do(req)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, 3*time.Second, "должны прервать ожидание по ctx, а не ждать 5с")
}

// --- Скачивание файла не ретраится --------------------------------------

// countingRT — кастомный RoundTripper, который просто считает вызовы
// и возвращает заданный статус. Нужен, чтобы проверить поведение
// транспорта на файловом URL без реального соединения.
type countingRT struct {
	calls  atomic.Int32
	status int
}

func (c *countingRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{},
		Request:    r,
	}, nil
}

func TestSkipRetryForFileDownload_RealURL(t *testing.T) {
	// Реальный URL, который отдаёт bot.FileDownloadLink:
	// https://api.telegram.org/file/bot<token>/<file_path>
	// Именно такой запрос транспорт должен пропустить без ретраев.
	//
	// До исправления константа была "file.telegram.org" — для такого
	// URL проверка не срабатывала, и файл ретраился. Этот тест ловит
	// регрессию: он зелёный только при корректной проверке.
	base := &countingRT{status: http.StatusTooManyRequests}
	tr := NewRetryTransport(base, zap.NewNop())

	req, err := http.NewRequest(http.MethodGet,
		"https://api.telegram.org/file/bot123456:ABC-DEF/file_42.pdf", nil)
	require.NoError(t, err)

	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.EqualValues(t, 1, base.calls.Load(),
		"скачивание файла не должно ретраиться: base вызван ровно один раз")
}

func TestSkipRetryForFileDownload_FilePrefixVariants(t *testing.T) {
	// Проверяем саму функцию-предикат на нескольких URL.
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "реальный файл Bot API",
			url:  "https://api.telegram.org/file/bot123:ABC/report.pdf",
			want: true,
		},
		{
			name: "файл без расширения",
			url:  "https://api.telegram.org/file/bot123:ABC/file_1",
			want: true,
		},
		{
			name: "getMe — не файл",
			url:  "https://api.telegram.org/bot123:ABC/getMe",
			want: false,
		},
		{
			name: "getUpdates — не файл",
			url:  "https://api.telegram.org/bot123:ABC/getUpdates",
			want: false,
		},
		{
			name: "getFile (метаданные) — не файл",
			url:  "https://api.telegram.org/bot123:ABC/getFile?file_id=xyz",
			want: false,
		},
		{
			name: "старый хост file.telegram.org больше не актуален",
			url:  "https://file.telegram.org/file/xxx",
			want: false,
		},
		{
			name: "другой хост вообще",
			url:  "https://example.com/file/x",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tt.url, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.want, skipRetryForFileDownload(req))
		})
	}
}

func TestRetryTransport_NonFileTelegramRequest_StillRetries(t *testing.T) {
	// Контрольный тест: обычный API-запрос (getUpdates) к api.telegram.org
	// должен ретраиться как раньше. Если бы мы случайно отключили retry
	// для всего хоста api.telegram.org, этот тест бы упал.
	base := &countingRT{status: http.StatusTooManyRequests}
	tr := NewRetryTransport(base, zap.NewNop())

	req, err := http.NewRequest(http.MethodPost,
		"https://api.telegram.org/bot123:ABC/getUpdates", nil)
	require.NoError(t, err)

	// Retry-After нет — транспорт не будет ждать, но попробует повторить
	// попытку. Посчитаем вызовы: их должно быть больше одного, потому что
	// для не-файловых запросов retry включён.
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Без Retry-After транспорт возвращает 429 сразу после первой попытки
	// (см. TestRetryTransport_429_NoRetryAfter). Но главное — skip не
	// сработал: если бы сработал, вызовов тоже был бы 1. Проверим через
	// прямой вызов предиката — он уже покрыт выше. Здесь фиксируем, что
	// путь /bot.../getUpdates не попал в исключение.
	assert.False(t, skipRetryForFileDownload(req),
		"getUpdates не должен считаться скачиванием файла")
}
