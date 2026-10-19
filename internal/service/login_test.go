package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// Тесты Login и CurrentStudent — самые важные в сервисе. Если сломан
// вход, ботом нельзя пользоваться.

func TestLogin_Success(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})

	// Студент найден по коду.
	b.students.getFn = func(_ context.Context, code string) (domain.Student, error) {
		require.Equal(t, "CODE-1", code)
		return domain.Student{Code: 42, Name: "Иванов Иван", PersonalCode: "CODE-1"}, nil
	}

	student, err := svc.Login(context.Background(), 100, "  CODE-1  ")

	require.NoError(t, err)
	assert.Equal(t, int64(42), student.Code)
	assert.Equal(t, "Иванов Иван", student.Name)

	// Аккаунт создан с обрезанным кодом (TrimSpace в Login).
	require.Len(t, b.accounts.created, 1)
	assert.Equal(t, int64(100), b.accounts.created[0].ID)
	assert.Equal(t, "CODE-1", b.accounts.created[0].Code)

	// Заявка на код удалена (best-effort).
	assert.Equal(t, []int64{100}, b.files.removedLoginIDs)

	// Лимитер сброшен после успешного входа.
	assert.Equal(t, []int64{100}, b.limiter.resetIDs)
}

func TestLogin_ErrTooManyAttempts(t *testing.T) {
	// Если лимитер не пускает — ни один репозиторий не должен
	// дёргаться. Это критично: смысл лимитера в том, чтобы защитить
	// БД и 1С-выгрузку от флуда.
	svc, b := newTestService(ServiceConfig{})
	b.limiter.allowFn = func(int64) bool { return false }

	_, err := svc.Login(context.Background(), 100, "CODE")

	assert.ErrorIs(t, err, domain.ErrTooManyAttempts)
	assert.Empty(t, b.accounts.created, "аккаунт не должен создаваться")
	assert.Empty(t, b.files.removedLoginIDs, "заявка не должна удаляться")
	assert.Empty(t, b.limiter.resetIDs, "Reset не должен вызываться")
}

func TestLogin_EmptyCode(t *testing.T) {
	// Пустой код (после TrimSpace) — ErrNotFound, без обращения к БД.
	// Лимитер при этом уже списал попытку (Allow вызван до проверки).
	svc, b := newTestService(ServiceConfig{})

	_, err := svc.Login(context.Background(), 100, "   ")

	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.Empty(t, b.accounts.created)
}

func TestLogin_CodeTooLong(t *testing.T) {
	// Код длиннее maxCodeLen (32) отсекается без обращения к БД.
	svc, b := newTestService(ServiceConfig{})

	long := make([]byte, 33)
	for i := range long {
		long[i] = 'x'
	}

	_, err := svc.Login(context.Background(), 100, string(long))

	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.Empty(t, b.accounts.created)
}

func TestLogin_StudentNotFound(t *testing.T) {
	// Студента с таким кодом нет — ErrNotFound, аккаунт не создаём.
	svc, b := newTestService(ServiceConfig{})
	// students.getFn по умолчанию возвращает ErrNotFound — оставляем.

	_, err := svc.Login(context.Background(), 100, "UNKNOWN")

	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.Empty(t, b.accounts.created)
}

func TestLogin_ErrCodeTaken(t *testing.T) {
	// Код уже привязан к другому аккаунту — пробрасываем как есть,
	// Reset лимитера НЕ вызывается (вход не состоялся).
	svc, b := newTestService(ServiceConfig{})
	b.students.getFn = func(_ context.Context, _ string) (domain.Student, error) {
		return domain.Student{Code: 42, Name: "X"}, nil
	}
	b.accounts.createFn = func(_ context.Context, _ int64, _ string) error {
		return domain.ErrCodeTaken
	}

	_, err := svc.Login(context.Background(), 100, "CODE")

	assert.ErrorIs(t, err, domain.ErrCodeTaken)
	assert.Empty(t, b.limiter.resetIDs, "при неуспехе Reset не вызывается")
}

