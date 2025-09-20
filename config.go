package main

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// AppConfig содержит все настройки приложения
type AppConfig struct {
	TelegramBotUrl        string
	TelegramBotToken      string
	TelegramChatID        string
	TravelPayoutsToken    string
	TravelPayoutsUrlPrice string
	OriginIATA            []string
	DestinationIATA       string
	MaxPrice              int
	MonthsToSearch        int
}

func loadConfig() (*AppConfig, error) {
	// Загружаем .env файл
	if err := godotenv.Load(); err != nil {
		log.Println("Файл .env не найден, используем переменные окружения")
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

// Для строкового массива
func getEnvStringArray(key string, defaultValue []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	return strings.Split(value, ",")
}
