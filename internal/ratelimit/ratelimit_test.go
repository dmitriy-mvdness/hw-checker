package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Тесты ratelimit работают через newWithWindow — маленькое окно
// (десятки миллисекунд) вместо минуты, чтобы не спать в тестах.

// waitTilBlocked — утилита: ждёт, пока RetryAfter(key) вернёт 0 (окно
// сброшено) или кончится отведённое время. Нужна из-за ограниченного
// разрешения time.Now: если тест ходит вокруг границы окна, лучше
// переждать, чем ловить флейки.
func waitTilFree(t *testing.T, l *Limiter, key int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l.RetryAfter(key) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("окно не сбросилось за %s", timeout)
}

func TestNew_ZeroLimit_BecomesOne(t *testing.T) {
	// New(0) / New(-1): лимитер не паникует, limit становится 1.
	l := New(0)
	defer l.Close()

	assert.True(t, l.Allow(1))
	assert.False(t, l.Allow(1))

	l2 := New(-5)
	defer l2.Close()

	assert.True(t, l2.Allow(1))
	assert.False(t, l2.Allow(1))
}

func TestAllow_UpToLimit(t *testing.T) {
	l := newWithWindow(3, time.Minute)
	defer l.Close()

	assert.True(t, l.Allow(1))
	assert.True(t, l.Allow(1))
	assert.True(t, l.Allow(1))
	assert.False(t, l.Allow(1), "4-я попытка должна упереться в лимит")
}

func TestAllow_DifferentUsers(t *testing.T) {
	l := newWithWindow(2, time.Minute)
	defer l.Close()

	assert.True(t, l.Allow(1))
	assert.True(t, l.Allow(1))
	assert.False(t, l.Allow(1))

	// Второй юзер — свой счётчик.
	assert.True(t, l.Allow(2))
	assert.True(t, l.Allow(2))
	assert.False(t, l.Allow(2))

	// Первый всё ещё заблокирован — состояние не пересекается.
	assert.False(t, l.Allow(1))
}

func TestAllow_WindowExpires(t *testing.T) {
	l := newWithWindow(1, 40*time.Millisecond)
	defer l.Close()

	assert.True(t, l.Allow(1))
	assert.False(t, l.Allow(1))

	// Ждём с запасом, потом проверяем, что окно сбросилось.
	waitTilFree(t, l, 1, time.Second)

	assert.True(t, l.Allow(1), "после окна попытка снова разрешена")
}

func TestRetryAfter_NotBlocked(t *testing.T) {
	l := newWithWindow(3, time.Minute)
	defer l.Close()

	// Пустой ключ.
	assert.Zero(t, l.RetryAfter(1))

	// Ключ с попытками, но не исчерпан.
	assert.True(t, l.Allow(1))
	assert.Zero(t, l.RetryAfter(1))
}

func TestRetryAfter_Blocked(t *testing.T) {
	l := newWithWindow(1, time.Minute)
	defer l.Close()

	assert.True(t, l.Allow(1))
	assert.False(t, l.Allow(1))

	wait := l.RetryAfter(1)
	assert.Greater(t, wait, time.Duration(0))
	assert.LessOrEqual(t, wait, time.Minute)
}

func TestRetryAfter_Decreases(t *testing.T) {
	l := newWithWindow(1, 200*time.Millisecond)
	defer l.Close()

	assert.True(t, l.Allow(1))
	assert.False(t, l.Allow(1))

	first := l.RetryAfter(1)
	time.Sleep(30 * time.Millisecond)
	second := l.RetryAfter(1)

	assert.Less(t, second, first, "RetryAfter должен уменьшаться со временем")
}

func TestRetryAfter_AfterExpiry(t *testing.T) {
	l := newWithWindow(1, 30*time.Millisecond)
	defer l.Close()

	assert.True(t, l.Allow(1))
	assert.False(t, l.Allow(1))

	waitTilFree(t, l, 1, time.Second)

	assert.Zero(t, l.RetryAfter(1))
}

func TestReset(t *testing.T) {
	l := newWithWindow(1, time.Minute)
	defer l.Close()

	assert.True(t, l.Allow(1))
	assert.False(t, l.Allow(1))
	assert.Greater(t, l.RetryAfter(1), time.Duration(0))

	l.Reset(1)

	// После Reset ключ как новый.
	assert.Zero(t, l.RetryAfter(1))
	assert.True(t, l.Allow(1))
}

func TestClose_Idempotent(t *testing.T) {
	l := newWithWindow(1, time.Minute)
	l.Close()

	// Повторный Close не должен паниковать.
	require.NotPanics(t, func() {
		l.Close()
	})
}

func TestConcurrent_Allow(t *testing.T) {
	// 100 горутин долбят Allow(1), лимит — 10. Ровно 10 должны пройти.
	// Ловит гонки в -race и регрессию в логике счётчика.
	l := newWithWindow(10, time.Minute)
	defer l.Close()

	const workers = 100
	var wg sync.WaitGroup
	var wins atomic.Int32

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if l.Allow(1) {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 10, wins.Load())
}

func TestConcurrent_DifferentKeys(t *testing.T) {
	// Много ключей параллельно — каждый должен получить свои limit
	// попыток, независимо от остальных.
	l := newWithWindow(2, time.Minute)
	defer l.Close()

	const keys = 50
	var wg sync.WaitGroup

	for k := 0; k < keys; k++ {
		wg.Add(1)
		go func(key int64) {
			defer wg.Done()
			assert.True(t, l.Allow(key))
			assert.True(t, l.Allow(key))
			assert.False(t, l.Allow(key))
		}(int64(k))
	}
	wg.Wait()
}