func TestLogin_ErrAccountExists(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})
	b.students.getFn = func(_ context.Context, _ string) (domain.Student, error) {
		return domain.Student{Code: 42, Name: "X"}, nil
	}
	b.accounts.createFn = func(_ context.Context, _ int64, _ string) error {
		return domain.ErrAccountExists
	}

	_, err := svc.Login(context.Background(), 100, "CODE")

	assert.ErrorIs(t, err, domain.ErrAccountExists)
	assert.Empty(t, b.limiter.resetIDs)
}

func TestLogin_RemoveLoginRequestFails_StillSucceeds(t *testing.T) {
	// Удаление заявки — best-effort. Если оно упало, вход всё равно
	// успешен: пользователь вошёл, заявка просто останется у админа.
	svc, b := newTestService(ServiceConfig{})
	b.students.getFn = func(_ context.Context, _ string) (domain.Student, error) {
		return domain.Student{Code: 42, Name: "X"}, nil
	}
	b.files.removeLoginRequestFn = func(int64) error {
		return assert.AnError
	}

	_, err := svc.Login(context.Background(), 100, "CODE")

	assert.NoError(t, err)
	// Reset всё равно вызван.
	assert.Equal(t, []int64{100}, b.limiter.resetIDs)
}

// --- CurrentStudent -----------------------------------------------------

func TestCurrentStudent_Success(t *testing.T) {
	svc, b := newTestService(ServiceConfig{})
	b.accounts.getFn = func(_ context.Context, id int64) (domain.Account, error) {
		require.Equal(t, int64(100), id)
		return domain.Account{TelegramID: 100, PersonalCode: "CODE"}, nil
	}
	b.students.getFn = func(_ context.Context, code string) (domain.Student, error) {
		require.Equal(t, "CODE", code)
		return domain.Student{Code: 42, Name: "Иванов"}, nil
	}

	student, err := svc.CurrentStudent(context.Background(), 100)

	require.NoError(t, err)
	assert.Equal(t, int64(42), student.Code)
	// Аккаунт не удалён — всё хорошо.
	assert.Empty(t, b.accounts.deletedIDs)
}

func TestCurrentStudent_NoAccount(t *testing.T) {
	// Аккаунта нет — ErrNotFound как есть, никаких удалений.
	svc, b := newTestService(ServiceConfig{})
	// accounts.getFn по умолчанию ErrNotFound.

	_, err := svc.CurrentStudent(context.Background(), 100)

	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.Empty(t, b.accounts.deletedIDs)
}

func TestCurrentStudent_StaleAccountReset(t *testing.T) {
	// Аккаунт есть, но студента с таким кодом больше нет в выгрузке
	// (например, 1С пересоздала). Бот должен:
	//   1. вернуть ErrNotFound,
	//   2. удалить аккаунт, чтобы пользователь мог войти заново.
	svc, b := newTestService(ServiceConfig{})
	b.accounts.getFn = func(_ context.Context, _ int64) (domain.Account, error) {
		return domain.Account{TelegramID: 100, PersonalCode: "GONE"}, nil
	}
	// students.getFn по умолчанию ErrNotFound.

	_, err := svc.CurrentStudent(context.Background(), 100)

	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.Equal(t, []int64{100}, b.accounts.deletedIDs,
		"стейл-аккаунт должен быть удалён")
}

func TestCurrentStudent_StaleResetFails(t *testing.T) {
	// Аккаунт стейл, но удалить его не удалось (БД недоступна).
	// Возвращаем обёрнутую ошибку — это не ErrNotFound, а настоящая
	// проблема, её должен увидеть оператор в логах.
	svc, b := newTestService(ServiceConfig{})
	b.accounts.getFn = func(_ context.Context, _ int64) (domain.Account, error) {
		return domain.Account{TelegramID: 100, PersonalCode: "GONE"}, nil
	}
	b.accounts.deleteFn = func(_ context.Context, _ int64) error {
		return assert.AnError
	}

	_, err := svc.CurrentStudent(context.Background(), 100)

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrNotFound,
		"ошибка сброса не должна выглядеть как ErrNotFound")
	assert.Contains(t, err.Error(), "reset stale account")
}
