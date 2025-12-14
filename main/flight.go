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

var CityAirports = map[string][]string{
	// Азия
	"бали":         {"DPS"},
	"денпасар":     {"DPS"},
	"бангкок":      {"BKK"},
	"пхукет":       {"HKT"},
	"сингапур":     {"SIN"},
	"куалалумпур":  {"KUL"},
	"куала-лумпур": {"KUL"},
	"ханой":        {"HAN"},
	"хошимин":      {"SGN"},
	"дананг":       {"DAD"},
	"той":          {"NRT", "HND"},
	"сеул":         {"ICN", "GMP"},
	"пекин":        {"PEK"},
	"шахай":        {"PVG"},
	"шанхай":       {"PVG"},
	"дел":          {"DEL"},
	"дуба":         {"DXB"},
	"дубай":        {"DXB"},
	"стамбул":      {"IST"},

	// Европа
	"франкфурт": {"FRA"},
	"париж":     {"CDG", "ORY"},
	"лондон":    {"LHR", "LGW", "STN"},
	"берлин":    {"BER", "SXF", "TXL"},
	"амстердам": {"AMS"},
	"праж":      {"PRG"},
	"прага":     {"PRG"},
	"рим":       {"FCO"},
	"милан":     {"MXP", "LIN"},
	"мадрид":    {"MAD"},
	"барселон":  {"BCN"},
	"барселона": {"BCN"},
	"вена":      {"VIE"},
	"варшав":    {"WAW"},
	"варшава":   {"WAW"},

	// Америка
	"ньюйорк":     {"JFK", "LGA", "EWR"},
	"нью-йорк":    {"JFK", "LGA", "EWR"},
	"лосанделес":  {"LAX"},
	"лос-анделес": {"LAX"},
	"маям":        {"MIA"},
	"майами":      {"MIA"},
	"чикаг":       {"ORD", "MDW"},
	"чикаго":      {"ORD", "MDW"},
	"торонт":      {"YYZ"},
	"торонто":     {"YYZ"},
	"вancouver":   {"YVR"},
	"ванкувер":    {"YVR"},

	// Россия и СНГ
	"москв":          {"SVO", "DME", "VKO"},
	"москва":         {"SVO", "DME", "VKO"},
	"санктпетербург": {"LED"},
	"петербург":      {"LED"},
	"екатеринбург":   {"SVX"},
	"красноярск":     {"KJA"},
	"иркутск":        {"IKT"},
	"владивосток":    {"VVO"},
	"хабаровск":      {"KHV"},
	"алмат":          {"ALA"},
	"алматы":         {"ALA"},
	"ташкент":        {"TAS"},
	"бишкек":         {"FRU"},
}

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

	var allFlights []Flight

	for _, origin := range fs.config.OriginIATA {
		flights := fs.searchFlightsForOrigin(origin)
		allFlights = append(allFlights, flights...)
	}

	if len(allFlights) > 0 {
		return fs.formatMessage(allFlights), nil
	}

	return "ℹ️ Дешёвых билетов не найдено.", nil
}

func (fs *FlightSearch) searchFlightsForOrigin(origin string) []Flight {
	var flights []Flight

	for monthOffset := 0; monthOffset < fs.config.MonthsToSearch; monthOffset++ {
		monthDate := time.Now().AddDate(0, monthOffset, 0)
		monthStr := monthDate.Format("2006-01")

		fmt.Printf("Проверяем %s -> %s на %s...\n", origin, fs.config.DestinationIATA, monthStr)

		apiURL := fs.config.TravelPayoutsUrlPrice

		params := url.Values{}
		params.Add("origin", origin)
		params.Add("destination", fs.config.DestinationIATA)
		params.Add("currency", "rub")
		params.Add("departure_at", monthStr)
		params.Add("sorting", "price")
		params.Add("direct", "false")
		params.Add("limit", "15")
		params.Add("one_way", "true")
		params.Add("token", fs.config.TravelPayoutsToken)

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
			if flightData.Price > fs.config.MaxPrice {
				continue
			}
			if flightData.Duration > fs.config.MaxFlightTime {
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

func (fs *FlightSearch) formatMessage(flights []Flight) string {
	var sb strings.Builder

	sb.WriteString("✈️ <b>НАЙДЕНЫ ДЕШЁВЫЕ БИЛЕТЫ!</b>\n\n")

	flightsByOrigin := make(map[string][]Flight)
	for _, flight := range flights {
		flightsByOrigin[flight.Origin] = append(flightsByOrigin[flight.Origin], flight)
	}

	for origin, originFlights := range flightsByOrigin {
		sort.Slice(originFlights, func(i, j int) bool {
			return originFlights[i].Price < originFlights[j].Price
		})

		cityName := getCityName(origin)
		destName := getCityName(fs.config.DestinationIATA)

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

	sb.WriteString("📊 <b>Информация:</b>\n")
	sb.WriteString("   • 🎫 - ссылка на покупку\n")

	return sb.String()
}

// Вспомогательные функции
func getCityName(iata string) string {
	cityNames := map[string]string{
		// Азия
		"DPS": "Денпасар (Бали)",
		"BKK": "Бангкок",
		"HKT": "Пхукет",
		"SYD": "Сидней",
		"AKL": "Окленд",
		"SIN": "Сингапур",
		"KUL": "Куала-Лумпур",
		"HAN": "Ханой",
		"SGN": "Хошимин",
		"NRT": "Токио (Наррита)",
		"HND": "Токио (Ханеда)",
		"ICN": "Сеул",
		"GMP": "Сеул (Кимхо)",
		"PEK": "Пекин",
		"PVG": "Шанхай",
		"DEL": "Дели",
		"DXB": "Дубай",
		"IST": "Стамбул",

		// Европа
		"FRA": "Франкфурт",
		"CDG": "Париж (Шарль-де-Голль)",
		"ORY": "Париж (Орли)",
		"LHR": "Лондон (Хитроу)",
		"LGW": "Лондон (Гатвик)",
		"STN": "Лондон (Станстед)",
		"BER": "Берлин",
		"AMS": "Амстердам",
		"PRG": "Прага",
		"FCO": "Рим",
		"MXP": "Милан",
		"MAD": "Мадрид",
		"BCN": "Барселона",
		"VIE": "Вена",
		"WAW": "Варшава",

		// Америка
		"JFK": "Нью-Йорк (Кеннеди)",
		"LGA": "Нью-Йорк (ЛаГуардиа)",
		"EWR": "Нью-Йорк (Ньюарк)",
		"LAX": "Лос-Анджелес",
		"MIA": "Майами",
		"ORD": "Чикаго",
		"YYZ": "Торонто",
		"YVR": "Ванкувер",

		// Россия и СНГ
		"SVO": "Москва (Шереметьево)",
		"DME": "Москва (Домодедово)",
		"VKO": "Москва (Внуково)",
		"LED": "Санкт-Петербург",
		"SVX": "Екатеринбург",
		"KJA": "Красноярск",
		"IKT": "Иркутск",
		"VVO": "Владивосток",
		"KHV": "Хабаровск",
		"ALA": "Алматы",
		"TAS": "Ташкент",
		"FRU": "Бишкек",

		// Города вылета
		"OVB": "Новосибирск",
		"BAX": "Барнаул",
	}

	if name, ok := cityNames[iata]; ok {
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
