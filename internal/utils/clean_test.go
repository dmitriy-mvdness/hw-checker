package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Тесты очистки и форматирования строк из 1С. Функции используются на
// экранах бота — если сломать регулярку, пользователь увидит «Математика
// (1-й курс)» вместо «Математика», и это заметит не ты, а он.

func TestCleanSubjectName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "скобки и дефис", in: "Математика (1-й курс)", want: "Математика"},
		{name: "скобки без дефиса", in: "Математика (2 курс)", want: "Математика"},
		{name: "скобки с точкой", in: "Математика (1-й курс).", want: "Математика"},
		{name: "с пробелом в конце", in: "Математика (1-й курс) ", want: "Математика"},
		{name: "без суффикса", in: "Математика", want: "Математика"},
		{name: "пустая", in: "", want: ""},
		// Название только из «1-й курс» — после очистки пусто,
		// функция должна вернуть исходник, а не пустую строку.
		{name: "только курс", in: "1-й курс", want: "1-й курс"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CleanSubjectName(tt.in))
		})
	}
}

func TestCleanThemeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "простой префикс", in: "Тема 1. Введение", want: "Введение"},
		{name: "дробный номер", in: "Тема 2.5. Сложное", want: "Сложное"},
		{name: "без точки", in: "Тема 3 Введение", want: "Введение"},
		{name: "нижний регистр", in: "тема 1. введение", want: "Введение"},
		{name: "без префикса", in: "Введение", want: "Введение"},
		{name: "без префикса с маленькой", in: "введение", want: "Введение"},
		// Только префикс без названия — после очистки пусто,
		// возвращаем исходник.
		{name: "только префикс", in: "Тема 1. ", want: "Тема 1. "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CleanThemeName(tt.in))
		})
	}
}

func TestCapitalizeAfterThemePrefix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "с префиксом", in: "Тема 1. введение", want: "Тема 1. Введение"},
		{name: "с дробным номером", in: "тема 2.5. тест", want: "тема 2.5. Тест"},
		{name: "без префикса", in: "введение", want: "Введение"},
		{name: "без префикса заглавная", in: "Введение", want: "Введение"},
		{name: "пустая", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CapitalizeAfterThemePrefix(tt.in))
		})
	}
}
