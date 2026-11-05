package main

import (
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	api          *tgbotapi.BotAPI
	config       *AppConfig
	flightSearch *FlightSearch

	// searchMu не даёт запустить два поиска одновременно
	// (например, кнопкой и автоматическим поиском по расписанию).
	searchMu sync.Mutex
}

func NewBot(config *AppConfig, flightSearch *FlightSearch) (*Bot, error) {
	bot, err := tgbotapi.NewBotAPI(config.TelegramBotToken)
	if err != nil {
		return nil, err
	}

	return &Bot{
		api:          bot,
		config:       config,
		flightSearch: flightSearch,
	}, nil
}

func (b *Bot) Start() {
	log.Printf("Авторизован как %s", b.api.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		// Обрабатываем нажатия на inline-кнопки
		if update.CallbackQuery != nil {
			b.handleCallback(update.CallbackQuery)
			continue
		}

		if update.Message == nil {
			continue
		}

		// Проверяем права пользователя
		if !b.isUserAllowed(update.Message.From.ID) {
			log.Printf("Проверяем права пользователя %d", update.Message.From.ID)
			b.SendMessage(update.Message.Chat.ID, "❌ У вас нет прав для использования этого бота.")
			continue
		}

		// Обрабатываем команды
		switch update.Message.Command() {
		case "start":
			b.handleStart(update.Message)
		case "search", "find", "поиск":
			b.handleSearch(update.Message)
		case "status", "статус":
			b.handleStatus(update.Message)
		case "help", "помощь":
			b.handleHelp(update.Message)
		case "cities":
			b.handleCitiesList(update.Message)
		case "origin":
			b.handleOrigin(update.Message)
		case "setprice":
			b.handleSetPrice(update.Message)
		case "datefilter", "df":
			b.handleDateFilter(update.Message)
		default:
			b.handleUnknown(update.Message)
		}
	}
}

func (b *Bot) isUserAllowed(userID int64) bool {
	log.Printf("Запрос от %d", userID)

	// Доступ только явно указанным администраторам.
	// Пустой список не должен открывать бота всем (проверяется в AppConfig.Validate).
	return slices.Contains(b.config.AdminUsers, userID)
}

func (b *Bot) handleStart(message *tgbotapi.Message) {
	text := `👋 <b>Бот поиска дешёвых авиабилетов</b>

<b>Команды:</b>
/search - 🔍 Начать поиск билетов
/status - 📊 Статус бота
/help - ❓ Помощь

Поиск также можно запустить кнопкой ниже.`

	b.SendMessageWithKeyboard(message.Chat.ID, text, mainKeyboard())
}

func (b *Bot) handleSearch(message *tgbotapi.Message) {
	args := strings.Fields(message.Text)

	monthsToSearch := b.config.MonthsToSearch

	// Если указано направление как второй параметр
	if len(args) >= 2 {
		destination := strings.ToUpper(args[1])

		if len(args) >= 3 {
			var err error
			monthsToSearch, err = strconv.Atoi(args[2])
			if err != nil {
				b.SendMessage(message.Chat.ID,
					fmt.Sprintf("❌ Неверное количество месяцев: <code>%s</code>. Введите число от 1 до 12.", args[2]))
				return
			}
			if monthsToSearch < 1 || monthsToSearch > 12 {
				b.SendMessage(message.Chat.ID,
					"❌ Количество месяцев должно быть от 1 до 12.")
				return
			}
		}

		success := b.setDestinationByCityName(message.Chat.ID, destination, monthsToSearch)
		if !success {
			return
		}
	}

	// Запускаем поиск в фоне, чтобы бот продолжал реагировать на другие команды
	b.startSearch(message.Chat.ID)
}

// startSearch выполняет поиск в отдельной горутине.
// Одновременно может идти только один поиск.
func (b *Bot) startSearch(chatID int64) {
	go func() {
		if !b.searchMu.TryLock() {
			b.SendMessage(chatID, "⏳ <b>Поиск уже выполняется.</b> Подождите, пока он завершится.")
			return
		}
		defer b.searchMu.Unlock()

		b.SendMessage(chatID, "🔍 <b>Начинаю поиск билетов</b>")

		result, err := b.flightSearch.Search()
		if err != nil {
			b.SendMessage(chatID, fmt.Sprintf("❌ <b>Ошибка при поиске:</b>\n<code>%v</code>", err))
			return
		}

		log.Printf("Поиск завершён, длина результата: %d", len(result))
		b.SendMessageWithKeyboard(chatID, result, mainKeyboard())
	}()
}

