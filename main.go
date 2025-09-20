package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Flight структура для хранения информации о перелёте
type Flight struct {
	Origin        string
	Destination   string
	DepartureDate string
	DayOfWeek     string
	DepartureTime string
	Price         int
	Airline       string
	Link          string
	Duration      int
	Transfers     int
}

// APIResponse ответ от API TravelPayouts
type APIResponse struct {
	Data []struct {
		Origin      string `json:"origin"`
		Destination string `json:"destination"`
		DepartureAt string `json:"departure_at"`
		Price       int    `json:"price"`
		Airline     string `json:"airline"`
		Link        string `json:"link"`
		Duration    int    `json:"duration"`
		Transfers   int    `json:"transfers"`
	} `json:"data"`
	Error   string `json:"error"`
	Success bool   `json:"success"`
}

func main() {
	// Загружаем конфигурацию
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Ошибка загрузки конфигурации: %v", err)
	}

	fmt.Println("🚀 Запускаем трекер дешёвых авиабилетов на Go...")

	checkFlights(config)
}

func checkFlights(config *AppConfig) {
	fmt.Printf("\n%s Начинаем поиск билетов...\n", time.Now().Format("2006-01-02 15:04"))

	var allFlights []Flight

	// Запускаем поиск для каждого города concurrently
	for _, origin := range config.OriginIATA {
		flights := searchFlightsForOrigin(origin, config)
		allFlights = append(allFlights, flights...)
	}

	if len(allFlights) > 0 {
		sendTelegramMessage(formatMessage(allFlights, config), config)
	} else {
		fmt.Println("Дешёвых билетов не найдено.")
	}
}

func searchFlightsForOrigin(origin string, config *AppConfig) []Flight {
	var flights []Flight

	for monthOffset := 0; monthOffset < config.MonthsToSearch; monthOffset++ {
		monthDate := time.Now().AddDate(0, monthOffset, 0)
		monthStr := monthDate.Format("2006-01")

		fmt.Printf("Проверяем %s -> %s на %s...\n", origin, config.DestinationIATA, monthStr)

		apiURL := config.TravelPayoutsUrlPrice

		params := url.Values{}
		params.Add("origin", origin)
		params.Add("destination", config.DestinationIATA)
		params.Add("currency", "rub")
		params.Add("departure_at", monthStr)
		params.Add("sorting", "price")
		params.Add("direct", "false")
		params.Add("limit", "15")
		params.Add("one_way", "true")
		params.Add("token", config.TravelPayoutsToken)

		req, err := http.NewRequest("GET", apiURL+"?"+params.Encode(), nil)
		if err != nil {
			fmt.Printf("Ошибка создания запроса: %v\n", err)
			continue
		}

		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("Accept", "application/json")

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Ошибка сети: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fmt.Printf("HTTP ошибка: %s\n", resp.Status)
			continue
		}

		var apiResponse APIResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
			fmt.Printf("Ошибка парсинга JSON: %v\n", err)
			continue
		}

		if !apiResponse.Success {
			fmt.Printf("API ошибка: %s\n", apiResponse.Error)
			continue
		}

		for _, flightData := range apiResponse.Data {
			if flightData.Price <= config.MaxPrice {
				departureTime, err := time.Parse(time.RFC3339, flightData.DepartureAt)
				if err != nil {
					fmt.Printf("Ошибка парсинга даты: %v\n", err)
					continue
				}

				flight := Flight{
					Origin:        origin,
					Destination:   flightData.Destination,
					DepartureDate: departureTime.Format("02.01.2006"),
					DayOfWeek:     getRussianDayOfWeek(departureTime.Weekday()),
					DepartureTime: departureTime.Format("15:04"),
					Price:         flightData.Price,
					Airline:       flightData.Airline,
					Link:          "https://aviasales.ru" + flightData.Link,
					Duration:      flightData.Duration,
					Transfers:     flightData.Transfers,
				}
				flights = append(flights, flight)
			}
		}

		time.Sleep(1 * time.Second) // Пауза между запросами
	}

	return flights
}

