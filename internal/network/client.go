// Package network собирает HTTP-клиент для Telegram API. Если задан
// PROXY_URL — весь трафик идёт через прокси (SOCKS5 или HTTP); если
// пусто — работаем напрямую.
package network

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

// NewHTTPClient создаёт HTTP-клиент. Пустой proxyURL — обычный клиент
// без прокси. Заданный — http.Transport с http.ProxyURL: поддерживаются
// схемы socks5:// и http:// (net/http умеет обе).
//
// Транспорт оборачивается в RetryTransport: ответы 429 от Telegram
// повторяются автоматически с паузой из retry_after.
func NewHTTPClient(proxyURL string, log *zap.Logger) (*http.Client, error) {
	transport := &http.Transport{
		// Таймаут рукопожатия TLS: защита от «зависшего» прокси.
		// 15 секунд — с запасом даже для медленного Tor-моста.
		TLSHandshakeTimeout: 15 * time.Second,

		// Ожидание заголовков ответа после отправки запроса. Тело
		// может стримиться дольше — файлы из Telegram качаются
		// медленно, но заголовки приходят быстро. 60 секунд — на
		// случай, когда прокси разогревается (Tor иногда думает).
		ResponseHeaderTimeout: 60 * time.Second,

		// Держим idle-соединения: экономит TLS-рукопожатия при частых
		// запросах к api.telegram.org.
		IdleConnTimeout: 120 * time.Second,

		// Сколько ждать установки TCP-соединения (включая прокси).
		// По умолчанию 30с, но для Tor-мостов этого мало.
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,

		// Пул idle-соединений. У Telegram один хост (api.telegram.org),
		// поэтому MaxIdleConnsPerHost решает больше, чем общий лимит.
		// Дефолт Go — 2, для нас мало: каждое новое соединение = TLS
		// handshake + прокси.
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
	}

	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse PROXY_URL: %w", err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("PROXY_URL некорректен: нужен вид socks5://host:port или http://host:port")
		}
		transport.Proxy = http.ProxyURL(u)
	}

	return &http.Client{
		Transport: NewRetryTransport(transport, log),

		// Общий таймаут на запрос, включая тело ответа. 300 секунд
		// (5 минут) — с запасом на 20 МБ при медленном соединении
		// (100 Кбит/с = 1600 сек, но реальный минимум у студентов
		// выше). Больше — риск залипнуть навсегда.
		Timeout: 300 * time.Second,
	}, nil
}
