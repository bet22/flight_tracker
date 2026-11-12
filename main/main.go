package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("PANIC: %v\n", r)
			fmt.Printf("Stack:\n%s\n", debug.Stack())
		}
	}()
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

	// Справочник городов (JSON-файл с горячей перезагрузкой)
	if err := InitCities(config.CitiesFile, seedCities()); err != nil {
		log.Fatalf("Ошибка загрузки справочника городов: %v", err)
	}

	// Хранилище персональных настроек (JSON-файл)
	store, err := NewSettingsStore(config.SettingsFile, config.DefaultUserSettings())
	if err != nil {
		log.Fatalf("Ошибка загрузки настроек: %v", err)
	}
	for _, adminID := range config.AdminUsers {
		store.EnsureUser(adminID)
	}

	// Создаем поисковый сервис
	flightSearch := NewFlightSearch(config)

	// Создаем бота
	bot, err := NewBot(config, flightSearch, store)
	if err != nil {
		log.Fatalf("Ошибка создания бота: %v", err)
	}

	// Запускаем автоматический поиск по расписанию
	cronStop := startScheduledSearch(bot)

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

func startScheduledSearch(bot *Bot) func() {
	c := cron.New()

	// Каждую минуту проверяем, у кого из пользователей наступило время автопоиска
	_, err := c.AddFunc("* * * * *", func() {
		bot.RunScheduledSearches(time.Now())
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
