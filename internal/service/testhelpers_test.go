package service

import "go.uber.org/zap"

// zapNop — логгер, который ничего не пишет. Тесты не проверяют логи
// (это отдельная тема), поэтому сервису отдаём no-op. Вынесено в
// отдельный файл, чтобы fakes_test.go не тянул зависимость на zap
// ради одной строчки.
func zapNop() *zap.Logger {
	return zap.NewNop()
}
