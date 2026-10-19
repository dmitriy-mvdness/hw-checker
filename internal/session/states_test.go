package session

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Все тесты — чёрный ящик над States. Пакетные (package session), чтобы
// не плодить session_test.go ради того же. Гонок не боимся: RWMutex
// внутри, но всё равно гоняем с -race.

func TestStates_Get_Empty(t *testing.T) {
	s := NewStates()
	got := s.Get(42)

	assert.Equal(t, StateNone, got.State)
	assert.Equal(t, 0, got.MessageID)
	assert.Empty(t, got.ThemeCode)
	assert.Empty(t, got.SupportReason)
}

func TestStates_WaitCode(t *testing.T) {
	s := NewStates()
	s.WaitCode(1, 100)

	got := s.Get(1)
	assert.Equal(t, StateWaitingCode, got.State)
	assert.Equal(t, 100, got.MessageID)
	assert.Empty(t, got.ThemeCode)
	assert.Empty(t, got.SupportReason)

	assert.True(t, s.IsWaitingCode(1))
	assert.False(t, s.IsWaitingFile(1))
	assert.False(t, s.IsWaitingSupport(1))
}

func TestStates_WaitFile(t *testing.T) {
	s := NewStates()
	s.WaitFile(1, "theme-A", 200)

	got := s.Get(1)
	assert.Equal(t, StateWaitingFile, got.State)
	assert.Equal(t, 200, got.MessageID)
	assert.Equal(t, "theme-A", got.ThemeCode)

	assert.True(t, s.IsWaitingFile(1))
	assert.False(t, s.IsWaitingCode(1))
	assert.False(t, s.IsWaitingSupport(1))
}

func TestStates_WaitSupport(t *testing.T) {
	s := NewStates()
	s.WaitSupport(1, "bug", 300)

	got := s.Get(1)
	assert.Equal(t, StateWaitingSupport, got.State)
	assert.Equal(t, 300, got.MessageID)
	assert.Equal(t, "bug", got.SupportReason)

	assert.True(t, s.IsWaitingSupport(1))
	assert.False(t, s.IsWaitingCode(1))
	assert.False(t, s.IsWaitingFile(1))
}

func TestStates_TakeFile_Once(t *testing.T) {
	s := NewStates()
	s.WaitFile(1, "theme-A", 100)

	first, ok := s.TakeFile(1)
	require.True(t, ok)
	assert.Equal(t, "theme-A", first.ThemeCode)
	assert.Equal(t, 100, first.MessageID)

	// Второй вызов должен провалиться: сессии уже нет.
	_, ok = s.TakeFile(1)
	assert.False(t, ok)

	// И состояние сброшено.
	assert.False(t, s.IsWaitingFile(1))
	assert.Equal(t, StateNone, s.Get(1).State)
}

func TestStates_TakeFile_WrongState(t *testing.T) {
	s := NewStates()
	s.WaitCode(1, 100)

	_, ok := s.TakeFile(1)
	assert.False(t, ok, "TakeFile не должен работать в StateWaitingCode")

	// Состояние не тронуто.
	assert.True(t, s.IsWaitingCode(1))
}

func TestStates_TakeSupport_Once(t *testing.T) {
	s := NewStates()
	s.WaitSupport(1, "bug", 100)

	first, ok := s.TakeSupport(1)
	require.True(t, ok)
	assert.Equal(t, "bug", first.SupportReason)
	assert.Equal(t, 100, first.MessageID)

	_, ok = s.TakeSupport(1)
	assert.False(t, ok)

	assert.False(t, s.IsWaitingSupport(1))
}

func TestStates_TakeSupport_WrongState(t *testing.T) {
	s := NewStates()
	s.WaitFile(1, "t", 100)

	_, ok := s.TakeSupport(1)
	assert.False(t, ok)
	assert.True(t, s.IsWaitingFile(1))
}

func TestStates_Clear(t *testing.T) {
	s := NewStates()

	// Clear на пустом — идемпотентно.
	s.Clear(1)
	assert.Equal(t, StateNone, s.Get(1).State)

	// Заводим состояние и чистим.
	s.WaitCode(1, 100)
	s.Clear(1)
	assert.Equal(t, StateNone, s.Get(1).State)
	assert.False(t, s.IsWaitingCode(1))
}

func TestStates_ChangeState(t *testing.T) {
	// Переход из одного режима в другой: старая сессия вытесняется
	// новой, обе не сосуществуют.
	s := NewStates()

	s.WaitCode(1, 100)
	s.WaitFile(1, "theme-A", 200)

	assert.False(t, s.IsWaitingCode(1))
	assert.True(t, s.IsWaitingFile(1))
	assert.Equal(t, "theme-A", s.Get(1).ThemeCode)
	assert.Equal(t, 200, s.Get(1).MessageID)
}

func TestStates_TakeFile_OnlyOneWins(t *testing.T) {
	// Самая важная проверка: если 20 горутин одновременно зовут TakeFile
	// на одну сессию, ровно одна должна получить true. Защищает от
	// ситуации, когда альбом из N файлов расходует N попыток.
	s := NewStates()
	s.WaitFile(1, "theme-A", 100)

	const workers = 20
	var wg sync.WaitGroup
	var wins atomic.Int32

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, ok := s.TakeFile(1); ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 1, wins.Load())
}

