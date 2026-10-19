package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// Тесты SubmitHomework. Самый важный — откат файла при ошибке БД:
// если файл сохранился, а запись в submissions не прошла, файл-сирота
// должен быть удалён, иначе копится мусор на диске.

// saveOK — настройка фейка студента и темы для happy-path.
// Возвращает student и theme, чтобы тесты могли их переиспользовать.
func saveOK(b *fakeBundle) (domain.Student, domain.Theme) {
	student := domain.Student{Code: 42, Name: "Иванов Иван", PersonalCode: "CODE"}
	theme := domain.Theme{Code: "T-1", Name: "Тема 1", SubjectCode: 5}

	b.accounts.getFn = func(_ context.Context, _ int64) (domain.Account, error) {
		return domain.Account{TelegramID: 100, PersonalCode: "CODE"}, nil
	}
	b.students.getFn = func(_ context.Context, _ string) (domain.Student, error) {
		return student, nil
	}
	b.homework.getThemeFn = func(_ context.Context, _ int64, _ string) (domain.Theme, error) {
		return theme, nil
	}
	return student, theme
}

func TestSubmitHomework_Success(t *testing.T) {
	svc, b := newTestService(ServiceConfig{
		MaxSubmissions: 3,
		MaxFileSize:    10 * 1024 * 1024,
		AllowedExts:    []string{"pdf", "docx"},
	})
	saveOK(b)

	// Уже 1 сдача, значит попытка будет 2.
	b.submissions.countFn = func(_ context.Context, _ int64, _ string) (int, error) {
		return 1, nil
	}

	// Файл «сохраняется» и возвращает путь.
	b.files.saveHomeworkFn = func(
		student domain.Student, theme domain.Theme, attempt int,
		originalName string, data []byte,
	) (string, error) {
		assert.Equal(t, int64(42), student.Code)
		assert.Equal(t, "T-1", theme.Code)
		assert.Equal(t, 2, attempt, "attempt должен быть count+1")
		assert.Equal(t, "hw.pdf", originalName)
		assert.Equal(t, []byte("data"), data)
		return "homework/G/T-1/file.pdf", nil
	}

	// БД возвращает ID=99.
	b.submissions.createFn = func(_ context.Context, sub domain.Submission) (int64, error) {
		assert.Equal(t, int64(42), sub.StudentCode)
		assert.Equal(t, "T-1", sub.ThemeCode)
		assert.Equal(t, 2, sub.Attempt)
		assert.Equal(t, "homework/G/T-1/file.pdf", sub.FilePath)
		assert.Equal(t, "hw.pdf", sub.OriginalName)
		return 99, nil
	}

	sub, err := svc.SubmitHomework(
		context.Background(), 100, "T-1", "hw.pdf", []byte("data"),
	)

	require.NoError(t, err)
	assert.Equal(t, int64(99), sub.ID)
	assert.Equal(t, 2, sub.Attempt)
	assert.Equal(t, "homework/G/T-1/file.pdf", sub.FilePath)

	// При успехе файл не удаляется.
	assert.Empty(t, b.files.removedPaths)
}

func TestSubmitHomework_NotLoggedIn(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})

	// Аккаунта нет — сервис вернёт ErrNotFound, до темы и файла не дойдёт.
	_, err := svc.SubmitHomework(
		context.Background(), 100, "T-1", "hw.pdf", []byte("data"),
	)

	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.Empty(t, b.submissions.createdSubs)
}

func TestSubmitHomework_ThemeNotFound(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})
	b.accounts.getFn = func(_ context.Context, _ int64) (domain.Account, error) {
		return domain.Account{TelegramID: 100, PersonalCode: "CODE"}, nil
	}
	b.students.getFn = func(_ context.Context, _ string) (domain.Student, error) {
		return domain.Student{Code: 42}, nil
	}
	// homework.getThemeFn по умолчанию ErrNotFound.

	_, err := svc.SubmitHomework(
		context.Background(), 100, "T-1", "hw.pdf", []byte("data"),
	)

	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.Empty(t, b.submissions.createdSubs)
}

func TestSubmitHomework_BadExt(t *testing.T) {
	svc, b := newTestService(ServiceConfig{
		MaxSubmissions: 3,
		MaxFileSize:    10 * 1024 * 1024,
		AllowedExts:    []string{"pdf"},
	})
	saveOK(b)

	_, err := svc.SubmitHomework(
		context.Background(), 100, "T-1", "hw.exe", []byte("data"),
	)

	assert.ErrorIs(t, err, domain.ErrBadFileType)
	assert.Empty(t, b.submissions.createdSubs)
}

