package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

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
		default:
			b.handleUnknown(update.Message)
		}
	}
}

func (b *Bot) isUserAllowed(userID int64) bool {
	// Если не указаны администраторы, разрешаем всем
	if len(b.config.AdminUsers) == 0 {
		return true
	}

	// Проверяем есть ли пользователь в списке администраторов
	for _, adminID := range b.config.AdminUsers {
		if userID == adminID {
			return true
		}
	}
	return false
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

	// 🆕 Если указано направление как второй параметр
	if len(args) >= 2 {
		if len(args) >= 3 {
			destination := strings.ToUpper(args[1])
			monthsToSearch, err := strconv.Atoi(args[2])
			if err != nil {

			}
			b.setDestinationAndMonthsToSearch(message.Chat.ID, destination, monthsToSearch)
		} else {
			destination := strings.ToUpper(args[1])
			b.setDestination(message.Chat.ID, destination)
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
• Авто-поиск: каждый день в 10:00

Бот работает в штатном режиме 🟢`,
		strings.Join(b.config.OriginIATA, "/"),
		b.config.DestinationIATA,
		b.config.MaxPrice,
		b.config.MonthsToSearch,
	)

	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}

func (b *Bot) handleHelp(message *tgbotapi.Message) {
	text := `❓ <b>Помощь по боту</b>

<b>Команды:</b>
/search - Запустить поиск билетов
/status - Показать статус бота
/help - Эта справка

<b>Автоматический поиск:</b>
Бот автоматически ищет билеты каждый день в 10:00 и присылает уведомления.

<b>Ручной поиск:</b>
Используйте команду /search в любое время для запуска поиска.

<b>Настройки:</b>
Параметры поиска задаются в конфигурации бота.`

	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = "HTML"
	b.api.Send(msg)
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

func (b *Bot) setDestinationAndMonthsToSearch(chatID int64, destination string, monthsToSearch int) {
	oldDestination := b.config.DestinationIATA
	b.flightSearch.SetDestination(destination)
	oldMonthsToSearch := b.config.MonthsToSearch
	b.flightSearch.SetMonthsToSearch(monthsToSearch)

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ <b>Направление и глибина поиска изменены:</b>\n%s → %s\n➡️\n%s → %s</b>\n%d мес.→ %d мес.",
			strings.Join(b.config.OriginIATA, "/"),
			getCityName(oldDestination),
			strings.Join(b.config.OriginIATA, "/"),
			getCityName(destination),
			b.config.MonthsToSearch,
			oldMonthsToSearch))
	msg.ParseMode = "HTML"
	b.api.Send(msg)
}
func (b *Bot) handleUnknown(message *tgbotapi.Message) {
	text := "❓ Неизвестная команда. Используйте /help для просмотра доступных команд."
	msg := tgbotapi.NewMessage(message.Chat.ID, text)
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
