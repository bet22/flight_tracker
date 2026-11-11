package main

import (
	"fmt"
	"log"
	"slices"
	"sort"
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
	store        *SettingsStore

	// searchLocks — по одному мьютексу на пользователя,
	// чтобы поиски разных людей не блокировали друг друга.
	locksMu     sync.Mutex
	searchLocks map[int64]*sync.Mutex

	// lastScheduled защищает от повторного запуска автопоиска в одну и ту же минуту.
	scheduleMu    sync.Mutex
	lastScheduled map[int64]string
}

func NewBot(config *AppConfig, flightSearch *FlightSearch, store *SettingsStore) (*Bot, error) {
	bot, err := tgbotapi.NewBotAPI(config.TelegramBotToken)
	if err != nil {
		return nil, err
	}

	return &Bot{
		api:           bot,
		config:        config,
		flightSearch:  flightSearch,
		store:         store,
		searchLocks:   make(map[int64]*sync.Mutex),
		lastScheduled: make(map[int64]string),
	}, nil
}

// settingsFor возвращает настройки пользователя, создавая их при необходимости.
func (b *Bot) settingsFor(userID int64) UserSettings {
	b.store.EnsureUser(userID)
	return b.store.Get(userID)
}

// userSearchLock возвращает мьютекс поиска конкретного пользователя.
func (b *Bot) userSearchLock(userID int64) *sync.Mutex {
	b.locksMu.Lock()
	defer b.locksMu.Unlock()

	lock, ok := b.searchLocks[userID]
	if !ok {
		lock = &sync.Mutex{}
		b.searchLocks[userID] = lock
	}
	return lock
}

