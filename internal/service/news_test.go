package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Тесты SyncNews — логика «что нового». Тонкая работа с IsInitialized:
//   - первый запуск: только MarkSeen, никаких AddTopics (иначе студент
//     при первом запуске увидит всё как «новое»);
//   - повторный: added → AddTopics+MarkSeen, removed → RemoveTopics.

func TestSyncNews_FirstRun(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})

	b.homework.listAllHomeworkThemesFn = func(_ context.Context) ([]string, error) {
		return []string{"T-1", "T-2", "T-3"}, nil
	}
	// IsInitialized по умолчанию false.

	err := svc.SyncNews(context.Background())
	require.NoError(t, err)

	// Всё, что есть, помечено как seen.
	require.Len(t, b.news.markedSeen, 1)
	assert.ElementsMatch(t, []string{"T-1", "T-2", "T-3"}, b.news.markedSeen[0])

	// AddTopics не вызывался — иначе новости покажутся на первом запуске.
	assert.Empty(t, b.news.addedTopics)
	assert.Empty(t, b.news.removedTopics)

	// Флаг инициализации установлен.
	assert.True(t, b.news.initialized)
}

func TestSyncNews_AddsNewThemes(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})

	b.homework.listAllHomeworkThemesFn = func(_ context.Context) ([]string, error) {
		return []string{"T-1", "T-2", "T-3"}, nil
	}
	b.news.initialized = true
	// Раньше видели T-1 и T-2, теперь появился T-3.
	b.news.listSeenFn = func(_ context.Context) (map[string]bool, error) {
		return map[string]bool{"T-1": true, "T-2": true}, nil
	}

	err := svc.SyncNews(context.Background())
	require.NoError(t, err)

	// Только T-3 должен попасть в AddTopics и MarkSeen.
	require.Len(t, b.news.addedTopics, 1)
	assert.Equal(t, []string{"T-3"}, b.news.addedTopics[0])

	require.Len(t, b.news.markedSeen, 1)
	assert.Equal(t, []string{"T-3"}, b.news.markedSeen[0])

	// Удалений не было.
	assert.Empty(t, b.news.removedTopics)
}

func TestSyncNews_RemovesGoneThemes(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})

	// В выгрузке только T-1.
	b.homework.listAllHomeworkThemesFn = func(_ context.Context) ([]string, error) {
		return []string{"T-1"}, nil
	}
	b.news.initialized = true
	// Раньше видели T-1, T-2, T-3 — двое исчезли.
	b.news.listSeenFn = func(_ context.Context) (map[string]bool, error) {
		return map[string]bool{"T-1": true, "T-2": true, "T-3": true}, nil
	}

	err := svc.SyncNews(context.Background())
	require.NoError(t, err)

	// AddTopics не вызывался — новых нет.
	assert.Empty(t, b.news.addedTopics)

	// RemoveTopics получил T-2 и T-3 (порядок не гарантирован).
	require.Len(t, b.news.removedTopics, 1)
	assert.ElementsMatch(t, []string{"T-2", "T-3"}, b.news.removedTopics[0])
}

func TestSyncNews_NoChanges(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})

	b.homework.listAllHomeworkThemesFn = func(_ context.Context) ([]string, error) {
		return []string{"T-1", "T-2"}, nil
	}
	b.news.initialized = true
	b.news.listSeenFn = func(_ context.Context) (map[string]bool, error) {
		return map[string]bool{"T-1": true, "T-2": true}, nil
	}

	err := svc.SyncNews(context.Background())
	require.NoError(t, err)

	// Ни добавлений, ни удалений.
	assert.Empty(t, b.news.addedTopics)
	assert.Empty(t, b.news.removedTopics)
	assert.Empty(t, b.news.markedSeen)
}

func TestSyncNews_ListThemesError(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})
	b.homework.listAllHomeworkThemesFn = func(_ context.Context) ([]string, error) {
		return nil, assert.AnError
	}

	err := svc.SyncNews(context.Background())

	require.Error(t, err)
	// До news дело не дошло.
	assert.Empty(t, b.news.markedSeen)
}
