// Package storage отвечает за файловое хранилище сдач: раскладку по
// папкам, безопасные имена и атомарную запись через temp + rename.
//
// Всё состояние хранится на диске: ни один метод не держит данные в
// памяти между вызовами. Это важно для сценария «бот запускается раз
// в день» — после перезапуска все файлы и заявки остаются на месте,
// TTL считается от даты файла, а не от времени жизни процесса.
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmitriy-mvdness/hw-checker/internal/domain"
)

// loginRequestTTL — сколько заявка на код считается активной.
//
// Фактически это «как часто студент может напомнить о себе админу».
// 30 дней — компромисс: защищает от случайного спама кнопкой, но не
// запирает студента навсегда, если первая заявка потерялась.
const loginRequestTTL = 30 * 24 * time.Hour

// Files раскладывает сдачи по диску:
//
//	<base>/homework/<Группа>/<Код_темы Название>/<ts>_v<N>_<Имя>_<Код>.<ext>
//
// Все методы потокобезопасны на уровне ФС: атомарная запись через
// temp + rename, идемпотентные удаления. Внутреннего мьютекса нет —
// конкурентные вызовы для одного и того же пути редки и не ломают
// раскладку (последний rename побеждает).
type Files struct {
	base string

	// now подменяется в тестах, чтобы не ждать реальный loginRequestTTL
	// (30 дней) и не зависеть от точности системных часов. В продакшене
	// это time.Now; менять его в обход NewFiles не нужно.
	now func() time.Time
}

// NewFiles создаёт файловое хранилище с указанной корневой папкой.
// Папка не создаётся здесь: это сделает первый Save* при необходимости.
func NewFiles(base string) *Files {
	return &Files{base: base, now: time.Now}
}

// SaveHomework сохраняет файл сдачи и возвращает путь относительно base.
// Пишет через временный файл: при сбое результат не остаётся на диске.
//
// Имя файла: <ts>_v<N>_<Имя>_<Код>.<ext>. Компоненты прогоняются через
// sanitize — имена из 1С могут содержать символы, недопустимые в путях
// Windows.
//
// Ошибки: файл без расширения, ошибка создания папки/временного файла,
// ошибка записи/синхронизации/переименования. В любом случае на диске
// не остаётся частично записанного результата.
func (f *Files) SaveHomework(
	student domain.Student,
	theme domain.Theme,
	attempt int,
	originalName string,
	data []byte,
) (string, error) {
	ext := strings.ToLower(filepath.Ext(originalName))
	if ext == "" {
		return "", fmt.Errorf("файл без расширения")
	}

	group := sanitize(student.GroupName)
	if group == "" {
		group = "_no_group"
	}

	themeDir := sanitize(fmt.Sprintf("%s %s", theme.Code, theme.Name))
	ts := f.now().Format("2006-01-02_15-04-05")
	name := fmt.Sprintf("%s_v%d_%s_%s%s",
		ts, attempt,
		sanitize(student.Name),
		sanitize(student.PersonalCode),
		ext,
	)

	relDir := filepath.Join("homework", group, themeDir)
	relPath := filepath.Join(relDir, name)
	absDir := filepath.Join(f.base, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", absDir, err)
	}

	tmp, err := os.CreateTemp(absDir, ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return "", fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close temp: %w", err)
	}

	absPath := filepath.Join(f.base, relPath)
	if err := os.Rename(tmpPath, absPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("rename: %w", err)
	}

	return relPath, nil
}

// Remove удаляет файл по относительному пути. Используется, если запись
// в БД не удалась после успешного сохранения файла.
//
// Пустой путь — no-op: так удобно писать best-effort вызовы в сервисе,
// не проверяя предварительно relPath. Несуществующий файл — ошибка
// (os.Remove); вызывающий решает, критично ли это.
func (f *Files) Remove(relPath string) error {
	if relPath == "" {
		return nil
	}
	return os.Remove(filepath.Join(f.base, relPath))
}

