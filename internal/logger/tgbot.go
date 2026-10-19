// tgbot.go — адаптеры логгеров go-telegram/bot под zap.
package logger

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// BotErrorsHandler возвращает обработчик ошибок для go-telegram/bot,
// который пишет их в zap. Сообщения про остановку long-polling и
// временные сетевые сбои обрабатываются особо: это не ошибки
// приложения, а штатное поведение — библиотека сама переподключается.
func BotErrorsHandler(log *zap.Logger) func(err error) {
	return func(err error) {
		if err == nil {
			return
		}

		msg := err.Error()

		// Штатное завершение long-polling — вообще молчим. Библиотека
		// формулирует остановку по-разному в зависимости от версии:
		// "interrupt signal received" (Linux/macOS SIGINT),
		// "terminated signal received" (Windows и некоторые сборки).
		// Контекст отменён родителем — тоже норма при graceful shutdown.
		if strings.Contains(msg, "interrupt signal received") ||
			strings.Contains(msg, "terminated signal received") ||
			strings.Contains(msg, "context canceled") {
			return
		}

		// Временные сетевые сбои — Warn без стектрейса. Библиотека
		// переподключается сама, шуметь Error'ом не за что.
		if isTransientNetworkError(msg) {
			log.Warn("сеть недоступна, переподключаемся",
				zap.String("error", msg))
			return
		}

		// Всё остальное — реальная ошибка, с полным контекстом.
		log.Error("ошибка tgbot", zap.Error(err))
	}
}

// BotDebugHandler возвращает обработчик отладочных сообщений для
// go-telegram/bot, который пишет их в zap на уровне Debug. Сами
// сообщения появляются только при bot.WithDebug().
func BotDebugHandler(log *zap.Logger) func(format string, args ...any) {
	return func(format string, args ...any) {
		log.Debug(fmt.Sprintf(format, args...))
	}
}

// isTransientNetworkError — эвристика по подстрокам. Библиотека не
// даёт типизированных ошибок, приходится смотреть текст. Список
// маркеров покрывает Windows, Linux и macOS формулировки.
//
// Что здесь — и почему каждая группа:
//
//   - "connection refused" / "actively refused it" / "proxyconnect":
//     прокси или удалённая сторона отказывает. У нас это чаще всего
//     выключенный Tor: start.bat не поднял его, пользователь закрыл
//     окно, порт 9050 не слушается. Бот сам переподключится, когда
//     Tor вернётся.
//
//   - "timeout awaiting response headers": long-poll висит дольше
//     ResponseHeaderTimeout. Чаще всего — Tor думает или сеть
//     мигнула. Следующий getUpdates пройдёт.
//
//   - "wsarecv" / "wsasend": Windows-специфичные коды разрыва
//     соединения. Ловим как есть, потому что текст ошибки на разных
//     Windows-сборках разный.
//
//   - "EOF" / "connection reset" / "broken pipe": классика обрыва
//     TCP. Библиотека переподключится.
//
//   - "i/o timeout" / "no such host" / "network is unreachable":
//     сетевые проблемы. no such host не транзиентен сам по себе, но
//     у нас это обычно прокси DNS. При реальной проблеме бот
//     продолжит их писать, а оператор заметит по частоте.
func isTransientNetworkError(msg string) bool {
	markers := []string{
		// Отказ соединения, включая прокси.
		"connection refused",
		"actively refused it",
		"connection reset",
		"connection aborted",
		"connection attempt failed",
		"connection was aborted",

		// Таймауты.
		"i/o timeout",
		"timeout awaiting response headers",

		// Windows-специфичные коды.
		"wsarecv",
		"wsasend",

		// Обрывы и DNS.
		"EOF",
		"no such host",
		"network is unreachable",
		"broken pipe",
		"getaddrinfow",
	}
	for _, m := range markers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}