// RunScheduledSearch запускает автоматический поиск и рассылает результат
// администраторам. Если поиск уже идёт (например, запущен кнопкой),
// задача пропускается, чтобы не дублировать запросы к API.
func (b *Bot) RunScheduledSearch() {
	if !b.searchMu.TryLock() {
		log.Printf("Поиск уже выполняется, пропускаем задачу по расписанию")
		return
	}
	defer b.searchMu.Unlock()

	result, err := b.flightSearch.Search()
	if err != nil {
		log.Printf("❌ Ошибка автоматического поиска: %v", err)
		return
	}

	for _, adminID := range b.config.AdminUsers {
		b.SendMessageWithKeyboard(adminID, result, mainKeyboard())
	}
}

func (b *Bot) handleStatus(message *tgbotapi.Message) {
	b.sendStatus(message.Chat.ID)
}

func (b *Bot) sendStatus(chatID int64) {
	text := fmt.Sprintf(`📊 <b>Статус бота</b>

<b>Направления поиска:</b>
• %s → %s

<b>Параметры:</b>
• Макс. цена: %d руб.
• Глубина поиска: %d месяцев
• Макс. время полёта: %d мин.
• Авто-поиск: каждый день в 10:00

%s

Бот работает в штатном режиме 🟢`,
		strings.Join(b.config.OriginIATA, "/"),
		b.config.DestinationIATA,
		b.config.MaxPrice,
		b.config.MonthsToSearch,
		b.config.MaxFlightTime,
		b.flightSearch.GetDateFilterInfo(),
	)

	b.SendMessageWithKeyboard(chatID, text, mainKeyboard())
}

func (b *Bot) handleHelp(message *tgbotapi.Message) {
	b.sendHelp(message.Chat.ID)
}

func (b *Bot) sendHelp(chatID int64) {
	text := `❓ <b>Помощь по боту</b>

<b>Поиск:</b>
/search - Запустить поиск билетов
/search бангкок - Поиск в конкретный город
/search бангкок 6 - Поиск на 6 месяцев

<b>Настройки:</b>
/setprice - Показать/изменить макс. цену
/origin - Управление городом вылета
/cities - Список доступных городов

<b>Фильтр дат:</b>
/datefilter - Показать текущий фильтр дат
/datefilter set 2027-06-01 2027-06-15 - Установить диапазон
/datefilter add 2027-06-15 - Добавить дату в список
/datefilter remove 2027-06-15 - Удалить дату из списка
/datefilter clear - Очистить фильтр дат
/datefilter disable - Отключить фильтр дат

<b>Информация:</b>
/status - Показать статус бота
/help - Эта справка

<b>Кнопки:</b>
Запускать поиск и открывать разделы можно кнопками под сообщением.

<b>Автоматический поиск:</b>
Бот автоматически ищет билеты каждый день в 10:00 и присылает уведомления.`

	b.SendMessageWithKeyboard(chatID, text, mainKeyboard())
}

func (b *Bot) handleOrigin(message *tgbotapi.Message) {
	args := strings.Fields(message.Text)

	if len(args) < 2 {
		b.SendMessage(message.Chat.ID,
			"❌ Укажите город вылета. Например: /origin москва")
		return
	}
	cityName := strings.Join(args[1:], " ")
	b.setOrigin(message.Chat.ID, cityName)
}

func (b *Bot) setDestination(chatID int64, destination string) {
	oldDestination := b.config.DestinationIATA
	b.flightSearch.SetDestination(destination)

	b.SendMessage(chatID,
		fmt.Sprintf("✅ <b>Направление изменено:</b>\n%s → %s\n➡️\n%s → %s",
			strings.Join(b.config.OriginIATA, "/"),
			getCityName(oldDestination),
			strings.Join(b.config.OriginIATA, "/"),
			getCityName(destination)))
}