func TestSubmitHomework_FileTooLarge(t *testing.T) {
	svc, b := newTestService(ServiceConfig{
		MaxSubmissions: 3,
		MaxFileSize:    10, // 10 байт
		AllowedExts:    []string{"pdf"},
	})
	saveOK(b)

	_, err := svc.SubmitHomework(
		context.Background(), 100, "T-1", "hw.pdf", []byte("too much data here"),
	)

	assert.ErrorIs(t, err, domain.ErrFileTooLarge)
	assert.Empty(t, b.submissions.createdSubs)
}

func TestSubmitHomework_TooManyAttempts(t *testing.T) {
	svc, b := newTestService(ServiceConfig{
		MaxSubmissions: 3,
		MaxFileSize:    10 * 1024 * 1024,
		AllowedExts:    []string{"pdf"},
	})
	saveOK(b)
	b.submissions.countFn = func(_ context.Context, _ int64, _ string) (int, error) {
		return 3, nil // уже максимум
	}

	_, err := svc.SubmitHomework(
		context.Background(), 100, "T-1", "hw.pdf", []byte("data"),
	)

	assert.ErrorIs(t, err, domain.ErrTooManyAttempts)
	assert.Empty(t, b.submissions.createdSubs, "запись не должна создаваться")
	assert.Empty(t, b.files.removedPaths, "файл не сохранялся — нечего удалять")
}

func TestSubmitHomework_RollbackOnDBError(t *testing.T) {
	// Главный кейс: файл сохранился, но создание записи в БД упало.
	// Сервис должен удалить только что сохранённый файл, иначе он
	// останется мусором на диске — в БД его нет, никто не найдёт.
	svc, b := newTestService(ServiceConfig{
		MaxSubmissions: 3,
		MaxFileSize:    10 * 1024 * 1024,
		AllowedExts:    []string{"pdf"},
	})
	saveOK(b)

	const savedPath = "homework/G/T-1/file.pdf"
	b.files.saveHomeworkFn = func(
		domain.Student, domain.Theme, int, string, []byte,
	) (string, error) {
		return savedPath, nil
	}
	b.submissions.createFn = func(_ context.Context, _ domain.Submission) (int64, error) {
		return 0, assert.AnError
	}

	_, err := svc.SubmitHomework(
		context.Background(), 100, "T-1", "hw.pdf", []byte("data"),
	)

	require.Error(t, err)
	// Удаление вызвано с тем же путём, что вернул SaveHomework.
	assert.Equal(t, []string{savedPath}, b.files.removedPaths,
		"файл-сирота должен быть удалён")
}

// --- Legacy-сдачи (homework старого бота) ------------------------------
//
// Ключевые инварианты:
//   1. Legacy-сдача делает тему зелёной, даже если new-attempts = 0.
//   2. Legacy-сдача НЕ съедает попытки нового бота: count остаётся 0,
//      SubmitHomework пропускает до maxSubmissions.
//   3. Ошибка legacy-репозитория не роняет экран — работаем без него.

func TestThemes_LegacyMarksSubmitted(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})
	saveOK(b) // student с PersonalCode = "CODE"
	b.homework.listThemesFn = func(_ context.Context, _ int64, _ int64) ([]domain.Theme, error) {
		return []domain.Theme{
			{Code: "T-1", Name: "Тема 1", SubjectCode: 5},
			{Code: "T-2", Name: "Тема 2", SubjectCode: 5},
		}, nil
	}
	// В новом боте сдач нет.
	b.submissions.submittedThemeCodesFn = func(_ context.Context, _ int64, _ []string) (map[string]bool, error) {
		return map[string]bool{}, nil
	}
	// В старом — T-1 сдана.
	b.legacy.submittedThemeCodesFn = func(_ context.Context, code string, _ []string) (map[string]bool, error) {
		require.Equal(t, "CODE", code, "legacy должен спросить по personal_code")
		return map[string]bool{"T-1": true}, nil
	}

	themes, err := svc.Themes(context.Background(), 100, 5)
	require.NoError(t, err)
	require.Len(t, themes, 2)

	assert.True(t, themes[0].Submitted, "T-1 сдана в legacy")
	assert.False(t, themes[1].Submitted, "T-2 не сдана нигде")
}

