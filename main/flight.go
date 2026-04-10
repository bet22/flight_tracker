package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

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

type FlightSearch struct {
	config *AppConfig
}

func FindAirportCode(cityName string) ([]string, string) {
	// Приводим к нижнему регистру и убираем пробелы
	normalized := strings.ToLower(strings.TrimSpace(cityName))

	// Прямой поиск
	if codes, exists := CityAirports[normalized]; exists {
		return codes, getCityName(codes[0])
	}

	// Поиск по частичному совпадению
	for city, codes := range CityAirports {
		if strings.Contains(normalized, city) || strings.Contains(city, normalized) {
			return codes, getCityName(codes[0])
		}
	}

	return nil, ""
}

func GetCityList() string {
	var cities []string

	// Собираем уникальные города (берем первый аэропорт для каждого города)
	addedCities := make(map[string]bool)
	for _, codes := range CityAirports {
		if len(codes) > 0 && !addedCities[codes[0]] {
			cityName := getCityName(codes[0])
			cities = append(cities, fmt.Sprintf("%s - %s", codes[0], cityName))
			addedCities[codes[0]] = true
		}
	}

	sort.Strings(cities)
	return strings.Join(cities, "\n")
}

func NewFlightSearch(config *AppConfig) *FlightSearch {
	return &FlightSearch{
		config: config,
	}
}

func (fs *FlightSearch) Search() (string, error) {
	fmt.Printf("\n%s Начинаем поиск билетов...\n", time.Now().Format("2006-01-02 15:04"))

	var arrival []Flight
	var departure []Flight

	for _, origin := range fs.config.OriginIATA {
		flights := fs.searchFlightsForOrigin(origin, fs.config.MonthsToSearch, false)
		arrival = append(arrival, flights...)
	}

	if len(fs.config.OriginIATA) > 0 {
		departure = append(departure, fs.searchFlightsForOrigin(fs.config.DestinationIATA, fs.config.MonthsToSearch, true)...)
	}

	if len(arrival) > 0 || len(departure) > 0 {
		return fs.formatMessage(arrival, departure), nil
	}

	return "ℹ️ Дешёвых билетов не найдено.", nil
}

// httpClient переиспользуется для всех запросов
var httpClient = &http.Client{Timeout: 30 * time.Second}

func (fs *FlightSearch) searchFlightsForOrigin(origin string, monthsToSearch int, backTicket bool) []Flight {
	var flights []Flight
	var dest string
	if backTicket {
		if len(fs.config.OriginIATA) == 0 {
			fmt.Printf("Пустой OriginIATA. Невозможно найти обратный билет")
			return nil
		}
		dest = fs.config.OriginIATA[0]
	} else {
		dest = fs.config.DestinationIATA
	}

	now := time.Now()
	for monthOffset := 0; monthOffset < monthsToSearch; monthOffset++ {
		monthDate := now.AddDate(0, monthOffset, 0)
		monthStr := monthDate.Format("2006-01")

		fmt.Printf("Проверяем %s -> %s на %s...\n", origin, dest, monthStr)

		apiURL := fs.config.TravelPayoutsUrlPrice

		params := url.Values{}
		params.Add("origin", origin)
		params.Add("destination", dest)
		params.Add("currency", "rub")
		params.Add("departure_at", monthStr)
		params.Add("sorting", "price")
		params.Add("direct", "false")
		params.Add("limit", "30")
		params.Add("one_way", "true")
		params.Add("token", fs.config.TravelPayoutsToken)

		req, err := http.NewRequest("GET", apiURL+"?"+params.Encode(), nil)
		if err != nil {
			fmt.Printf("Ошибка создания запроса: %v\n", err)
			continue
		}

		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("Accept", "application/json")

		resp, err := httpClient.Do(req)
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
			if flightData.Price > fs.config.MaxPrice {
				continue
			}
			if flightData.Duration > fs.config.MaxFlightTime {
				continue
			}
			if !fs.config.DateFilter.Matches(flightData.DepartureAt) {
				continue
			}
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

		time.Sleep(1 * time.Second)
	}

	return flights
}

func (df *DateFilter) Matches(dateStr string) bool {
	if !df.Enabled {
		return true
	}

	flightDate, err := time.Parse("2006-01-02", dateStr[:10])
	if err != nil {
		return false
	}

	switch df.Mode {
	case "range":
		// Проверяем, попадает ли дата в диапазон
		if !df.StartDate.IsZero() && flightDate.Before(df.StartDate) {
			return false
		}
		if !df.EndDate.IsZero() && flightDate.After(df.EndDate) {
			return false
		}
		return true

	case "list":
		// Проверяем, есть ли дата в списке
		for _, d := range df.Dates {
			if d == dateStr[:10] {
				return true
			}
		}
		return false

	default:
		return true
	}
}

func (fs *FlightSearch) formatMessage(arrival []Flight, departure []Flight) string {
	var sb strings.Builder

	sb.WriteString("✈️ <b>НАЙДЕНЫ ДЕШЁВЫЕ БИЛЕТЫ!</b>\n\n")

	fs.formatFlightGroup(&sb, "arrival", arrival, fs.config.DestinationIATA)
	fs.formatFlightGroup(&sb, "departure", departure, fs.config.OriginIATA[0])

	sb.WriteString("📊 <b>Информация:</b>\n")
	sb.WriteString("   • 🎫 - ссылка на покупку\n")

	return sb.String()
}

// formatFlightGroup форматирует группу рейсов (прилёт или вылет)
func (fs *FlightSearch) formatFlightGroup(sb *strings.Builder, groupType string, flights []Flight, returnDestination string) {
	if len(flights) == 0 {
		return
	}

	byOrigin := make(map[string][]Flight)
	for _, flight := range flights {
		byOrigin[flight.Origin] = append(byOrigin[flight.Origin], flight)
	}

	// Сортируем origins для стабильного вывода
	var origins []string
	for origin := range byOrigin {
		origins = append(origins, origin)
	}
	sort.Strings(origins)

	for _, origin := range origins {
		originFlights := byOrigin[origin]
		sort.Slice(originFlights, func(i, j int) bool {
			return originFlights[i].Price < originFlights[j].Price
		})

		cityName := getCityName(origin)
		destName := getCityName(returnDestination)

		sb.WriteString(fmt.Sprintf("🛫 <b>%s → %s</b>\n", cityName, destName))
		sb.WriteString("<code>")
		sb.WriteString("Дата          | Цена    | Время   | Пересад | Рейс\n")
		sb.WriteString("--------------|---------|---------|---------|------\n")
		sb.WriteString("</code>")

		for _, flight := range originFlights[:min(10, len(originFlights))] {
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
}

// Вспомогательные функции
func getCityName(iata string) string {
	if name, ok := CityNames[iata]; ok {
		return name
	}
	return iata
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

func (fs *FlightSearch) SetDestination(destination string) {
	fs.config.DestinationIATA = strings.ToUpper(destination)
}

func (fs *FlightSearch) SetMonthsToSearch(monthsToSearch int) {
	fs.config.MonthsToSearch = monthsToSearch
}

func (fs *FlightSearch) SetOriginIATA(origin string) {
	for _, existing := range fs.config.OriginIATA {
		if existing == origin {
			return
		}
	}
	fs.config.OriginIATA = []string{origin}
}

func (fs *FlightSearch) SetMaxPrice(price int) {
	fs.config.MaxPrice = price
}