func (b *Bot) handleUnknown(message *tgbotapi.Message) {
	text := "❓ Неизвестная команда. Используйте /help для просмотра доступных команд."
	b.SendMessage(message.Chat.ID, text)
}

func (b *Bot) handleSetPrice(message *tgbotapi.Message) {
	args := strings.Fields(message.Text)

	if len(args) < 2 {
		b.SendMessage(message.Chat.ID,
			fmt.Sprintf("💰 <b>Текущая макс. цена:</b> %d руб.\n\n"+
				"Используйте: /setprice 35000", b.config.MaxPrice))
		return
	}

	newPrice, err := strconv.Atoi(args[1])
	if err != nil {
		b.SendMessage(message.Chat.ID,
			fmt.Sprintf("❌ Неверное значение: <code>%s</code>. Введите число.", args[1]))
		return
	}

	if newPrice <= 0 {
		b.SendMessage(message.Chat.ID, "❌ Цена должна быть больше 0.")
		return
	}

	if newPrice > 1000000 {
		b.SendMessage(message.Chat.ID, "❌ Цена слишком большая. Максимум: 1 000 000.")
		return
	}

	oldPrice := b.config.MaxPrice
	b.flightSearch.SetMaxPrice(newPrice)

	b.SendMessage(message.Chat.ID,
		fmt.Sprintf("✅ <b>Макс. цена изменена:</b>\n%d₽ → %d₽", oldPrice, newPrice))
}

// Установка направления по названию города
func (b *Bot) setDestinationByCityName(chatID int64, cityName string, monthsToSearch int) bool {
	codes, foundCityName := FindAirportCode(cityName)

	if codes == nil {
		// 🆕 Город не найден, показываем подсказку
		b.SendMessage(chatID, fmt.Sprintf("❌ <b>Город '%s' не найден.</b>\n\n"+
			"💡 <i>Используйте:</i>\n"+
			"/search бангкок - поиск по названию\n"+
			"/search BKK - поиск по коду аэропорта\n"+
			"/cities - список доступных городов", cityName))
		return false
	}

	// 🆕 Если найдено несколько аэропортов, берем первый
	destination := codes[0]

	b.flightSearch.SetMonthsToSearch(monthsToSearch)
	oldDestination := b.config.DestinationIATA
	b.flightSearch.SetDestination(destination)

	var airportInfo string
	if len(codes) > 1 {
		airportInfo = fmt.Sprintf("\n🏢 Доступные аэропорты: %s", strings.Join(codes, ", "))
	}

	b.SendMessage(chatID, fmt.Sprintf("✅ <b>Направление изменено:</b>\n%s → %s\n➡️\n%s → %s%s",
		strings.Join(b.config.OriginIATA, "/"),
		getCityName(oldDestination),
		strings.Join(b.config.OriginIATA, "/"),
		foundCityName,
		airportInfo))
	return true
}

// Команда для списка городов (заменяет /destinations)
func (b *Bot) handleCitiesList(message *tgbotapi.Message) {
	b.sendCitiesList(message.Chat.ID)
}

func (b *Bot) sendCitiesList(chatID int64) {
	text := "🏙️ <b>Доступные города для поиска:</b>\n\n"
	text += GetCityList()
	text += "\n\n💡 <i>Используйте команду /search ГОРОД для поиска</i>\n"
	text += "Например:\n"
	text += "/search бангкок - поиск по названию\n"
	text += "/search BKK - поиск по коду аэропорта\n"
	text += "/search - поиск в текущее направление"
	b.SendMessage(chatID, text)
}

func (b *Bot) setOrigin(chatID int64, cityName string) bool {
	codes, _ := FindOriginAirportCode(cityName)

	if codes == nil {
		b.SendMessage(chatID, fmt.Sprintf("❌ <b>Город вылета '%s' не найден.</b>\n\n"+
			"💡 <i>Используйте:</i>\n"+"/origin москва - установить Москву", cityName))
		return false
	}
	origin := codes[0]
	oldOrigins := make([]string, len(b.config.OriginIATA))
	copy(oldOrigins, b.config.OriginIATA)
	b.flightSearch.SetOriginIATA(origin)

	var originInfo string
	if len(codes) > 1 {
		originInfo = fmt.Sprintf("\n🏢 Доступные аэропорты: %s", strings.Join(codes, ", "))
	}
	b.SendMessage(chatID, fmt.Sprintf("✅ <b>Город вылета изменен:</b>\n%s → %s\n➡️\n%s → %s%s",
		strings.Join(oldOrigins, "/"),
		getCityName(b.config.DestinationIATA),
		origin,
		getCityName(b.config.DestinationIATA),
		originInfo))
	return true
}

