# Makefile — точки входа для разработки бота.
#
# `.env` подключаем опционально (дефис = не падать, если файла нет),
# переменные экспортируем в окружение для go run / go test.
-include .env
export

.PHONY: run check test test-v build clean tidy fmt fix

# --- Кроссплатформенные команды -----------------------------------------
# Windows-make не понимает rm -rf, Unix-make не понимает `if not exist`.
ifeq ($(OS),Windows_NT)
    MKDIR = if not exist bin mkdir bin
    RMDIR = if exist bin rmdir /s /q bin
else
    MKDIR = mkdir -p bin
    RMDIR = rm -rf bin
endif

# --- Пакеты с тестами ---------------------------------------------------
# Явный список вместо ./... — чтобы в выводе не было [no test files].
# При добавлении тестов в новый пакет допиши сюда одну строку.
TEST_PKGS = \
	./internal/config/... \
	./internal/database/... \
	./internal/network/... \
	./internal/ratelimit/... \
	./internal/repository/... \
	./internal/service/... \
	./internal/session/... \
	./internal/storage/... \
	./internal/utils/...

# --- Проверка сборки ----------------------------------------------------
# Быстро. Гоняется перед build — не собираем сломанный код.
check:
	@echo Checking...
	@go build ./...
	@go vet ./...

# --- Тесты --------------------------------------------------------------
# -race         детектор гонок (session, ratelimit конкурентные)
# -count=1      отключает кеш — иначе повторный запуск вернёт (cached)
# -timeout 60s  вместо дефолтных 10 минут: зависон увидим быстрее
test:
	@echo Testing...
	@go test $(TEST_PKGS) -race -count=1 -timeout 60s

# Verbose-версия: тот же прогон + -v, чтобы видеть каждый подтест.
# Полезно при разборе одного упавшего кейса.
test-v:
	@echo Testing (verbose)...
	@go test $(TEST_PKGS) -race -count=1 -timeout 60s -v

# --- Запуск и сборка ----------------------------------------------------
run:
	@echo Starting the program...
	@go run ./cmd/bot

build: check
	@echo Building the program...
	@$(MKDIR)
	@go build -trimpath -ldflags "-s -w" -o bin/hw-checker.exe ./cmd/bot

# --- Уборка и зависимости ----------------------------------------------
clean:
	@echo Cleaning...
	@$(RMDIR)

tidy:
	@echo Updating dependencies...
	@go mod tidy

# --- Форматирование -----------------------------------------------------
# goimports — форк gofmt из golang.org/x/tools. Делает всё, что gofmt,
# плюс:
#   - сортирует импорты в три группы (stdlib / third-party / local),
#   - удаляет неиспользуемые,
#   - добавляет недостающие (если модуль уже в go.mod или в кэше).
#
# Установка (один раз):
#   go install golang.org/x/tools/cmd/goimports@latest
# Убедись, что $GOPATH/bin в PATH.
fmt:
	@echo Formatting...
	@goimports -w .

# Полная проверка: обновляет go.mod/go.sum, потом форматирует.
fix: tidy fmt
	@echo Done.