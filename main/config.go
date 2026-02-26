package main

import (
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