// updateSettings изменяет настройки пользователя, сохраняет их и сообщает об ошибке.
func (b *Bot) updateSettings(userID, chatID int64, fn func(*UserSettings) error) bool {
	if err := b.store.Update(userID, fn); err != nil {
		log.Printf("Не удалось сохранить настройки пользователя %d: %v", userID, err)
		b.SendMessage(chatID, "❌ <b>Не удалось сохранить настройки.</b>\nПопробуйте ещё раз.")
		return false
	}
	return true
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

		if update.Message == nil || update.Message.From == nil {
			continue
		}

		// Проверяем права пользователя
		if !b.isUserAllowed(update.Message.From.ID) {
			log.Printf("Проверяем права пользователя %d", update.Message.From.ID)
			b.SendMessage(update.Message.Chat.ID, "❌ У вас нет прав для использования этого бота.")
			continue
		}

		// Создаём персональные настройки при первом обращении
		b.store.EnsureUser(update.Message.From.ID)

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
		case "schedule", "расписание":
			b.handleSchedule(update.Message)
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
/status - 📊 Мои настройки
/schedule - ⏰ Время автопоиска
/help - ❓ Помощь

Настройки у каждого пользователя свои и сохраняются автоматически.
Поиск также можно запустить кнопкой ниже.`

	b.SendMessageWithKeyboard(message.Chat.ID, text, mainKeyboard())
}

func (b *Bot) handleSearch(message *tgbotapi.Message) {
	args := strings.Fields(message.Text)
	userID := message.From.ID
	chatID := message.Chat.ID

	settings := b.settingsFor(userID)
	monthsToSearch := settings.MonthsToSearch

	// Если указано направление как второй параметр
	if len(args) >= 2 {
		destination := strings.ToUpper(args[1])

		if len(args) >= 3 {
			var err error
			monthsToSearch, err = strconv.Atoi(args[2])
			if err != nil {
				b.SendMessage(chatID,
					fmt.Sprintf("❌ Неверное количество месяцев: <code>%s</code>. Введите число от 1 до 12.", args[2]))
				return
			}
			if monthsToSearch < 1 || monthsToSearch > 12 {
				b.SendMessage(chatID,
					"❌ Количество месяцев должно быть от 1 до 12.")
				return
			}
		}

		success := b.setDestinationByCityName(userID, chatID, destination, monthsToSearch)
		if !success {
			return
		}
	}

	// Запускаем поиск в фоне, чтобы бот продолжал реагировать на другие команды
	b.startSearch(userID, chatID)
}

// startSearch выполняет поиск в отдельной горутине.
// Один пользователь не может запустить два поиска одновременно.
func (b *Bot) startSearch(userID, chatID int64) {
	go func() {
		lock := b.userSearchLock(userID)
		if !lock.TryLock() {
			b.SendMessage(chatID, "⏳ <b>Поиск уже выполняется.</b> Подождите, пока он завершится.")
			return
		}
		defer lock.Unlock()

		settings := b.settingsFor(userID)
		b.SendMessage(chatID, "🔍 <b>Начинаю поиск билетов</b>")

		result, err := b.flightSearch.Search(settings)
		if err != nil {
			b.SendMessage(chatID, fmt.Sprintf("❌ <b>Ошибка при поиске:</b>\n<code>%v</code>", err))
			return
		}

		log.Printf("Поиск пользователя %d завершён, длина результата: %d", userID, len(result))
		b.SendMessageWithKeyboard(chatID, result, mainKeyboard())
	}()
}

// RunScheduledSearches запускает автопоиск для пользователей,
// у которых текущее время совпадает с персональным расписанием.
func (b *Bot) RunScheduledSearches(now time.Time) {
	currentTime := now.Format("15:04")
	minuteKey := now.Format("2006-01-02 15:04")

	for userID, settings := range b.store.All() {
		if !settings.Schedule.Enabled || settings.Schedule.Time != currentTime {
			continue
		}

		b.scheduleMu.Lock()
		if b.lastScheduled[userID] == minuteKey {
			b.scheduleMu.Unlock()
			continue
		}
		b.lastScheduled[userID] = minuteKey
		b.scheduleMu.Unlock()

		go b.runScheduledSearch(userID, settings)
	}
}

// runScheduledSearch выполняет автопоиск по настройкам пользователя
// и отправляет результат в его личный чат.
func (b *Bot) runScheduledSearch(userID int64, settings UserSettings) {
	lock := b.userSearchLock(userID)
	if !lock.TryLock() {
		log.Printf("Пользователь %d: поиск уже идёт, пропускаем автопоиск", userID)
		return
	}
	defer lock.Unlock()

	log.Printf("Автопоиск для пользователя %d", userID)
	result, err := b.flightSearch.Search(settings)
	if err != nil {
		log.Printf("❌ Ошибка автопоиска пользователя %d: %v", userID, err)
		return
	}

	b.SendMessageWithKeyboard(userID, result, mainKeyboard())
}

func (b *Bot) handleStatus(message *tgbotapi.Message) {
	b.sendStatus(message.From.ID, message.Chat.ID)
}

func (b *Bot) sendStatus(userID, chatID int64) {
	settings := b.settingsFor(userID)

	text := fmt.Sprintf(`📊 <b>Ваши настройки</b>

<b>Направления поиска:</b>
• %s → %s

<b>Параметры:</b>
• Макс. цена: %d руб.
• Глубина поиска: %d месяцев
• Макс. время полёта: %d мин.

<b>Автопоиск:</b> %s

%s

Настройки сохраняются автоматически 🟢`,
		cityNamesText(settings.OriginIATA),
		getCityName(settings.DestinationIATA),
		settings.MaxPrice,
		settings.MonthsToSearch,
		settings.MaxFlightTime,
		settings.ScheduleInfo(),
		settings.DateFilterInfo(),
	)

	b.SendMessageWithKeyboard(chatID, text, mainKeyboard())
}

func (b *Bot) handleSchedule(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	args := strings.Fields(message.Text)

	if len(args) < 2 {
		b.sendSchedule(userID, chatID)
		return
	}

	switch strings.ToLower(args[1]) {
	case "on", "вкл", "включить":
		b.setScheduleEnabled(userID, chatID, true)
	case "off", "выкл", "выключить":
		b.setScheduleEnabled(userID, chatID, false)
	default:
		value := args[1]
		if strings.EqualFold(value, "set") && len(args) >= 3 {
			value = args[2]
		}

		normalized, ok := normalizeScheduleTime(value)
		if !ok {
			b.SendMessage(chatID, "❌ <b>Неверное время.</b>\n\nПример: <code>/schedule 09:30</code>")
			return
		}

		if !b.updateSettings(userID, chatID, func(settings *UserSettings) error {
			settings.Schedule.Enabled = true
			settings.Schedule.Time = normalized
			return nil
		}) {
			return
		}

		b.SendMessage(chatID, fmt.Sprintf("✅ <b>Автопоиск включён</b> ежедневно в <b>%s</b> (время сервера).", normalized))
	}
}

func (b *Bot) setScheduleEnabled(userID, chatID int64, enabled bool) {
	if !b.updateSettings(userID, chatID, func(settings *UserSettings) error {
		settings.Schedule.Enabled = enabled
		return nil
	}) {
		return
	}

	if enabled {
		settings := b.settingsFor(userID)
		b.SendMessage(chatID, fmt.Sprintf("✅ <b>Автопоиск включён</b> ежедневно в <b>%s</b> (время сервера).", settings.Schedule.Time))
		return
	}

	b.SendMessage(chatID, "⏹️ <b>Автопоиск выключен.</b>")
}

func (b *Bot) sendSchedule(userID, chatID int64) {
	settings := b.settingsFor(userID)
	b.SendMessageWithKeyboard(chatID, scheduleText(settings), scheduleKeyboard())
}

func scheduleText(settings UserSettings) string {
	return fmt.Sprintf(`⏰ <b>Автопоиск</b>

Статус: %s

<b>Как настроить:</b>
/schedule 09:30 — включить и задать время
/schedule on — включить
/schedule off — выключить

Время указывается в часовом поясе сервера.`, settings.ScheduleInfo())
}

func (b *Bot) handleScheduleCallback(query *tgbotapi.CallbackQuery, userID, chatID int64, messageID int) {
	enabled := query.Data == "schedule:on"

	if !b.updateSettings(userID, chatID, func(settings *UserSettings) error {
		settings.Schedule.Enabled = enabled
		return nil
	}) {
		return
	}

	b.sendOrEdit(chatID, messageID, scheduleText(b.settingsFor(userID)), scheduleKeyboard())
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

<b>Настройки (у каждого свои):</b>
/setprice - Показать/изменить макс. цену
/origin - Управление городом вылета
/schedule - Время автопоиска
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
🔍 «Начать поиск» — запуск поиска.
🛫 «Город вылета» и 🛬 «Город прилёта» — выбор аэропорта из списка.
Остальные кнопки открывают разделы бота.

<b>Автоматический поиск:</b>
У каждого пользователя своё время автопоиска, по умолчанию 10:00.
Изменить: /schedule 09:30 (время сервера).
Все настройки сохраняются автоматически.`

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
	b.setOrigin(message.From.ID, message.Chat.ID, cityName)
}

