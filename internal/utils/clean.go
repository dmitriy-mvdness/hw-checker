// Package utils содержит мелкие хелперы для очистки и форматирования
// пользовательских данных. Ничего не знает ни про SQL, ни про Telegram.
package utils

import (
	"regexp"
	"strings"
	"unicode"
)

// Регулярки для очистки названий предметов и тем.
var (
	subjectCourseSuffixRe = regexp.MustCompile(`\s*[,\s]?\(?\d+-?й?\s*курс\)?\.?\s*$`)
	themeNumberPrefixRe   = regexp.MustCompile(`^[Тт]ема\s+\d+(?:\.\d+)?\.?\s+`)
)

// CleanSubjectName убирает «N-й курс» с конца названия предмета.
// Используется только для отображения: в БД и файлах имя остаётся как есть.
func CleanSubjectName(name string) string {
	cleaned := strings.TrimSpace(subjectCourseSuffixRe.ReplaceAllString(name, ""))
	if cleaned == "" {
		return name
	}
	return cleaned
}

// CleanThemeName убирает префикс «Тема N. » из названия темы.
// Используется на экране сдачи, где уже есть метка «Тема:».
func CleanThemeName(name string) string {
	cleaned := strings.TrimSpace(themeNumberPrefixRe.ReplaceAllString(name, ""))
	if cleaned == "" {
		return name
	}
	return capitalizeFirst(cleaned)
}

// CapitalizeAfterThemePrefix сохраняет префикс «Тема N. », но делает
// первую букву названия заглавной. Используется в списках тем.
func CapitalizeAfterThemePrefix(name string) string {
	prefix := themeNumberPrefixRe.FindString(name)
	if prefix == "" {
		return capitalizeFirst(name)
	}
	rest := name[len(prefix):]
	return prefix + capitalizeFirst(rest)
}

// capitalizeFirst делает первую руну строки заглавной.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
