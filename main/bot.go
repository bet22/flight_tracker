package main

import (
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	api          *tgbotapi.BotAPI
	config       *AppConfig
	flightSearch *FlightSearch
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
		if update.Message == nil {
			continue
		}

		// Проверяем права пользователя
		if !b.isUserAllowed(update.Message.From.ID) {
			log.Printf("Проверяем права пользователя %d", update.Message.From.ID)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "❌ У вас нет прав для использования этого бота.")
			b.api.Send(msg)
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
	// Если не указаны администраторы, разрешаем всем
	log.Printf("Запрос от %d", userID)
	if len(b.config.AdminUsers) == 0 {
		return true
	}

	// Проверяем есть ли пользователь в списке администраторов
	return slices.Contains(b.config.AdminUsers, userID)
}

func (b *Bot) handleStart(message *tgbotapi.Message) {
	text := `👋 <b>Бот поиска дешёвых авиабилетов</b>

<b>Команды:</b>
/search - 🔍 Начать поиск билетов
/status - 📊 Статус бота
/help - ❓ Помощь

<b>Направления:</b>
• Новосибирск/Барнаул → Денпасар (Бали)
• Макс. цена: 35,000 руб.
• Поиск на 6 месяцев вперёд`

	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
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
				msg := tgbotapi.NewMessage(message.Chat.ID,
					fmt.Sprintf("❌ Неверное количество месяцев: <code>%s</code>. Введите число от 1 до 12.", args[2]))
				msg.ParseMode = "HTML"
				b.api.Send(msg)
				return
			}
			if monthsToSearch < 1 || monthsToSearch > 12 {
				msg := tgbotapi.NewMessage(message.Chat.ID,
					"❌ Количество месяцев должно быть от 1 до 12.")
				msg.ParseMode = "HTML"
				b.api.Send(msg)
				return
			}
		}

		success := b.setDestinationByCityName(message.Chat.ID, destination, monthsToSearch)
		if !success {
			return
		}
	}

	// Отправляем сообщение о начале поиска
	msg := tgbotapi.NewMessage(message.Chat.ID, "🔍 <b>Начинаю поиск билетов...</b>\nЭто займет несколько секунд.")
	msg.ParseMode = "HTML"
	b.api.Send(msg)

	// Выполняем поиск
	result, err := b.flightSearch.Search()
	if err != nil {
		errorMsg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("❌ <b>Ошибка при поиске:</b>\n<code>%v</code>", err))
		errorMsg.ParseMode = "HTML"
		b.api.Send(errorMsg)
		return
	}

	// Отправляем результат
	response := tgbotapi.NewMessage(message.Chat.ID, result)
	response.ParseMode = "HTML"
	response.DisableWebPagePreview = true
	b.api.Send(response)
}

