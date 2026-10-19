// middleware.go — защита обработчиков от паник и учёт in-flight
// обработчиков для graceful shutdown.
package handler

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"
)

// recoverMiddleware ловит панику в обработчике и логирует стек.
// Не покрывает горутины изнутри — для них safeGo.
func recoverMiddleware(log *zap.Logger, next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, u *models.Update) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("паника в обработчике",
					zap.Any("recover", r),
					zap.Stack("stack"),
				)
			}
		}()
		next(ctx, b, u)
	}
}

// drainMiddleware учитывает обработчик в wg и отвязывает его ctx от
// отмены родителя. Смысл: при SIGTERM b.Start возвращается, но уже
// начатые обработчики должны довести работу до конца (записать сдачу,
// доредактировать сообщение). WithoutCancel сохраняет values, но
// игнорирует Done.
//
// Внутри recoverMiddleware — так wg.Done гарантированно вызовется
// даже при панике.
func (h *Handler) drainMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, u *models.Update) {
		h.wg.Add(1)
		h.activeHandlers.Add(1)
		defer func() {
			h.activeHandlers.Add(-1)
			h.wg.Done()
		}()

		next(context.WithoutCancel(ctx), b, u)
	}
}

// safeGo запускает fn в горутине с перехватом паник.
// В drainMiddleware не участвует: фоновые ответы данные не пишут.
func safeGo(log *zap.Logger, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("паника в горутине",
					zap.Any("recover", r),
					zap.Stack("stack"),
				)
			}
		}()
		fn()
	}()
}
