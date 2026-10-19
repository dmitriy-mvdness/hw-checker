// Package handler принимает апдейты Telegram, вызывает сервис и
// редактирует экраны бота. Здесь только транспорт и представление,
// вся бизнес-логика — в service.
package handler

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/dmitriy-mvdness/hw-checker/internal/keyboard"
	"github.com/dmitriy-mvdness/hw-checker/internal/service"
	"github.com/dmitriy-mvdness/hw-checker/internal/session"
	"go.uber.org/zap"
)

// prefixRoute — префиксный маршрут callback-данных и его обработчик.
type prefixRoute struct {
	prefix string
	fn     callbackPrefixHandlerFunc
}

// Handler держит сервис, состояния пользователей и таблицу маршрутов.
type Handler struct {
	svc        *service.Service
	states     *session.States
	log        *zap.Logger
	httpClient *http.Client

	callbackRoutes       map[string]callbackHandlerFunc
	callbackPrefixRoutes []prefixRoute

	// wg считает обработчики, которые сейчас в работе — для graceful shutdown.
	// В wg попадают только message- и callback-обработчики (через drainMiddleware);
	// safeGo-горутины не в счёт, они данные не пишут.
	wg sync.WaitGroup

	// activeHandlers дублирует счётчик wg для логов: WaitGroup не отдаёт
	// свой счётчик наружу, а нам надо показывать «жду N обработчиков».
	activeHandlers atomic.Int64
}

// New создаёт Handler и заполняет таблицу callback-маршрутов.
func New(svc *service.Service, states *session.States, log *zap.Logger, httpClient *http.Client) *Handler {
	h := &Handler{
		svc:        svc,
		states:     states,
		log:        log,
		httpClient: httpClient,
	}

	h.callbackRoutes = map[string]callbackHandlerFunc{
		keyboard.CbUserAuth:      h.cbUserAuth,
		keyboard.CbRequestCode:   h.cbUserRequestCode,
		keyboard.CbAuthMenu:      h.cbAuthMenu,
		keyboard.CbMainMenu:      h.cbMainMenu,
		keyboard.CbProfile:       h.cbProfile,
		keyboard.CbLogout:        h.cbLogoutConfirm,
		keyboard.CbLogoutConfirm: h.cbLogout,
		keyboard.CbHomework:      h.cbHomework,
		keyboard.CbSupport:       h.cbSupport,
		keyboard.CbSupportTech:   h.cbSupportTech,
		keyboard.CbSupportHelp:   h.cbSupportHelp,
		keyboard.CbMySubmissions: h.cbMySubmissions,
		keyboard.CbNews:          h.cbNews,
		keyboard.CbCancelResume:  h.cbCancelResume,
	}

	h.callbackPrefixRoutes = []prefixRoute{
		{keyboard.PrefixSubject, h.cbSubject},
		{keyboard.PrefixTheme, h.cbTheme},
		{keyboard.PrefixSupport, h.cbSupportReason},
		{keyboard.PrefixResume, h.cbTheme},
	}

	return h
}

// Shutdown ждёт in-flight обработчиков или ctx.Done.
// Возвращает true, если все успели.
//
// Пишем в лог «ожидаю N», чтобы при Ctrl+C было видно, что бот
// не завис, а дожидается конкретной работы (например, скачивания файла).
func (h *Handler) Shutdown(ctx context.Context) bool {
	active := h.activeHandlers.Load()
	if active == 0 {
		return true
	}

	h.log.Info("ожидаю завершения обработчиков, прерывать нельзя (макс ждем 60 сек)",
		zap.Int64("active", active),
	)

	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		h.log.Info("все обработчики завершены")
		return true
	case <-ctx.Done():
		h.log.Warn("не все обработчики успели завершиться",
			zap.Int64("active", h.activeHandlers.Load()),
		)
		return false
	}
}