func (b *Bot) handleUnknown(message *tgbotapi.Message) {
	text := "❓ Неизвестная команда. Используйте /help для просмотра доступных команд."
	b.SendMessage(message.Chat.ID, text)
}

func (b *Bot) handleSetPrice(message *tgbotapi.Message) {
	args := strings.Fields(message.Text)
	userID := message.From.ID
	chatID := message.Chat.ID

	settings := b.settingsFor(userID)

	if len(args) < 2 {
		b.SendMessage(chatID,
			fmt.Sprintf("💰 <b>Текущая макс. цена:</b> %d руб.\n\n"+
				"Используйте: /setprice 35000", settings.MaxPrice))
		return
	}

	newPrice, err := strconv.Atoi(args[1])
	if err != nil {
		b.SendMessage(chatID,
			fmt.Sprintf("❌ Неверное значение: <code>%s</code>. Введите число.", args[1]))
		return
	}

	if newPrice <= 0 {
		b.SendMessage(chatID, "❌ Цена должна быть больше 0.")
		return
	}

	if newPrice > 1000000 {
		b.SendMessage(chatID, "❌ Цена слишком большая. Максимум: 1 000 000.")
		return
	}

	oldPrice := settings.MaxPrice
	if !b.updateSettings(userID, chatID, func(settings *UserSettings) error {
		settings.MaxPrice = newPrice
		return nil
	}) {
		return
	}

	b.SendMessage(chatID,
		fmt.Sprintf("✅ <b>Макс. цена изменена:</b>\n%d₽ → %d₽", oldPrice, newPrice))
}

