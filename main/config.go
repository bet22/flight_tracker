package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// AppConfig содержит все настройки приложения
type AppConfig struct {
	TelegramBotUrl        string
	TelegramBotToken      string
	TelegramChatID        string
	AdminUsers            []int64
	TravelPayoutsToken    string
	TravelPayoutsUrlPrice string
	OriginIATA            []string
	DestinationIATA       string
	MaxPrice              int
	MonthsToSearch        int
	MaxFlightTime         int
	DateFilter            DateFilter
}

// Validate проверяет корректность конфигурации
func (config *AppConfig) Validate() error {
	var errs []string

	if config.TelegramBotToken == "" {
		errs = append(errs, "TELEGRAM_BOT_TOKEN не установлен")
	}
	if config.TravelPayoutsToken == "" {
		errs = append(errs, "TRAVELPAYOUTS_TOKEN не установлен")
	}
	if config.TravelPayoutsUrlPrice == "" {
		errs = append(errs, "TRAVELPAYOUTS_URL_PRICE не установлен")
	}
	if len(config.OriginIATA) == 0 || config.OriginIATA[0] == "" {
		errs = append(errs, "ORIGIN_IATA не установлен")
	}
	if config.DestinationIATA == "" {
		errs = append(errs, "DESTINATION_IATA не установлен")
	}
	if config.MaxPrice <= 0 {
		errs = append(errs, fmt.Sprintf("MAX_PRICE должен быть > 0 (текущее: %d)", config.MaxPrice))
	}
	if config.MonthsToSearch <= 0 || config.MonthsToSearch > 12 {
		errs = append(errs, fmt.Sprintf("MONTHS_TO_SEARCH должен быть от 1 до 12 (текущее: %d)", config.MonthsToSearch))
	}
	if config.MaxFlightTime <= 0 {
		errs = append(errs, fmt.Sprintf("MAX_FLIGHT_TIME должен быть > 0 (текущее: %d)", config.MaxFlightTime))
	}

	if len(errs) > 0 {
		return fmt.Errorf("Ошибка валидации конфигурации:\n  • %s", strings.Join(errs, "\n  • "))
	}
	return nil
}

type DateFilter struct {
	StartDate time.Time // Начало периода
	EndDate   time.Time // Конец периода
	Dates     []string  // Конкретные даты (позже)
	Enabled   bool      // Включен ли фильтр
	Mode      string    // "range" или "list"
}

func loadConfig() (*AppConfig, error) {
	// Загружаем .env файл
	if err := godotenv.Load(); err != nil {
		log.Println("Файл .env не найден, используем переменные окружения")
	}

	var adminUsers []int64
	if adminIDs := getEnv("ADMIN_USER_IDS", ""); adminIDs != "" {
		ids := strings.Split(adminIDs, ",")
		for _, idStr := range ids {
			if id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64); err == nil {
				adminUsers = append(adminUsers, id)
			}
		}
	}

	dateFilter := DateFilter{
		Enabled: false,
		Mode:    "range",
	}

	if startDateStr := getEnv("DATE_FILTER_START", ""); startDateStr != "" {
		if startDate, err := time.Parse("2006-01-02", startDateStr); err == nil {
			dateFilter.StartDate = startDate
			dateFilter.Enabled = true
		}
	}

	if endDateStr := getEnv("DATE_FILTER_END", ""); endDateStr != "" {
		if endDate, err := time.Parse("2006-01-02", endDateStr); err == nil {
			dateFilter.EndDate = endDate
			dateFilter.Enabled = true
		}
	}

	if dateListStr := getEnv("DATE_FILTER_LIST", ""); dateListStr != "" {
		dates := strings.Split(dateListStr, ",")
		for _, dateStr := range dates {
			dateStr = strings.TrimSpace(dateStr)
			if _, err := time.Parse("2006-01-02", dateStr); err == nil {
				dateFilter.Dates = append(dateFilter.Dates, dateStr)
			}
		}
		if len(dateFilter.Dates) > 0 {
			dateFilter.Mode = "list"
			dateFilter.Enabled = true
		}
	}

	return &AppConfig{
		TelegramBotUrl:        os.Getenv("TELEGRAM_BOT_URL"),
		TelegramBotToken:      os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:        os.Getenv("TELEGRAM_CHAT_ID"),
		TravelPayoutsToken:    os.Getenv("TRAVELPAYOUTS_TOKEN"),
		TravelPayoutsUrlPrice: os.Getenv("TRAVELPAYOUTS_URL_PRICE"),
		OriginIATA:            getEnvStringArray("ORIGIN_IATA", []string{""}),
		DestinationIATA:       os.Getenv("DESTINATION_IATA"),
		MaxPrice:              getEnvInt("MAX_PRICE", 30000),
		MonthsToSearch:        getEnvInt("MONTHS_TO_SEARCH", 3),
		AdminUsers:            adminUsers,
		MaxFlightTime:         getEnvInt("MAX_FLIGHT_TIME", 1440),
		DateFilter:            dateFilter,
	}, nil
}

func getEnvInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}

	return value
}

func getEnv(key string, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Для строкового массива
func getEnvStringArray(key string, defaultValue []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	return strings.Split(value, ",")
}
