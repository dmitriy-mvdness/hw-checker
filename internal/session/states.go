// Package session хранит состояние пользователей в памяти: кто сейчас
// вводит персональный код, кто ждёт файл для сдачи ДЗ. Состояние живёт
// только в памяти: после перезапуска бота режимы сбрасываются, а аккаунт
// лежит в storage.db, файлы — на диске.
package session

import (
	"sync"
	"time"
)

// fileWaitTTL — сколько режим ожидания файла считается активным. После
// этого срока состояние считается протухшим: юзер кликнул тему, отвлёкся,
// вернулся — и не должен натыкаться на «пришли файл» посреди другого
// сценария. Отсчёт с момента клика по теме.
//
// TTL применяется только к StateWaitingFile. Для StateWaitingCode и
// StateWaitingSupport он был бы регрессией: юзер может вернуться к
// вводу кода или к написанию обращения через час, и состояние должно
// его дождаться.
const fileWaitTTL = 30 * time.Minute

// lastThemeTTL — сколько помним «последнюю открытую тему». Если юзер
// кликнул тему, ушёл, WaitingFile протух, а потом прислал файл вне
// контекста — предложим вернуться к этой теме. Через 6 часов это уже
// «другая сессия», напоминать странно.
const lastThemeTTL = 10 * time.Hour

// State — режим, в котором находится пользователь.
type State int

// Режимы пользователя. Порядок важен для iota: StateNone — нулевое
// значение, поэтому отсутствие записи трактуется как «обычный режим».
const (
	StateNone State = iota
	StateWaitingCode
	StateWaitingFile
	StateWaitingSupport
)

// Session описывает текущее состояние пользователя.
type Session struct {
	State State // режим, в котором пользователь

	// MessageID — сообщение с экраном, которое бот редактирует, чтобы
	// показать результат (например, «код неверный» или «файл принят»).
	MessageID int

	ThemeCode     string // код темы (только StateWaitingFile)
	SupportReason string // код причины (только StateWaitingSupport)

	// updatedAt — момент последней установки состояния. Используется
	// только для TTL режима ожидания файла: если сессия ждёт файл
	// дольше fileWaitTTL, она считается протухшей и не отдаётся наружу.
	updatedAt time.Time
}

// lastThemeEntry — «последняя открытая тема» пользователя. Нужна, чтобы
// при файле вне контекста предложить вернуться к сдаче. Не путать с
// Session: Session — активный режим, lastTheme — память о намерении.
type lastThemeEntry struct {
	code  string
	setAt time.Time

	// msgID — сообщение с экраном темы. Когда WaitingFile протухает,
	// мы редактируем именно это сообщение в «Продолжить сдачу?», а не
	// шлём новое — чтобы в чате не копились экраны.
	msgID int

	// hintShown — true, если экран уже превращён в «Продолжить сдачу?».
	// Защита от повторного редактирования при серии файлов подряд.
	// Сбрасывается в false при RememberTheme — например, когда юзер
	// нажал «Продолжить» и цикл может повториться.
	hintShown bool
}

// States — потокобезопасное хранилище состояний по ID пользователя.
// Telegram-апдейты обрабатываются параллельно, поэтому нужен RWMutex.
type States struct {
	mu         sync.RWMutex
	m          map[int64]Session
	lastThemes map[int64]lastThemeEntry

	// now подменяется в тестах, чтобы не ждать реальные 30 минут.
	now func() time.Time
}

// NewStates создаёт пустое хранилище состояний.
func NewStates() *States {
	return &States{
		m:          make(map[int64]Session),
		lastThemes: make(map[int64]lastThemeEntry),
		now:        time.Now,
	}
}

// newStatesWithClockFn создаёт States с подменяемыми часами. Нужен
// только тестам: в продакшене время идёт через time.Now.
func newStatesWithClockFn(now func() time.Time) *States {
	return &States{
		m:          make(map[int64]Session),
		lastThemes: make(map[int64]lastThemeEntry),
		now:        now,
	}
}

// Get возвращает сессию пользователя. Если записи нет — нулевое значение.
// Если сессия ждёт файл и протухла — тоже нулевое: вызывающий код
// (IsWaitingFile, TakeFile) трактует это как «юзер не в режиме».
//
// Запись при этом не удаляется — она перезапишется при следующем Wait*
// либо будет удалена Clear/Take*. Ленивая чистка: не хотим брать Lock
// на горячем пути Get ради удаления одной записи.
func (s *States) Get(userID int64) Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess := s.m[userID]
	if sess.State == StateWaitingFile && s.now().Sub(sess.updatedAt) > fileWaitTTL {
		return Session{}
	}
	return sess
}

