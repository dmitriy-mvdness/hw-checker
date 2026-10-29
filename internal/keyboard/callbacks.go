// callbacks.go — единый источник callback-данных для inline-кнопок.
// Хендлеры и клавиатуры используют эти константы вместо литералов:
// опечатка в одной строке не отключит кнопку тихо.
//
// Точные маршруты (например, "main_menu") диспетчеризуются по map в
// Handler.callbackRoutes; префиксные (например, "subject:5") — по
// Handler.callbackPrefixRoutes через HasPrefix.
package keyboard

import "strconv"

// Точные callback-данные: полное совпадение с ключом callbackRoutes.
const (
	CbAuthMenu      = "auth_menu"
	CbUserAuth      = "user_auth"
	CbRequestCode   = "request_code"
	CbMainMenu      = "main_menu"
	CbProfile       = "profile"
	CbLogout        = "logout"
	CbLogoutConfirm = "logout_confirm"
	CbHomework      = "homework"
	CbSupport       = "support"
	CbSupportTech   = "support_tech"
	CbSupportHelp   = "support_help"
	CbMySubmissions = "my_submissions"
	CbNews          = "news"
	CbCancelResume  = "cancel_resume"

	CbIgnore = "ignore" // заглушка для кнопок, которые не должны ничего делать
)

// Префиксные callback-данные: <prefix><arg>. Handler.dispatch разбирает
// их через strings.HasPrefix и передаёт аргумент в обработчик.
const (
	PrefixSubject = "subject:"
	PrefixTheme   = "theme:"
	PrefixSupport = "support:"
	PrefixResume  = "resume:"
)

// BackCallbackPrefix — префикс callback-данных для кнопки «Назад».
// Обрабатывается в handleCallback: "back:<target>".
const BackCallbackPrefix = "back:"

// CallbackSubject — callback-данные кнопки предмета.
// Теперь включает страницу: "subject:<code>:<page>".
func CallbackSubject(code int64, page int) string {
	return PrefixSubject + strconv.FormatInt(code, 10) + ":" + strconv.Itoa(page)
}

// CallbackTheme — callback-данные кнопки темы.
func CallbackTheme(code string) string {
	return PrefixTheme + code
}

// CallbackSupportReason — callback-данные кнопки причины обращения.
func CallbackSupportReason(reason string) string {
	return PrefixSupport + reason
}

// CallbackResume — callback-данные «Продолжить сдачу».
func CallbackResume(themeCode string) string {
	return PrefixResume + themeCode
}