func (b *Bot) handleStatus(message *tgbotapi.Message) {
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

	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) handleHelp(message *tgbotapi.Message) {
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
/datefilter set 2024-06-01 2024-06-15 - Установить диапазон
/datefilter add 2024-06-15 - Добавить дату в список
/datefilter remove 2024-06-15 - Удалить дату из списка
/datefilter clear - Очистить фильтр дат
/datefilter disable - Отключить фильтр дат

<b>Информация:</b>
/status - Показать статус бота
/help - Эта справка

<b>Автоматический поиск:</b>
Бот автоматически ищет билеты каждый день в 10:00 и присылает уведомления.`

	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) handleOrigin(message *tgbotapi.Message) {
	args := strings.Fields(message.Text)

	if len(args) >= 2 {
		msg := tgbotapi.NewMessage(message.Chat.ID,
			"❌ Укажите город вылета. Например: <code>/origin set москва</code>")
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}
	cityName := strings.Join(args[2:], " ")
	b.setOrigin(message.Chat.ID, cityName)
}

func (b *Bot) setDestination(chatID int64, destination string) {
	oldDestination := b.config.DestinationIATA
	b.flightSearch.SetDestination(destination)

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ <b>Направление изменено:</b>\n%s → %s\n➡️\n%s → %s",
			strings.Join(b.config.OriginIATA, "/"),
			getCityName(oldDestination),
			strings.Join(b.config.OriginIATA, "/"),
			getCityName(destination)))
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) handleUnknown(message *tgbotapi.Message) {
	text := "❓ Неизвестная команда. Используйте /help для просмотра доступных команд."
	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	b.api.Send(msg)
}

// handleSetPrice обрабатывает команду /setprice <цена>
func (b *Bot) handleSetPrice(message *tgbotapi.Message) {
	args := strings.Fields(message.Text)

	if len(args) < 2 {
		msg := tgbotapi.NewMessage(message.Chat.ID,
			fmt.Sprintf("💰 <b>Текущая макс. цена:</b> %d руб.\n\n"+
				"Используйте: <code>/setprice 35000</code>", b.config.MaxPrice))
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}

	newPrice, err := strconv.Atoi(args[1])
	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID,
			fmt.Sprintf("❌ Неверное значение: <code>%s</code>. Введите число.", args[1]))
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}

	if newPrice <= 0 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Цена должна быть больше 0.")
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}

	if newPrice > 1000000 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ Цена слишком большая. Максимум: 1 000 000.")
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}

	oldPrice := b.config.MaxPrice
	b.flightSearch.SetMaxPrice(newPrice)

	msg := tgbotapi.NewMessage(message.Chat.ID,
		fmt.Sprintf("✅ <b>Макс. цена изменена:</b>\n%d₽ → %d₽", oldPrice, newPrice))
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

// SendMessage отправляет сообщение в указанный чат
func (b *Bot) SendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	msg.DisableWebPagePreview = true
	msg.DisableNotification = true
	b.api.Send(msg)
}

// Установка направления по названию города
func (b *Bot) setDestinationByCityName(chatID int64, cityName string, monthsToSearch int) bool {
	codes, foundCityName := FindAirportCode(cityName)

	if codes == nil {
		// 🆕 Город не найден, показываем подсказку
		msg := tgbotapi.NewMessage(chatID,
			fmt.Sprintf("❌ <b>Город '%s' не найден.</b>\n\n"+
				"💡 <i>Используйте:</i>\n"+
				"<code>/search бангкок</code> - поиск по названию\n"+
				"<code>/search BKK</code> - поиск по коду аэропорта\n"+
				"<code>/cities</code> - список доступных городов", cityName))
		msg.ParseMode = "HTML"
		b.api.Send(msg)
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

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ <b>Направление изменено:</b>\n%s → %s\n➡️\n%s → %s%s",
			strings.Join(b.config.OriginIATA, "/"),
			getCityName(oldDestination),
			strings.Join(b.config.OriginIATA, "/"),
			foundCityName,
			airportInfo))
	msg.ParseMode = "HTML"
	b.api.Send(msg)
	return true
}

// Команда для списка городов (заменяет /destinations)
func (b *Bot) handleCitiesList(message *tgbotapi.Message) {
	text := "🏙️ <b>Доступные города для поиска:</b>\n\n"
	text += GetCityList()
	text += "\n\n💡 <i>Используйте команду /search ГОРОД для поиска</i>\n"
	text += "Например:\n"
	text += "<code>/search бангкок</code> - поиск по названию\n"
	text += "<code>/search BKK</code> - поиск по коду аэропорта\n"
	text += "<code>/search сидней</code> - поиск в Сидней\n"
	text += "<code>/search</code> - поиск в текущее направление"

	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

// 🆕 ДОБАВЛЕНО: справка по команде origin
func (b *Bot) setOrigin(chatID int64, cityName string) bool {
	codes, _ := FindOriginAirportCode(cityName)

	if codes == nil {
		msg := tgbotapi.NewMessage(chatID,
			fmt.Sprintf("❌ <b>Город вылета '%s' не найден.</b>\n\n"+
				"💡 <i>Используйте:</i>\n"+
				"<code>/origin list</code> - список доступных городов\n"+
				"<code>/origin set москва</code> - установить Москву", cityName))
		msg.ParseMode = "HTML"
		b.api.Send(msg)
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
	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ <b>Город вылета изменен:</b>\n%s → %s\n➡️\n%s → %s%s",
			strings.Join(oldOrigins, "/"),
			getCityName(b.config.DestinationIATA),
			origin,
			getCityName(b.config.DestinationIATA),
			originInfo))
	msg.ParseMode = "HTML"
	b.api.Send(msg)
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

// handleDateFilter обрабатывает команду /datefilter и его подкоманды
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
		msg := tgbotapi.NewMessage(message.Chat.ID,
			"❓ Неизвестная подкоманда. Используйте:\n"+
				"<code>/datefilter</code> - показать текущий фильтр\n"+
				"<code>/datefilter set 2024-06-01 2024-06-15</code> - установить диапазон\n"+
				"<code>/datefilter add 2024-06-01</code> - добавить дату в список\n"+
				"<code>/datefilter remove 2024-06-01</code> - удалить дату из списка\n"+
				"<code>/datefilter clear</code> - очистить фильтр\n"+
				"<code>/datefilter disable</code> - отключить фильтр")
		msg.ParseMode = "HTML"
		b.api.Send(msg)
	}
}

func (b *Bot) handleDateFilterSet(message *tgbotapi.Message, args []string) {
	if len(args) < 4 {
		msg := tgbotapi.NewMessage(message.Chat.ID,
			"📅 <b>Установка диапазона дат</b>\n\n"+
				"Используйте: <code>/datefilter set ГГГГ-ММ-ДД ГГГГ-ММ-ДД</code>\n\n"+
				"Пример: <code>/datefilter set 2024-06-01 2024-06-15</code>")
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}

	startDate, err1 := time.Parse("2006-01-02", args[2])
	endDate, err2 := time.Parse("2006-01-02", args[3])

	if err1 != nil || err2 != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID,
			"❌ <b>Неверный формат дат.</b>\n\n"+
				"Используйте формат <code>ГГГГ-ММ-ДД</code>\n\n"+
				"Пример: <code>/datefilter set 2024-06-01 2024-06-15</code>")
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}

	if endDate.Before(startDate) {
		msg := tgbotapi.NewMessage(message.Chat.ID,
			"❌ <b>Дата окончания раньше даты начала.</b>")
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}

	b.flightSearch.SetDateRange(startDate, endDate)

	msg := tgbotapi.NewMessage(message.Chat.ID,
		fmt.Sprintf("✅ <b>Диапазон дат установлен:</b>\n📅 с <code>%s</code> по <code>%s</code>",
			startDate.Format("02.01.2006"),
			endDate.Format("02.01.2006")))
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) handleDateFilterAdd(message *tgbotapi.Message, args []string) {
	if len(args) < 3 {
		msg := tgbotapi.NewMessage(message.Chat.ID,
			"📋 <b>Добавление даты в список</b>\n\n"+
				"Используйте: <code>/datefilter add ГГГГ-ММ-ДД</code>\n\n"+
				"Пример: <code>/datefilter add 2024-06-15</code>")
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}

	dateStr := args[2]
	_, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID,
			"❌ <b>Неверный формат даты.</b>\n\n"+
				"Используйте формат <code>ГГГГ-ММ-ДД</code>\n\n"+
				"Пример: <code>/datefilter add 2024-06-15</code>")
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}

	b.flightSearch.AddDateToDate(dateStr)

	t, _ := time.Parse("2006-01-02", dateStr)
	msg := tgbotapi.NewMessage(message.Chat.ID,
		fmt.Sprintf("✅ <b>Дата добавлена:</b> <code>%s</code> (%s)",
			dateStr, getRussianDayOfWeek(t.Weekday())))
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) handleDateFilterRemove(message *tgbotapi.Message, args []string) {
	if len(args) < 3 {
		msg := tgbotapi.NewMessage(message.Chat.ID,
			"🗑️ <b>Удаление даты из списка</b>\n\n"+
				"Используйте: <code>/datefilter remove ГГГГ-ММ-ДД</code>\n\n"+
				"Пример: <code>/datefilter remove 2024-06-15</code>")
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}

	dateStr := args[2]
	success := b.flightSearch.RemoveDateFromDate(dateStr)

	if !success {
		msg := tgbotapi.NewMessage(message.Chat.ID,
			fmt.Sprintf("❌ <b>Дата <code>%s</code> не найдена в списке.</b>", dateStr))
		msg.ParseMode = "HTML"
		b.api.Send(msg)
		return
	}

	msg := tgbotapi.NewMessage(message.Chat.ID,
		fmt.Sprintf("🗑️ <b>Дата удалена:</b> <code>%s</code>", dateStr))
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) handleDateFilterClear(message *tgbotapi.Message) {
	b.flightSearch.ClearDateFilter()

	msg := tgbotapi.NewMessage(message.Chat.ID,
		"🧹 <b>Фильтр дат очищен.</b>\nТеперь поиск идёт по всем датам.")
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) handleDateFilterDisable(message *tgbotapi.Message) {
	b.flightSearch.DisableDateFilter()

	msg := tgbotapi.NewMessage(message.Chat.ID,
		"⏹️ <b>Фильтр дат отключён.</b>\nТеперь поиск идёт по всем датам.")
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) sendDateFilterStatus(chatID int64) {
	info := b.flightSearch.GetDateFilterInfo()
	msg := tgbotapi.NewMessage(chatID, info)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}