// IsWaitingCode сообщает, ждёт ли бот код от этого пользователя.
func (s *States) IsWaitingCode(userID int64) bool {
	return s.Get(userID).State == StateWaitingCode
}

// IsWaitingFile сообщает, ждёт ли бот файл от этого пользователя.
func (s *States) IsWaitingFile(userID int64) bool {
	return s.Get(userID).State == StateWaitingFile
}

// IsWaitingSupport сообщает, ждёт ли бот обращение в поддержку.
func (s *States) IsWaitingSupport(userID int64) bool {
	return s.Get(userID).State == StateWaitingSupport
}

// WaitCode переводит пользователя в режим ввода кода.
func (s *States) WaitCode(userID int64, messageID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[userID] = Session{
		State:     StateWaitingCode,
		MessageID: messageID,
		updatedAt: s.now(),
	}
}

// WaitFile переводит пользователя в режим ожидания файла по теме.
func (s *States) WaitFile(userID int64, themeCode string, messageID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[userID] = Session{
		State:     StateWaitingFile,
		MessageID: messageID,
		ThemeCode: themeCode,
		updatedAt: s.now(),
	}
}

// WaitSupport переводит пользователя в режим ожидания текста/файла
// по выбранной причине обращения.
func (s *States) WaitSupport(userID int64, reason string, messageID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[userID] = Session{
		State:         StateWaitingSupport,
		MessageID:     messageID,
		SupportReason: reason,
		updatedAt:     s.now(),
	}
}

// TakeFile атомарно забирает сессию, если пользователь ждёт файл.
// Возвращает (Session, true) только одному вызову; остальные получают
// (Session{}, false). Нужно, чтобы альбом из нескольких файлов не
// расходовал попытки: первый апдейт забирает сессию, остальные — нет.
//
// Протухшую сессию не отдаём и стираем — иначе файл, присланный после
// TTL, обработается как валидная сдача на давно забытую тему. lastTheme
// при этом НЕ трогаем: именно в этот момент подсказка «продолжить
// сдачу?» становится актуальной.
func (s *States) TakeFile(userID int64) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.m[userID]
	if !ok || sess.State != StateWaitingFile {
		return Session{}, false
	}

	if s.now().Sub(sess.updatedAt) > fileWaitTTL {
		delete(s.m, userID)
		return Session{}, false
	}

	delete(s.m, userID)
	return sess, true
}

// TakeSupport атомарно забирает сессию поддержки. Возвращает true
// только одному вызову — защита от повторной обработки.
func (s *States) TakeSupport(userID int64) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.m[userID]
	if !ok || sess.State != StateWaitingSupport {
		return Session{}, false
	}
	delete(s.m, userID)
	return sess, true
}

// Clear выводит пользователя из любого режима (кнопка «Назад»,
// успешный вход, смена экрана). Также забывает «последнюю открытую
// тему»: юзер сам вышел из контекста, напоминать не о чем.
func (s *States) Clear(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, userID)
	delete(s.lastThemes, userID)
}

// RememberTheme запоминает тему, которую юзер только что открыл.
// Вызывается из cbTheme вместе с WaitFile. При повторном клике по
// другой теме — перезаписывается, hintShown сбрасывается в false
// (экран снова показывает тему, а не подсказку).
func (s *States) RememberTheme(userID int64, code string, msgID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastThemes[userID] = lastThemeEntry{
		code:  code,
		setAt: s.now(),
		msgID: msgID,
	}
}

// LastTheme возвращает последнюю открытую тему, если она свежая.
// Второе значение — ID сообщения с экраном темы (для редактирования
// в подсказку). Третье — показана ли уже подсказка. Четвёртое — ok:
// false, если темы нет или она старше lastThemeTTL.
func (s *States) LastTheme(userID int64) (code string, msgID int, hintShown bool, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, found := s.lastThemes[userID]
	if !found {
		return "", 0, false, false
	}
	if s.now().Sub(e.setAt) > lastThemeTTL {
		return "", 0, false, false
	}
	return e.code, e.msgID, e.hintShown, true
}

// MarkThemeHintShown помечает, что экран темы превращён в подсказку
// «Продолжить сдачу?». Защита от повторной обработки при серии файлов
// подряд: второй файл уже не редактирует сообщение, а просто удаляется.
func (s *States) MarkThemeHintShown(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.lastThemes[userID]
	if !ok {
		return
	}
	e.hintShown = true
	s.lastThemes[userID] = e
}

// ClearLastTheme забывает «последнюю открытую тему». Вызывается при
// явном отказе юзера (кнопка «Отмена») и после успешной сдачи.
func (s *States) ClearLastTheme(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.lastThemes, userID)
}