func TestStates_Concurrent(t *testing.T) {
	// Общий стресс: разные юзеры в разных состояниях, конкурентные
	// чтения и записи. Ловит гонки в -race.
	s := NewStates()

	const n = 100
	var wg sync.WaitGroup

	// Писатели — по одному на юзера, разные режимы по кругу.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(id int64) {
			defer wg.Done()
			switch id % 3 {
			case 0:
				s.WaitCode(id, int(id))
			case 1:
				s.WaitFile(id, "t", int(id))
			case 2:
				s.WaitSupport(id, "bug", int(id))
			}
		}(int64(i))
	}

	// Читатели.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(id int64) {
			defer wg.Done()
			_ = s.Get(id)
			_ = s.IsWaitingCode(id)
			_ = s.IsWaitingFile(id)
			_ = s.IsWaitingSupport(id)
		}(int64(i))
	}

	wg.Wait()
}

// --- TTL режима ожидания файла -----------------------------------------
//
// Проверяем через подменяемый now: сдвигаем виртуальное время вместо
// того, чтобы спать реальные 30 минут. newStatesWithClock — тестовый
// конструктор, ставит свою функцию времени.

// newStatesWithClock — States с управляемыми часами. Указатель на
// time.Time, чтобы тест мог двигать время вперёд.
func newStatesWithClock(t *testing.T) (*States, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := newStatesWithClockFn(func() time.Time { return now })
	return s, &now
}

func TestStates_WaitFile_TTLExpires(t *testing.T) {
	s, now := newStatesWithClock(t)

	s.WaitFile(1, "T-1", 100)
	require.True(t, s.IsWaitingFile(1))

	// Сдвигаем время за границу TTL.
	*now = now.Add(fileWaitTTL + time.Minute)

	// Наружу состояние не отдаётся.
	assert.False(t, s.IsWaitingFile(1))
	assert.Equal(t, StateNone, s.Get(1).State)

	// TakeFile тоже не отдаёт протухшее и стирает запись.
	_, ok := s.TakeFile(1)
	assert.False(t, ok)
}

func TestStates_WaitFile_NotExpired(t *testing.T) {
	s, now := newStatesWithClock(t)

	s.WaitFile(1, "T-1", 100)

	// За минуту до истечения TTL состояние ещё живое.
	*now = now.Add(fileWaitTTL - time.Minute)

	assert.True(t, s.IsWaitingFile(1))

	sess, ok := s.TakeFile(1)
	require.True(t, ok)
	assert.Equal(t, "T-1", sess.ThemeCode)
}

func TestStates_WaitFile_FreshClickResetsTTL(t *testing.T) {
	// Если юзер вернулся в тему и снова кликнул (WaitFile перезаписал
	// updatedAt), TTL отсчитывается заново.
	s, now := newStatesWithClock(t)

	s.WaitFile(1, "T-1", 100)

	*now = now.Add(20 * time.Minute)

	// Свежий клик — перезапись с новым updatedAt.
	s.WaitFile(1, "T-1", 200)

	*now = now.Add(20 * time.Minute)

	// Всего прошло 40 минут с первого клика, но 20 — со второго.
	assert.True(t, s.IsWaitingFile(1))
	assert.Equal(t, 200, s.Get(1).MessageID)
}

func TestStates_WaitCode_NoTTL(t *testing.T) {
	// WaitingCode не подвержен TTL: юзер может вернуться к вводу кода
	// через час, состояние должно его дождаться.
	s, now := newStatesWithClock(t)

	s.WaitCode(1, 100)

	*now = now.Add(2 * time.Hour)

	assert.True(t, s.IsWaitingCode(1))

	sess := s.Get(1)
	assert.Equal(t, StateWaitingCode, sess.State)
	assert.Equal(t, 100, sess.MessageID)
}

func TestStates_WaitSupport_NoTTL(t *testing.T) {
	// WaitingSupport тоже без TTL: обращение можно писать долго,
	// обдумывать, вернуться через час и отправить.
	s, now := newStatesWithClock(t)

	s.WaitSupport(1, "bug", 100)

	*now = now.Add(2 * time.Hour)

	assert.True(t, s.IsWaitingSupport(1))

	sess := s.Get(1)
	assert.Equal(t, StateWaitingSupport, sess.State)
	assert.Equal(t, "bug", sess.SupportReason)
}

func TestStates_TakeFile_ExpiredEntryDeleted(t *testing.T) {
	// После TakeFile на протухшей сессии запись должна исчезнуть из map.
	// Проверяем косвенно: следующий TakeFile не должен найти ничего,
	// даже если время откатить назад (запись реально удалена, а не
	// просто скрыта фильтром в Get).
	s, now := newStatesWithClock(t)

	s.WaitFile(1, "T-1", 100)

	*now = now.Add(fileWaitTTL + time.Minute)

	_, ok := s.TakeFile(1)
	require.False(t, ok)

	// Откатываем время — если бы запись осталась, она бы снова стала
	// «свежей» и TakeFile вернул бы true.
	*now = now.Add(-2 * fileWaitTTL)

	_, ok = s.TakeFile(1)
	assert.False(t, ok, "протухшая запись должна быть удалена, а не скрыта")
}

func TestStates_Get_ExpiredDoesNotDeleteEntry(t *testing.T) {
	// Get — read-only и намеренно не удаляет запись (RLock). Проверяем,
	// что после Get на протухшей сессии запись всё ещё в map: её подчистит
	// следующий Take/Clear/Wait.
	s, now := newStatesWithClock(t)

	s.WaitFile(1, "T-1", 100)
	*now = now.Add(fileWaitTTL + time.Minute)

	// Get возвращает пустышку.
	assert.Equal(t, StateNone, s.Get(1).State)

	// Но запись в map осталась — смотрим в обход TTL-фильтра.
	s.mu.RLock()
	_, stillThere := s.m[1]
	s.mu.RUnlock()
	assert.True(t, stillThere, "Get не должен удалять записи")
}
