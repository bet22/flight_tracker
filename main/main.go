package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/robfig/cron/v3"
)

func main() {
	// Загружаем конфигурацию
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Ошибка загрузки конфигурации: %v", err)
	}

	// Валидируем конфигурацию
	if err := config.Validate(); err != nil {
		log.Fatalf("%v", err)
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
	cronStop := startScheduledSearch(bot, config, flightSearch)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Printf("Получен сигнал завершения, останавливаем...")
		cronStop()
		os.Exit(0)
	}()

	// Запускаем бота (блокирующая операция)
	bot.Start()
}

func startScheduledSearch(bot *Bot, config *AppConfig, flightSearch *FlightSearch) func() {
	c := cron.New()

	// Автоматический поиск каждый день в 10:00
	_, err := c.AddFunc("0 10 * * *", func() {
		log.Printf("Запуск автоматического поиска по расписанию")

		result, err := flightSearch.Search()
		if err != nil {
			log.Printf("❌ Ошибка автоматического поиска: %v", err)
			return
		}

		// Отправляем результат администраторам
		for _, adminID := range config.AdminUsers {
			bot.SendMessage(adminID, result)
		}
	})
	if err != nil {
		log.Printf("📅 Ошибка добавления cron задачи: %v", err)
	}

	c.Start()
	log.Println("📅 Планировщик запущен")

	return func() {
		c.Stop().Done()
	}
}
