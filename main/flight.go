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

// FindAirportCode ищет аэропорты по коду, названию города или алиасу.
func FindAirportCode(cityName string) ([]string, string) {
	codes, name, ok := cityStore().Lookup(cityName)
	if !ok {
		return nil, ""
	}
	return codes, name
}

func GetCityList() string {
	return cityStore().GetCityList()
}

func NewFlightSearch(config *AppConfig) *FlightSearch {
	return &FlightSearch{
		config: config,
	}
}

func (fs *FlightSearch) Search(settings UserSettings) (string, error) {
	fmt.Printf("\n%s Начинаем поиск билетов...\n", time.Now().Format("2006-01-02 15:04"))

	var arrival []Flight
	var departure []Flight

	for _, origin := range settings.OriginIATA {
		flights := fs.searchFlightsForOrigin(settings, origin, false)
		arrival = append(arrival, flights...)
	}

	if len(settings.OriginIATA) > 0 {
		departure = append(departure, fs.searchFlightsForOrigin(settings, settings.DestinationIATA, true)...)
	}

	if len(arrival) > 0 || len(departure) > 0 {
		fmt.Printf("\n%s Найдено %d билетов\n", time.Now().Format("2006-01-02 15:04"), len(arrival)+len(departure))
		return fs.formatMessage(settings, arrival, departure), nil
	}
	fmt.Printf("\n%s Дешёвых билетов не найдено\n", time.Now().Format("2006-01-02 15:04"))
	return "ℹ️ Дешёвых билетов не найдено.", nil
}

// httpClient переиспользуется для всех запросов
var httpClient = &http.Client{Timeout: 30 * time.Second}

func (fs *FlightSearch) searchFlightsForOrigin(settings UserSettings, origin string, backTicket bool) []Flight {
	fmt.Printf("\n%s Start method searchFlightsForOrigin\n", time.Now().Format("2006-01-02 15:04"))
	var flights []Flight
	var dest string
	if backTicket {
		if len(settings.OriginIATA) == 0 {
			fmt.Printf("Пустой OriginIATA. Невозможно найти обратный билет")
			return nil
		}
		dest = settings.OriginIATA[0]
	} else {
		dest = settings.DestinationIATA
	}

	now := time.Now()
	for monthOffset := 0; monthOffset < settings.MonthsToSearch; monthOffset++ {
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
			if flightData.Price > settings.MaxPrice {
				continue
			}
			if flightData.Duration > settings.MaxFlightTime {
				continue
			}
			if !settings.DateFilter.Matches(flightData.DepartureAt) {
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
	fmt.Printf("%s Finish method searchFlightsForOrigin\n", time.Now().Format("2006-01-02 15:04"))
	return flights
}

func (df *DateFilter) Matches(dateStr string) bool {
	if !df.Enabled {
		return true
	}
	if len(dateStr) < 10 {
		return false
	}

	flightDate, err := time.Parse("2006-01-02", dateStr[:10])
	if err != nil {
		return false
	}

	switch df.Mode {
	case "range":
		// Проверяем, попадает ли дата в диапазон (включая последний день)
		if !df.StartDate.IsZero() && flightDate.Before(df.StartDate) {
			return false
		}
		if !df.EndDate.IsZero() && !flightDate.Before(df.EndDate.AddDate(0, 0, 1)) {
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

func (fs *FlightSearch) formatMessage(settings UserSettings, arrival []Flight, departure []Flight) string {
	var sb strings.Builder

	sb.WriteString("✈️ <b>НАЙДЕНЫ ДЕШЁВЫЕ БИЛЕТЫ!</b>\n\n")

	fs.formatFlightGroup(&sb, arrival, settings.DestinationIATA)
	if len(settings.OriginIATA) > 0 {
		fs.formatFlightGroup(&sb, departure, settings.OriginIATA[0])
	}

	sb.WriteString("📊 <b>Информация:</b>\n")
	sb.WriteString("   • 🎫 - ссылка на покупку\n")

	return sb.String()
}

// formatFlightGroup форматирует группу рейсов (прилёт или вылет)
func (fs *FlightSearch) formatFlightGroup(sb *strings.Builder, flights []Flight, returnDestination string) {
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

		for _, flight := range originFlights[:minPrice(8, len(originFlights))] {
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
	return cityStore().Name(iata)
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
	}

	return fmt.Sprintf("%dм", mins)
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

func minPrice(a, b int) int {
	if a < b {
		return a
	}
	return b
}
