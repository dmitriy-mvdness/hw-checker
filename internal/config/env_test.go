package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Тесты getEnv* — чистые функции над os.Getenv. Изоляция через t.Setenv:
// после теста переменная автоматически восстанавливается, параллелить
// такие тесты нельзя (t.Setenv это запрещает), но нам и не нужно.
//
// Все кейсы — табличные: name + вход + ожидание. Так проще добавлять
// новые, не плодя отдельные функции.

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name     string
		value    string // что положить в переменную
		set      bool   // задавать ли переменную вообще
		fallback string
		want     string
	}{
		{name: "не задана", set: false, fallback: "fb", want: "fb"},
		{name: "пустая строка", set: true, value: "", fallback: "fb", want: "fb"},
		{name: "только пробелы", set: true, value: "   ", fallback: "fb", want: "fb"},
		{name: "значение с пробелами", set: true, value: "  hi  ", fallback: "fb", want: "hi"},
		{name: "обычное значение", set: true, value: "value", fallback: "fb", want: "value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("TEST_GET_ENV", tt.value)
			} else {
				// t.Setenv не умеет unset; используем os.Unsetenv через
				// t.Cleanup — но проще: t.Setenv("", "") даст пустую
				// строку, что в getEnv всё равно отдаст fallback.
				t.Setenv("TEST_GET_ENV", "")
			}
			got := getEnv("TEST_GET_ENV", tt.fallback)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback int
		want     int
	}{
		{name: "не задана", value: "", fallback: 7, want: 7},
		{name: "положительное", value: "42", fallback: 7, want: 42},
		{name: "с пробелами", value: " 42 ", fallback: 7, want: 42},
		{name: "не число", value: "abc", fallback: 7, want: 7},
		{name: "отрицательное", value: "-5", fallback: 7, want: 7},
		{name: "ноль", value: "0", fallback: 7, want: 7},
		{name: "дробное", value: "3.14", fallback: 7, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_GET_ENV_INT", tt.value)
			got := getEnvInt("TEST_GET_ENV_INT", tt.fallback)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetEnvIntAllowZero(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback int
		want     int
	}{
		{name: "не задана", value: "", fallback: 48, want: 48},
		{name: "положительное", value: "24", fallback: 48, want: 24},
		{name: "ноль — валидное (выключено)", value: "0", fallback: 48, want: 0},
		{name: "с пробелами", value: " 12 ", fallback: 48, want: 12},
		{name: "не число", value: "abc", fallback: 48, want: 48},
		{name: "отрицательное", value: "-1", fallback: 48, want: 48},
		{name: "дробное", value: "3.14", fallback: 48, want: 48},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_GET_ENV_INT_ZERO", tt.value)
			got := getEnvIntAllowZero("TEST_GET_ENV_INT_ZERO", tt.fallback)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetEnvList(t *testing.T) {
	fallback := []string{"a", "b"}

	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "не задана", value: "", want: fallback},
		{name: "простой список", value: "pdf,doc,docx", want: []string{"pdf", "doc", "docx"}},
		{name: "пробелы и регистр", value: " PDF , DOCX ", want: []string{"pdf", "docx"}},
		{name: "пустые элементы", value: "pdf,,docx,", want: []string{"pdf", "docx"}},
		{name: "только запятые", value: ",,,", want: fallback},
		{name: "с пробелами внутри", value: "  a  ,  b  ", want: []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_GET_ENV_LIST", tt.value)
			got := getEnvList("TEST_GET_ENV_LIST", fallback)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback bool
		want     bool
	}{
		{name: "не задана — fallback true", value: "", fallback: true, want: true},
		{name: "не задана — fallback false", value: "", fallback: false, want: false},

		// Истина во всех формах, включая регистр.
		{name: "1", value: "1", fallback: false, want: true},
		{name: "true", value: "true", fallback: false, want: true},
		{name: "TRUE", value: "TRUE", fallback: false, want: true},
		{name: "yes", value: "yes", fallback: false, want: true},
		{name: "Yes", value: "Yes", fallback: false, want: true},
		{name: "y", value: "y", fallback: false, want: true},
		{name: "on", value: "on", fallback: false, want: true},

		// Ложь во всех формах.
		{name: "0", value: "0", fallback: true, want: false},
		{name: "false", value: "false", fallback: true, want: false},
		{name: "FALSE", value: "FALSE", fallback: true, want: false},
		{name: "no", value: "no", fallback: true, want: false},
		{name: "No", value: "No", fallback: true, want: false},
		{name: "n", value: "n", fallback: true, want: false},
		{name: "off", value: "off", fallback: true, want: false},

		// Непонятное — fallback.
		{name: "непонятное true", value: "maybe", fallback: true, want: true},
		{name: "непонятное false", value: "maybe", fallback: false, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_GET_ENV_BOOL", tt.value)
			got := getEnvBool("TEST_GET_ENV_BOOL", tt.fallback)
			assert.Equal(t, tt.want, got)
		})
	}
}