func TestTheme_LegacyMarksSubmitted(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})
	saveOK(b)
	b.submissions.submittedThemeCodesFn = func(_ context.Context, _ int64, _ []string) (map[string]bool, error) {
		return map[string]bool{}, nil
	}
	b.legacy.submittedThemeCodesFn = func(_ context.Context, _ string, codes []string) (map[string]bool, error) {
		require.Equal(t, []string{"T-1"}, codes)
		return map[string]bool{"T-1": true}, nil
	}

	theme, err := svc.Theme(context.Background(), 100, "T-1")
	require.NoError(t, err)
	assert.True(t, theme.Submitted)
}

func TestSubmissionSummaries_LegacyOnly(t *testing.T) {
	// Тема сдана только в legacy: Attempts=0, Submitted=true.
	svc, b := newTestService(ServiceConfig{})
	saveOK(b)
	b.homework.listAllThemesFn = func(_ context.Context, _ int64) ([]domain.Theme, error) {
		return []domain.Theme{
			{Code: "T-1", Name: "Тема 1", SubjectCode: 5, SubjectName: "Математика"},
			{Code: "T-2", Name: "Тема 2", SubjectCode: 5, SubjectName: "Математика"},
		}, nil
	}
	// В новом боте — пусто.
	b.submissions.attemptsByThemeCodesFn = func(_ context.Context, _ int64, _ []string) (map[string]int, error) {
		return map[string]int{}, nil
	}
	// В старом — T-1 сдана.
	b.legacy.submittedThemeCodesFn = func(_ context.Context, _ string, _ []string) (map[string]bool, error) {
		return map[string]bool{"T-1": true}, nil
	}

	summaries, err := svc.SubmissionSummaries(context.Background(), 100)
	require.NoError(t, err)
	require.Len(t, summaries, 2)

	assert.Equal(t, 0, summaries[0].Attempts)
	assert.True(t, summaries[0].Submitted, "T-1 сдана в legacy")

	assert.Equal(t, 0, summaries[1].Attempts)
	assert.False(t, summaries[1].Submitted, "T-2 не сдана нигде")
}

func TestSubmitHomework_LegacyDoesNotConsumeAttempts(t *testing.T) {
	// Главный инвариант: legacy-сдача не уменьшает число доступных
	// попыток в новом боте. Студент сдавал в старом — и здесь имеет
	// полные 3 попытки.
	svc, b := newTestService(ServiceConfig{
		MaxSubmissions: 3,
		MaxFileSize:    10 * 1024 * 1024,
		AllowedExts:    []string{"pdf"},
	})
	student, _ := saveOK(b)

	// count = 0: в новом боте ещё не сдавали.
	b.submissions.countFn = func(_ context.Context, _ int64, _ string) (int, error) {
		return 0, nil
	}
	b.files.saveHomeworkFn = func(
		st domain.Student, th domain.Theme, attempt int,
		_ string, _ []byte,
	) (string, error) {
		assert.Equal(t, student.Code, st.Code)
		assert.Equal(t, 1, attempt, "первая попытка в новом боте — всегда 1")
		return "path", nil
	}
	b.submissions.createFn = func(_ context.Context, _ domain.Submission) (int64, error) {
		return 1, nil
	}

	// Заявляем, что legacy-сдача есть.
	b.legacy.submittedThemeCodesFn = func(_ context.Context, _ string, _ []string) (map[string]bool, error) {
		return map[string]bool{"T-1": true}, nil
	}

	sub, err := svc.SubmitHomework(context.Background(), 100, "T-1", "hw.pdf", []byte("x"))
	require.NoError(t, err)
	assert.Equal(t, 1, sub.Attempt)
}

func TestThemes_LegacyError_StillWorks(t *testing.T) {
	// Legacy-репозиторий упал — экран отдаём без legacy-флагов.
	// Студент не должен видеть ошибку из-за опциональной фичи.
	svc, b := newTestService(ServiceConfig{})
	saveOK(b)
	b.homework.listThemesFn = func(_ context.Context, _ int64, _ int64) ([]domain.Theme, error) {
		return []domain.Theme{{Code: "T-1", Name: "Тема 1", SubjectCode: 5}}, nil
	}
	b.submissions.submittedThemeCodesFn = func(_ context.Context, _ int64, _ []string) (map[string]bool, error) {
		return map[string]bool{}, nil
	}
	b.legacy.submittedThemeCodesFn = func(_ context.Context, _ string, _ []string) (map[string]bool, error) {
		return nil, assert.AnError
	}

	themes, err := svc.Themes(context.Background(), 100, 5)
	require.NoError(t, err, "legacy-ошибка не должна ронять экран")
	require.Len(t, themes, 1)
	assert.False(t, themes[0].Submitted)
}
