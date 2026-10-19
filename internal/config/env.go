// env.go — мелкие обёртки над os.Getenv: строки, числа, списки, bool.
package config

import (
	"os"
	"strconv"
	"strings"
)

// getEnv читает строку, при пустом значении возвращает fallback.
func getEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))

	if value == "" {
		return fallback
	}

	return value
}

// getEnvInt читает положительное целое, при пустом или некорректном
// значении возвращает fallback.
func getEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))

	if v == "" {
		return fallback
	}

	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}

	return n
}

// getEnvIntAllowZero — как getEnvInt, но пропускает 0 как валидное
// значение. Нужен там, где 0 — это осмысленный выбор «выключено»
// (например, SNAPSHOT_MAX_AGE_HOURS=0 отключает проверку свежести).
// Отрицательные и невалидные значения — по-прежнему fallback.
func getEnvIntAllowZero(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))

	if v == "" {
		return fallback
	}

	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fallback
	}

	return n
}

// getEnvList читает список через запятую, приводит к нижнему регистру
// и отбрасывает пустые элементы. При пустом результате — fallback.
func getEnvList(key string, fallback []string) []string {
	v := strings.TrimSpace(os.Getenv(key))

	if v == "" {
		return fallback
	}

	parts := strings.Split(v, ",")
	out := parts[:0]

	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}

	if len(out) == 0 {
		return fallback
	}

	return out
}

// getEnvBool читает переменную как булево. Понимает 1/true/yes/on и
// 0/false/no/off. Если значения нет или оно непонятное — возвращает
// fallback.
func getEnvBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))

	switch v {
	case "":
		return fallback
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}