// sanitize очищает имя от символов, недопустимых в путях Windows,
// и подрезает длину, чтобы путь не стал слишком длинным.
//
// Заменяет \ / : * ? " < > | на _. Обрезает до 60 байт (не рун) —
// для кириллицы это может разрезать многобайтовый символ, но имена
// групп/студентов из 1С такой длины практически не встречаются.
//
// Квирк (зафиксирован тестом TestSanitize): TrimSpace идёт до Trim("."),
// поэтому «. name .» превращается в « name » — точки уходят, пробелы
// остаются. На Windows такие имена обрубаются ФС, и relPath может
// разойтись с реальным файлом. В нашем сценарии не критично, но
// помните об этом, если будете менять порядок.
func sanitize(s string) string {
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		`\`, "_", `/`, "_", `:`, "_", `*`, "_", `?`, "_",
		`"`, "_", `<`, "_", `>`, "_", `|`, "_",
	)
	s = replacer.Replace(s)
	s = strings.TrimSpace(s)
	s = strings.Trim(s, ".")

	const maxLen = 60
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// LoginRequestExists — есть ли у пользователя АКТИВНЫЙ запрос кода.
//
// Активным считается файл не старше loginRequestTTL. Старые заявки
// игнорируются: студент должен иметь возможность запросить код
// повторно, если первая заявка потерялась (админ не отправил код,
// письмо не дошло, код забыт).
//
// Проверка двойная:
//   - по дате папки (быстро, без Stat): если папка старше cutoff —
//     пропускаем целиком, файла там быть не может;
//   - по mtime самого файла: защита от ручных манипуляций, когда
//     папка называется сегодняшней датой, а файл внутри старый.
//
// Рестарт бота на результат не влияет: и дата папки, и mtime — это
// метки на диске, а cutoff считается от реального системного времени.
func (f *Files) LoginRequestExists(telegramID int64) (bool, error) {
	loginDir := filepath.Join(f.base, "support", "login")
	dates, err := os.ReadDir(loginDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read login dir: %w", err)
	}

	filename := fmt.Sprintf("tg%d.txt", telegramID)
	cutoff := f.now().Add(-loginRequestTTL)

	for _, d := range dates {
		if !d.IsDir() {
			continue
		}

		// Папка названа по дате в формате 2006-01-02. Если она старше
		// cutoff — пропускаем, не читая содержимое: файла там быть не может.
		dirDate, perr := time.Parse("2006-01-02", d.Name())
		if perr != nil || dirDate.Before(cutoff) {
			continue
		}

		path := filepath.Join(loginDir, d.Name(), filename)
		info, serr := os.Stat(path)
		if serr != nil {
			continue
		}
		// Дополнительная проверка по mtime: дата папки могла совпасть
		// с «сегодня», но файл внутри — старый (перенесли руками).
		if info.ModTime().Before(cutoff) {
			continue
		}
		return true, nil
	}
	return false, nil
}

// SaveLoginRequest сохраняет запрос кода:
//
//	support/login/<YYYY-MM-DD>/tg<ID>.txt
//
// Дата — папка, чтобы админу было удобно смотреть запросы «за сегодня».
// Один пользователь — один активный файл: антиспам не даёт создать
// второй, пока не истёк loginRequestTTL. Старые файлы (из прошлых окон
// TTL) копятся в своих датовых папках — их уберёт RemoveLoginRequest
// при успешном входе. Если это станет проблемой (много папок), добавьте
// Cleanup, который вызывается при старте бота.
func (f *Files) SaveLoginRequest(telegramID int64, name, username, link string) (string, error) {
	date := f.now().Format("2006-01-02")
	relDir := filepath.Join("support", "login", date)
	relPath := filepath.Join(relDir, fmt.Sprintf("tg%d.txt", telegramID))
	absPath := filepath.Join(f.base, relPath)

	content := buildLoginText(name, username, link, f.now())
	if err := writeAtomic(absPath, []byte(content)); err != nil {
		return "", err
	}
	return relPath, nil
}

// RemoveLoginRequest удаляет запрос кода во всех датовых папках.
//
// Идемпотентна: если файла нет — ничего не делает. Вызывается после
// успешного входа, чтобы у админа не осталось висящей заявки.
//
// Best-effort по своей природе: ошибка os.Remove для одной папки не
// прерывает обход остальных, а общая ошибка не возвращается. Это
// осознанно: заявка — вспомогательный артефакт, её неудаление не должно
// ломать вход в аккаунт. Если когда-нибудь понадобится строгий режим —
// добавьте накопление ошибок через errors.Join.
func (f *Files) RemoveLoginRequest(telegramID int64) error {
	loginDir := filepath.Join(f.base, "support", "login")
	dates, err := os.ReadDir(loginDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read login dir: %w", err)
	}

	filename := fmt.Sprintf("tg%d.txt", telegramID)
	for _, d := range dates {
		if !d.IsDir() {
			continue
		}
		dateDir := filepath.Join(loginDir, d.Name())
		filePath := filepath.Join(dateDir, filename)
		if err := os.Remove(filePath); err != nil {
			continue
		}
		// Если датовая папка опустела — убираем её, чтобы support/login/
		// не зарастал пустыми днями.
		if remaining, _ := os.ReadDir(dateDir); len(remaining) == 0 {
			_ = os.Remove(dateDir)
		}
	}
	return nil
}

// SaveSupportText сохраняет текстовое обращение:
//
//	support/<reason>/<ts>_tg<ID>/info.txt
//
// Верхняя папка — код причины, внутри — папка обращения с info.txt.
// Возвращает путь к info.txt относительно base (используется в логах).
func (f *Files) SaveSupportText(req domain.SupportRequest) (string, error) {
	ts := f.now().Format("2006-01-02_15-04-05")
	relDir := filepath.Join("support", req.Reason,
		fmt.Sprintf("%s_tg%d", ts, req.TelegramID))

	infoPath := filepath.Join(f.base, relDir, "info.txt")
	content := buildSupportText(req, "", f.now())
	if err := writeAtomic(infoPath, []byte(content)); err != nil {
		return "", err
	}
	return filepath.Join(relDir, "info.txt"), nil
}