func FindOriginAirportCode(cityName string) ([]string, string) {
	normalized := strings.ToLower(strings.TrimSpace(cityName))

	// Прямой поиск test
	if codes, exists := CityAirports[normalized]; exists {
		return codes, getCityName(codes[0])
	}
	return nil, ""

}

func (b *Bot) handleDateFilter(message *tgbotapi.Message) {
	args := strings.Fields(message.Text)

	if len(args) < 2 {
		b.sendDateFilterStatus(message.Chat.ID)
		return
	}

	subcommand := strings.ToLower(args[1])

	switch subcommand {
	case "set", "диапазон":
		b.handleDateFilterSet(message, args)
	case "add", "добавить":
		b.handleDateFilterAdd(message, args)
	case "remove", "удалить":
		b.handleDateFilterRemove(message, args)
	case "clear", "очистить":
		b.handleDateFilterClear(message)
	case "disable", "отключить":
		b.handleDateFilterDisable(message)
	case "list", "список":
		b.sendDateFilterStatus(message.Chat.ID)
	default:
		b.SendMessage(message.Chat.ID, "❓ Неизвестная подкоманда. Используйте:\n"+
			"/datefilter - показать текущий фильтр\n"+
			"/datefilter set 2027-06-01 2027-06-15 - установить диапазон\n"+
			"/datefilter add 2027-06-01 - добавить дату в список\n"+
			"/datefilter remove 2027-06-01 - удалить дату из списка\n"+
			"/datefilter clear - очистить фильтр\n"+
			"/datefilter disable - отключить фильтр")
	}
}

func (b *Bot) handleDateFilterSet(message *tgbotapi.Message, args []string) {
	if len(args) < 4 {
		b.SendMessage(message.Chat.ID, "📅 <b>Установка диапазона дат</b>\n\n"+
			"Пример: /datefilter set 2027-06-01 2027-06-15")
		return
	}

	startDate, err1 := time.Parse("2006-01-02", args[2])
	endDate, err2 := time.Parse("2006-01-02", args[3])

	if err1 != nil || err2 != nil {
		b.SendMessage(message.Chat.ID, "❌ <b>Неверный формат дат.</b>\n\n"+
			"Пример: /datefilter set 2027-06-01 2027-06-15")
		return
	}

	if endDate.Before(startDate) {
		b.SendMessage(message.Chat.ID, "❌ <b>Дата окончания раньше даты начала.</b>")
		return
	}

	b.flightSearch.SetDateRange(startDate, endDate)

	b.SendMessage(message.Chat.ID, fmt.Sprintf("✅ <b>Диапазон дат установлен:</b>\n📅 с <code>%s</code> по <code>%s</code>",
		startDate.Format("02.01.2006"),
		endDate.Format("02.01.2006")))
}

func (b *Bot) handleDateFilterAdd(message *tgbotapi.Message, args []string) {
	if len(args) < 3 {
		b.SendMessage(message.Chat.ID, "📋 <b>Добавление даты в список</b>\n\n"+
			"Пример: /datefilter add 2027-06-15")
		return
	}

	dateStr := args[2]
	_, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		b.SendMessage(message.Chat.ID, "❌ <b>Неверный формат даты.</b>\n\n"+
			"Пример: /datefilter add 2027-06-15")
		return
	}

	b.flightSearch.AddDateToDate(dateStr)

	t, _ := time.Parse("2006-01-02", dateStr)
	b.SendMessage(message.Chat.ID, fmt.Sprintf("✅ <b>Дата добавлена:</b> <code>%s</code> (%s)", dateStr, getRussianDayOfWeek(t.Weekday())))
}

