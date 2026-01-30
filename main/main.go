package main

import (
	"fmt"
	"log"

	"github.com/robfig/cron/v3"
)

func main() {
	// Загружаем конфигурацию
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Ошибка загрузки конфигурации: %v", err)
	}

	fmt.Println("🚀 Запускаем трекер авиабилетов с Telegram ботом...")

	// Создаем поисковый сервис
	flightSearch := NewFlightSearch(config)

	// Создаем бота
	bot, err := NewBot(config, flightSearch)
	if err != nil {
		log.Fatalf("Ошибка создания бота: %v", err)
	}

	// Запускаем автоматический поиск по расписанию
	go startScheduledSearch(bot, config, flightSearch)

	// Запускаем бота (блокирующая операция)
	bot.Start()
}

func startScheduledSearch(bot *Bot, config *AppConfig, flightSearch *FlightSearch) {
	c := cron.New()

	// Автоматический поиск каждый день в 10:00
	c.AddFunc("0 10 * * *", func() {
		log.Println("🕙 Запуск автоматического поиска по расписанию...")

		result, err := flightSearch.Search()
		if err != nil {
			log.Printf("❌ Ошибка автоматического поиска: %v", err)
			return
		}

		// Отправляем результат в основной чат
		for _, adminID := range config.AdminUsers {
			bot.SendMessage(adminID, result)
		}
	})

	// Для теста: каждые 6 часов

	c.Start()
	log.Println("📅 Планировщик запущен")
}