// SaveSupportFile сохраняет файл-обращение:
//
//	support/<reason>/<ts>_tg<ID>/
//	├── info.txt
//	└── <original file>
//
// Расширение в имени нормализуется к нижнему регистру, но срезается
// по оригиналу — TrimSuffix регистрозависимый. Иначе "Report.PDF"
// превратился бы в "Report.PDF.pdf".
//
// filepath.Base отсекает path traversal: "../etc/passwd.txt" станет
// "passwd.txt" и ляжет в папку обращения, а не выше base. Это важно,
// потому что originalName приходит из Telegram и может быть чем угодно.
//
// Возвращает путь к info.txt относительно base, не к вложению.
func (f *Files) SaveSupportFile(req domain.SupportRequest, originalName string, data []byte) (string, error) {
	ts := f.now().Format("2006-01-02_15-04-05")

	origExt := filepath.Ext(originalName)
	ext := strings.ToLower(origExt)

	relDir := filepath.Join("support", req.Reason,
		fmt.Sprintf("%s_tg%d", ts, req.TelegramID))

	infoContent := buildSupportText(req, originalName, f.now())
	infoPath := filepath.Join(f.base, relDir, "info.txt")
	if err := writeAtomic(infoPath, []byte(infoContent)); err != nil {
		return "", err
	}

	base := strings.TrimSuffix(filepath.Base(originalName), origExt)
	fileName := sanitize(base) + ext
	if fileName == ext {
		fileName = "file" + ext
	}
	filePath := filepath.Join(f.base, relDir, fileName)
	if err := writeAtomic(filePath, data); err != nil {
		return "", err
	}

	return filepath.Join(relDir, "info.txt"), nil
}

// writeAtomic пишет файл через временный с последующим rename.
//
// Гарантии: при ошибке на любом шаге временный файл удаляется, целевой
// не появляется. Rename в пределах одной ФС атомарен — читатель увидит
// либо старый файл, либо новый, но не частично записанный.
//
// Папка создаётся здесь же (MkdirAll), чтобы вызывающий не думал о ней.
func writeAtomic(absPath string, data []byte) error {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// buildLoginText собирает info.txt для запроса кода.
//
// now передаётся явно, а не берётся из time.Now: вызывающий код (и
// тесты) контролирует время, а формат даты в файле совпадает с тем,
// что используется в имени папки. Если бы здесь стоял time.Now, при
// подмене f.now в тестах дата в файле и в пути разъезжалась.
func buildLoginText(name, username, link string, now time.Time) string {
	var b strings.Builder
	b.WriteString("Причина: Запрос кода\n")
	b.WriteString("Дата: " + now.Format("2006-01-02 15:04:05") + "\n\n")
	b.WriteString("Telegram:\n")
	if name != "" {
		b.WriteString("  Имя: " + name + "\n")
	}
	if username != "" {
		b.WriteString("  Username: @" + username + "\n")
	} else {
		b.WriteString("  Username: (не задан)\n")
	}
	b.WriteString("  Ссылка: " + link + "\n")
	return b.String()
}

// buildSupportText собирает info.txt для обращения в поддержку.
// attached — имя вложения, если оно есть (для SaveSupportText — пусто).
//
// now передаётся явно по той же причине, что и в buildLoginText:
// вызывающий контролирует время, формат совпадает с тем, что в пути.
func buildSupportText(req domain.SupportRequest, attached string, now time.Time) string {
	var b strings.Builder
	b.WriteString("Причина: " + domain.SupportReasonTitle(req.Reason) + "\n")
	b.WriteString("Дата: " + now.Format("2006-01-02 15:04:05") + "\n\n")

	if req.StudentName != "" {
		b.WriteString("Студент:\n")
		b.WriteString("  Имя: " + req.StudentName + "\n")
		if req.GroupName != "" {
			b.WriteString("  Группа: " + req.GroupName + "\n")
		}
		if req.StreamName != "" {
			b.WriteString("  Поток: " + req.StreamName + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("Telegram:\n")
	if req.TelegramName != "" {
		b.WriteString("  Имя: " + req.TelegramName + "\n")
	}
	b.WriteString(fmt.Sprintf("  ID: %d\n", req.TelegramID))
	if req.Username != "" {
		b.WriteString("  Username: @" + req.Username + "\n")
	}
	b.WriteString("  Ссылка: " + req.Link + "\n\n")

	if req.Text != "" {
		b.WriteString("Текст обращения:\n")
		b.WriteString(req.Text + "\n")
	}
	if attached != "" {
		b.WriteString("Вложение: " + attached + "\n")
	}
	return b.String()
}