// Установка направления по названию города
func (b *Bot) setDestinationByCityName(userID, chatID int64, cityName string, monthsToSearch int) bool {
	settings := b.settingsFor(userID)

	codes, foundCityName := FindAirportCode(cityName)
	if codes == nil {
		b.SendMessage(chatID, fmt.Sprintf("❌ <b>Город '%s' не найден.</b>\n\n"+
			"💡 <i>Используйте:</i>\n"+
			"/search бангкок - поиск по названию\n"+
			"/search BKK - поиск по коду аэропорта\n"+
			"/cities - список доступных городов", cityName))
		return false
	}

	// Если найдено несколько аэропортов, берем первый
	destination := codes[0]
	oldDestination := settings.DestinationIATA

	if !b.updateSettings(userID, chatID, func(settings *UserSettings) error {
		settings.DestinationIATA = destination
		settings.MonthsToSearch = monthsToSearch
		return nil
	}) {
		return false
	}

	var airportInfo string
	if len(codes) > 1 {
		airportInfo = fmt.Sprintf("\n🏢 Доступные аэропорты: %s", strings.Join(codes, ", "))
	}

	b.SendMessage(chatID, fmt.Sprintf("✅ <b>Направление изменено:</b>\n%s → %s\n➡️\n%s → %s%s",
		cityNamesText(settings.OriginIATA),
		getCityName(oldDestination),
		cityNamesText(settings.OriginIATA),
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

func (b *Bot) setOrigin(userID, chatID int64, cityName string) bool {
	settings := b.settingsFor(userID)
	codes, _ := FindOriginAirportCode(cityName)

	if codes == nil {
		b.SendMessage(chatID, fmt.Sprintf("❌ <b>Город вылета '%s' не найден.</b>\n\n"+
			"💡 <i>Используйте:</i>\n"+"/origin москва - установить Москву", cityName))
		return false
	}

	origin := codes[0]
	oldOrigins := settings.OriginIATA

	if !b.updateSettings(userID, chatID, func(settings *UserSettings) error {
		settings.OriginIATA = []string{origin}
		return nil
	}) {
		return false
	}

	var originInfo string
	if len(codes) > 1 {
		originInfo = fmt.Sprintf("\n🏢 Доступные аэропорты: %s", strings.Join(codes, ", "))
	}

	b.SendMessage(chatID, fmt.Sprintf("✅ <b>Город вылета изменён:</b>\n🛫 %s → %s\n\n🛬 Город прилёта: %s%s",
		cityNamesText(oldOrigins),
		getCityName(origin),
		getCityName(settings.DestinationIATA),
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
		b.sendDateFilterStatus(message.From.ID, message.Chat.ID)
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
		b.sendDateFilterStatus(message.From.ID, message.Chat.ID)
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
	userID := message.From.ID
	chatID := message.Chat.ID

	if len(args) < 4 {
		b.SendMessage(chatID, "📅 <b>Установка диапазона дат</b>\n\n"+
			"Пример: /datefilter set 2027-06-01 2027-06-15")
		return
	}

	startDate, err1 := time.Parse("2006-01-02", args[2])
	endDate, err2 := time.Parse("2006-01-02", args[3])

	if err1 != nil || err2 != nil {
		b.SendMessage(chatID, "❌ <b>Неверный формат дат.</b>\n\n"+
			"Пример: /datefilter set 2027-06-01 2027-06-15")
		return
	}

	if endDate.Before(startDate) {
		b.SendMessage(chatID, "❌ <b>Дата окончания раньше даты начала.</b>")
		return
	}

	if !b.updateSettings(userID, chatID, func(settings *UserSettings) error {
		settings.SetDateRange(startDate, endDate)
		return nil
	}) {
		return
	}

	b.SendMessage(chatID, fmt.Sprintf("✅ <b>Диапазон дат установлен:</b>\n📅 с <code>%s</code> по <code>%s</code>",
		startDate.Format("02.01.2006"),
		endDate.Format("02.01.2006")))
}

func (b *Bot) handleDateFilterAdd(message *tgbotapi.Message, args []string) {
	userID := message.From.ID
	chatID := message.Chat.ID

	if len(args) < 3 {
		b.SendMessage(chatID, "📋 <b>Добавление даты в список</b>\n\n"+
			"Пример: /datefilter add 2027-06-15")
		return
	}

	dateStr := args[2]
	if _, err := time.Parse("2006-01-02", dateStr); err != nil {
		b.SendMessage(chatID, "❌ <b>Неверный формат даты.</b>\n\n"+
			"Пример: /datefilter add 2027-06-15")
		return
	}

	if !b.updateSettings(userID, chatID, func(settings *UserSettings) error {
		settings.AddDate(dateStr)
		return nil
	}) {
		return
	}

	t, _ := time.Parse("2006-01-02", dateStr)
	b.SendMessage(chatID, fmt.Sprintf("✅ <b>Дата добавлена:</b> <code>%s</code> (%s)", dateStr, getRussianDayOfWeek(t.Weekday())))
}

func (b *Bot) handleDateFilterRemove(message *tgbotapi.Message, args []string) {
	userID := message.From.ID
	chatID := message.Chat.ID

	if len(args) < 3 {
		b.SendMessage(chatID, "🗑️ <b>Удаление даты из списка</b>\n\n"+
			"Пример: /datefilter remove 2027-06-15")
		return
	}

	dateStr := args[2]
	removed := false

	if !b.updateSettings(userID, chatID, func(settings *UserSettings) error {
		removed = settings.RemoveDate(dateStr)
		return nil
	}) {
		return
	}

	if !removed {
		b.SendMessage(chatID, fmt.Sprintf("❌ <b>Дата <code>%s</code> не найдена в списке.</b>", dateStr))
		return
	}

	b.SendMessage(chatID, fmt.Sprintf("🗑️ <b>Дата удалена:</b> <code>%s</code>", dateStr))
}

func (b *Bot) handleDateFilterClear(message *tgbotapi.Message) {
	if !b.updateSettings(message.From.ID, message.Chat.ID, func(settings *UserSettings) error {
		settings.ClearDateFilter()
		return nil
	}) {
		return
	}

	b.SendMessage(message.Chat.ID, "🧹 <b>Фильтр дат очищен.</b>\nТеперь поиск идёт по всем датам.")
}

func (b *Bot) handleDateFilterDisable(message *tgbotapi.Message) {
	if !b.updateSettings(message.From.ID, message.Chat.ID, func(settings *UserSettings) error {
		settings.DisableDateFilter()
		return nil
	}) {
		return
	}

	b.SendMessage(message.Chat.ID, "⏹️ <b>Фильтр дат отключён.</b>\nТеперь поиск идёт по всем датам.")
}

func (b *Bot) sendDateFilterStatus(userID, chatID int64) {
	info := b.settingsFor(userID).DateFilterInfo()
	b.SendMessageWithKeyboard(chatID, info, mainKeyboard())
}

// handleCallback обрабатывает нажатия на inline-кнопки.
func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	if query.From == nil || !b.isUserAllowed(query.From.ID) {
		b.answerCallback(query.ID, "❌ У вас нет прав для использования этого бота.", true)
		return
	}

	userID := query.From.ID
	b.store.EnsureUser(userID)

	chatID := userID
	if query.Message != nil {
		chatID = query.Message.Chat.ID
	}

	messageID := 0
	if query.Message != nil {
		messageID = query.Message.MessageID
	}

	if query.Data == "search" {
		// Отвечаем сразу, чтобы у пользователя не крутился индикатор загрузки
		b.answerCallback(query.ID, "🔍 Запускаю поиск…", false)
	} else {
		b.answerCallback(query.ID, "", false)
	}

	switch {
	case query.Data == "noop":
		// Кнопка-индикатор страницы — ничего не делаем.
	case query.Data == "menu:main":
		b.sendOrEdit(chatID, messageID, "👋 <b>Главное меню</b>\n\nВыберите действие:", mainKeyboard())
	case strings.HasPrefix(query.Data, "origin:"), strings.HasPrefix(query.Data, "dest:"):
		b.handleCityCallback(query, userID, chatID, messageID)
	case strings.HasPrefix(query.Data, "schedule:"):
		b.handleScheduleCallback(query, userID, chatID, messageID)
	default:
		switch query.Data {
		case "search":
			b.startSearch(userID, chatID)
		case "status":
			b.sendStatus(userID, chatID)
		case "help":
			b.sendHelp(chatID)
		case "cities":
			b.sendCitiesList(chatID)
		case "schedule":
			b.sendSchedule(userID, chatID)
		case "datefilter":
			b.sendDateFilterStatus(userID, chatID)
		default:
			b.SendMessage(chatID, "❓ Неизвестная кнопка.")
		}
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
			tgbotapi.NewInlineKeyboardButtonData("🛫 Город вылета", "origin:page:0"),
			tgbotapi.NewInlineKeyboardButtonData("🛬 Город прилёта", "dest:page:0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Статус", "status"),
			tgbotapi.NewInlineKeyboardButtonData("⏰ Автопоиск", "schedule"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Фильтр дат", "datefilter"),
			tgbotapi.NewInlineKeyboardButtonData("🏙️ Города", "cities"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❓ Помощь", "help"),
		),
	)
}

// scheduleKeyboard — управление персональным автопоиском.
func scheduleKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Включить", "schedule:on"),
			tgbotapi.NewInlineKeyboardButtonData("⏹️ Выключить", "schedule:off"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🏠 В меню", "menu:main"),
		),
	)
}

// cityOption — город/аэропорт для выбора кнопками.
type cityOption struct {
	code string
	name string
}

// cityNamesText переводит IATA-коды в читаемые названия городов.
func cityNamesText(codes []string) string {
	names := make([]string, 0, len(codes))
	for _, code := range codes {
		names = append(names, getCityName(code))
	}
	return strings.Join(names, ", ")
}

// cityOptions возвращает уникальные аэропорты из справочника,
// отсортированные по названию города.
func cityOptions() []cityOption {
	seen := make(map[string]bool)
	var options []cityOption
	for _, codes := range CityAirports {
		for _, code := range codes {
			if seen[code] {
				continue
			}
			seen[code] = true
			options = append(options, cityOption{code: code, name: getCityName(code)})
		}
	}

	sort.Slice(options, func(i, j int) bool {
		if options[i].name == options[j].name {
			return options[i].code < options[j].code
		}
		return options[i].name < options[j].name
	})

	return options
}

// findCityOption ищет аэропорт по IATA-коду.
func findCityOption(code string) (cityOption, bool) {
	for _, opt := range cityOptions() {
		if opt.code == code {
			return opt, true
		}
	}
	return cityOption{}, false
}

const cityPageSize = 12

// cityMenuKeyboard строит постраничный список аэропортов.
func cityMenuKeyboard(prefix string, options []cityOption, page int) tgbotapi.InlineKeyboardMarkup {
	totalPages := (len(options) + cityPageSize - 1) / cityPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	start := page * cityPageSize
	end := start + cityPageSize
	if end > len(options) {
		end = len(options)
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton
	for _, opt := range options[start:end] {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s — %s", opt.code, opt.name), prefix+":set:"+opt.code))
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️", fmt.Sprintf("%s:page:%d", prefix, page-1)),
		tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", page+1, totalPages), "noop"),
		tgbotapi.NewInlineKeyboardButtonData("➡️", fmt.Sprintf("%s:page:%d", prefix, page+1)),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🏠 В меню", "menu:main"),
	))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// showCityMenu показывает список городов для выбора вылета или прилёта.
func (b *Bot) showCityMenu(userID, chatID int64, messageID int, isOrigin bool, page int) {
	settings := b.settingsFor(userID)

	prefix := "dest"
	title := fmt.Sprintf("🛬 <b>Выберите город прилёта</b>\n\nТекущий: <b>%s</b>",
		getCityName(settings.DestinationIATA))
	if isOrigin {
		prefix = "origin"
		title = fmt.Sprintf("🛫 <b>Выберите город вылета</b>\n\nТекущий: <b>%s</b>",
			cityNamesText(settings.OriginIATA))
	}

	keyboard := cityMenuKeyboard(prefix, cityOptions(), page)

	b.sendOrEdit(chatID, messageID, title, keyboard)
}

// handleCityCallback обрабатывает выбор страницы и самого города.
func (b *Bot) handleCityCallback(query *tgbotapi.CallbackQuery, userID, chatID int64, messageID int) {
	parts := strings.Split(query.Data, ":")
	if len(parts) < 2 {
		return
	}

	isOrigin := parts[0] == "origin"

	switch parts[1] {
	case "page":
		page := 0
		if len(parts) >= 3 {
			page, _ = strconv.Atoi(parts[2])
		}
		b.showCityMenu(userID, chatID, messageID, isOrigin, page)

	case "set":
		if len(parts) < 3 {
			return
		}
		code := strings.ToUpper(parts[2])
		option, ok := findCityOption(code)
		if !ok {
			b.SendMessage(chatID, "❌ Неизвестный аэропорт.")
			return
		}

		if !b.updateSettings(userID, chatID, func(settings *UserSettings) error {
			if isOrigin {
				settings.OriginIATA = []string{option.code}
			} else {
				settings.DestinationIATA = option.code
			}
			return nil
		}) {
			return
		}

		settings := b.settingsFor(userID)

		var text string
		if isOrigin {
			text = fmt.Sprintf("✅ <b>Город вылета изменён:</b>\n🛫 %s\n\n🛬 Город прилёта: %s",
				getCityName(option.code), getCityName(settings.DestinationIATA))
		} else {
			text = fmt.Sprintf("✅ <b>Город прилёта изменён:</b>\n🛬 %s\n\n🛫 Город вылета: %s",
				getCityName(option.code), cityNamesText(settings.OriginIATA))
		}

		b.sendOrEdit(chatID, messageID, text, mainKeyboard())
	}
}

// sendOrEdit обновляет существующее сообщение или отправляет новое,
// если обновить не получилось.
func (b *Bot) sendOrEdit(chatID int64, messageID int, text string, keyboard tgbotapi.InlineKeyboardMarkup) {
	if messageID > 0 {
		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, keyboard)
		edit.ParseMode = tgbotapi.ModeHTML
		if _, err := b.api.Request(edit); err == nil {
			return
		} else if strings.Contains(err.Error(), "message is not modified") {
			// Пользователь нажал кнопку текущей страницы — сообщение уже такое.
			return
		} else {
			log.Printf("Не удалось обновить сообщение: %v", err)
		}
	}

	b.SendMessageWithKeyboard(chatID, text, keyboard)
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