func (b *Bot) handleDateFilterRemove(message *tgbotapi.Message, args []string) {
	if len(args) < 3 {
		b.SendMessage(message.Chat.ID, "🗑️ <b>Удаление даты из списка</b>\n\n"+
			"Пример: /datefilter remove 2027-06-15")
		return
	}

	dateStr := args[2]
	success := b.flightSearch.RemoveDateFromDate(dateStr)

	if !success {
		b.SendMessage(message.Chat.ID, fmt.Sprintf("❌ <b>Дата <code>%s</code> не найдена в списке.</b>", dateStr))
		return
	}

	b.SendMessage(message.Chat.ID, fmt.Sprintf("🗑️ <b>Дата удалена:</b> <code>%s</code>", dateStr))
}

func (b *Bot) handleDateFilterClear(message *tgbotapi.Message) {
	b.flightSearch.ClearDateFilter()
	b.SendMessage(message.Chat.ID, "🧹 <b>Фильтр дат очищен.</b>\nТеперь поиск идёт по всем датам.")
}

func (b *Bot) handleDateFilterDisable(message *tgbotapi.Message) {
	b.flightSearch.DisableDateFilter()
	b.SendMessage(message.Chat.ID, "⏹️ <b>Фильтр дат отключён.</b>\nТеперь поиск идёт по всем датам.")
}

func (b *Bot) sendDateFilterStatus(chatID int64) {
	info := b.flightSearch.GetDateFilterInfo()
	b.SendMessageWithKeyboard(chatID, info, mainKeyboard())
}

// handleCallback обрабатывает нажатия на inline-кнопки.
func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	if query.From == nil || !b.isUserAllowed(query.From.ID) {
		b.answerCallback(query.ID, "❌ У вас нет прав для использования этого бота.", true)
		return
	}

	chatID := query.From.ID
	if query.Message != nil {
		chatID = query.Message.Chat.ID
	}

	if query.Data == "search" {
		// Отвечаем сразу, чтобы у пользователя не крутился индикатор загрузки
		b.answerCallback(query.ID, "🔍 Запускаю поиск…", false)
	} else {
		b.answerCallback(query.ID, "", false)
	}

	switch query.Data {
	case "search":
		b.startSearch(chatID)
	case "status":
		b.sendStatus(chatID)
	case "help":
		b.sendHelp(chatID)
	case "cities":
		b.sendCitiesList(chatID)
	case "datefilter":
		b.sendDateFilterStatus(chatID)
	default:
		b.SendMessage(chatID, "❓ Неизвестная кнопка.")
	}
}

// answerCallback отвечает на callback-запрос Telegram.
// alert=true показывает всплывающее окно вместо короткого уведомления.
func (b *Bot) answerCallback(callbackID, text string, alert bool) {
	var cfg tgbotapi.CallbackConfig
	if alert {
		cfg = tgbotapi.NewCallbackWithAlert(callbackID, text)
	} else {
		cfg = tgbotapi.NewCallback(callbackID, text)
	}

	if _, err := b.api.Request(cfg); err != nil {
		log.Printf("Ошибка ответа на callback: %v", err)
	}
}

// mainKeyboard — основные действия бота.
func mainKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔍 Начать поиск", "search"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Статус", "status"),
			tgbotapi.NewInlineKeyboardButtonData("📅 Фильтр дат", "datefilter"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🏙️ Города", "cities"),
			tgbotapi.NewInlineKeyboardButtonData("❓ Помощь", "help"),
		),
	)
}

func (b *Bot) SendMessage(chatID int64, text string) {
	b.sendMessage(chatID, text, nil)
}

func (b *Bot) SendMessageWithKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	b.sendMessage(chatID, text, &keyboard)
}

func (b *Bot) sendMessage(chatID int64, text string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}
	_, errSend := b.api.Send(msg)
	if errSend != nil {
		log.Printf("Ошибка отправки %v", errSend)

		// Фолбэк: попробовать без ParseMode
		msg.ParseMode = ""
		if _, err2 := b.api.Send(msg); err2 != nil {
			log.Printf("Фолбэк тоже не удался: %v", err2)
		}
	}
}