func formatMessage(flights []Flight, config *AppConfig) string {
	var sb strings.Builder

	sb.WriteString("✈️ <b>НАЙДЕНЫ ДЕШЁВЫЕ БИЛЕТЫ!</b>\n\n")

	// Группируем по городам вылета
	flightsByOrigin := make(map[string][]Flight)
	for _, flight := range flights {
		flightsByOrigin[flight.Origin] = append(flightsByOrigin[flight.Origin], flight)
	}

	for origin, originFlights := range flightsByOrigin {
		// Сортируем по цене
		sort.Slice(originFlights, func(i, j int) bool {
			return originFlights[i].Duration < originFlights[j].Duration
		})

		cityName := getCityName(origin)
		destName := getCityName(config.DestinationIATA)

		sb.WriteString(fmt.Sprintf("🛫 <b>%s → %s</b>\n", cityName, destName))
		sb.WriteString("<code>")
		sb.WriteString("Дата          | Цена    | Время   | Пересад | Рейс\n")
		sb.WriteString("--------------|---------|---------|---------|------\n")
		sb.WriteString("</code>")

		for _, flight := range originFlights[:min(10, len(originFlights))] {
			//durationStr := formatDuration(flight.Duration)
			transfersStr := getTransfersText(flight.Transfers)

			sb.WriteString(fmt.Sprintf(
				"<code>%s %s | %6d₽ | %s | %7s | %s</code> ",
				flight.DepartureDate,
				flight.DayOfWeek,
				flight.Price,
				formatDuration(flight.Duration),
				transfersStr,
				flight.Airline,
			))
			sb.WriteString(fmt.Sprintf("<a href='%s'>🎫</a>\n", flight.Link))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("📊 <b>Информация:</b>\n")
	sb.WriteString("   • ⏱ - время в пути\n")
	sb.WriteString("   • 🎫 - ссылка на покупку\n")

	return sb.String()
}

func sendTelegramMessage(message string, config *AppConfig) {
	apiURL := fmt.Sprintf("%s%s/sendMessage", config.TelegramBotUrl, config.TelegramBotToken)

	params := url.Values{}
	params.Add("chat_id", config.TelegramChatID)
	params.Add("text", message)
	params.Add("parse_mode", "HTML")
	params.Add("disable_web_page_preview", "true")
	params.Add("disable_notification", "true")

	resp, err := http.PostForm(apiURL, params)
	if err != nil {
		fmt.Printf("Ошибка отправки в Telegram: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("Сообщение отправлено в Telegram")
	} else {
		fmt.Printf("Ошибка Telegram API: %s\n", resp.Status)
	}
}

// Вспомогательные функции
func getCityName(iata string) string {
	cities := map[string]string{
		"OVB": "Новосибирск",
		"BAX": "Барнаул",
		"DPS": "Денпасар (Бали)",
	}
	return cities[iata]
}

func getRussianDayOfWeek(day time.Weekday) string {
	days := map[time.Weekday]string{
		time.Monday:    "Пн",
		time.Tuesday:   "Вт",
		time.Wednesday: "Ср",
		time.Thursday:  "Чт",
		time.Friday:    "Пт",
		time.Saturday:  "Сб",
		time.Sunday:    "Вс",
	}
	return days[day]
}

func formatDuration(minutes int) string {
	hours := minutes / 60
	mins := minutes % 60

	if hours > 0 && mins > 0 {
		return fmt.Sprintf("%dч %dм", hours, mins)
	} else if hours > 0 {
		return fmt.Sprintf("%dч", hours)
	} else {
		return fmt.Sprintf("%dм", mins)
	}
}

func getTransfersText(transfers int) string {
	switch transfers {
	case 0:
		return "прямой"
	case 1:
		return "1 перес"
	default:
		return fmt.Sprintf("%d перес", transfers)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
