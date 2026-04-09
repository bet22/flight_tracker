package main

import (
	"testing"
	"time"
)

// --- FindAirportCode tests ---

func TestFindAirportCode_ExactMatch(t *testing.T) {
	codes, cityName := FindAirportCode("бангкок")
	if codes == nil {
		t.Fatal("Expected codes for 'бангкок'")
	}
	if codes[0] != "BKK" {
		t.Errorf("Expected BKK, got %s", codes[0])
	}
	if cityName != "Бангкок" {
		t.Errorf("Expected 'Бангкок', got '%s'", cityName)
	}
}

func TestFindAirportCode_PartialMatch(t *testing.T) {
	codes, _ := FindAirportCode("бангкокский")
	if codes == nil {
		t.Fatal("Expected partial match for 'бангкокский'")
	}
	if codes[0] != "BKK" {
		t.Errorf("Expected BKK, got %s", codes[0])
	}
}

func TestFindAirportCode_NoMatch(t *testing.T) {
	codes, cityName := FindAirportCode("несуществующийгород")
	if codes != nil {
		t.Errorf("Expected nil for unknown city, got %v", codes)
	}
	if cityName != "" {
		t.Errorf("Expected empty city name, got '%s'", cityName)
	}
}

func TestFindAirportCode_MultiAirport(t *testing.T) {
	codes, _ := FindAirportCode("лондон")
	if codes == nil {
		t.Fatal("Expected codes for 'лондон'")
	}
	if len(codes) != 3 {
		t.Errorf("Expected 3 airports for London, got %d", len(codes))
	}
}

func TestFindAirportCode_CaseInsensitive(t *testing.T) {
	codes1, _ := FindAirportCode("БАЛИ")
	codes2, _ := FindAirportCode("Бали")
	codes3, _ := FindAirportCode("бали")

	if codes1[0] != "DPS" || codes2[0] != "DPS" || codes3[0] != "DPS" {
		t.Errorf("Case insensitive search failed: got %v, %v, %v", codes1, codes2, codes3)
	}
}

// --- DateFilter.Matches tests ---

func TestDateFilter_Disabled(t *testing.T) {
	df := DateFilter{Enabled: false}
	if !df.Matches("2026-05-15") {
		t.Error("Disabled filter should match any date")
	}
}

func TestDateFilter_Range_InRange(t *testing.T) {
	df := DateFilter{
		Enabled:   true,
		Mode:      "range",
		StartDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
	}
	if !df.Matches("2026-05-15T10:00:00Z") {
		t.Error("Date in range should match")
	}
}

func TestDateFilter_Range_BeforeStart(t *testing.T) {
	df := DateFilter{
		Enabled:   true,
		Mode:      "range",
		StartDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	}
	if df.Matches("2026-05-05T10:00:00Z") {
		t.Error("Date before start should not match")
	}
}

func TestDateFilter_Range_AfterEnd(t *testing.T) {
	df := DateFilter{
		Enabled: true,
		Mode:    "range",
		EndDate: time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
	}
	if df.Matches("2026-05-25T10:00:00Z") {
		t.Error("Date after end should not match")
	}
}

func TestDateFilter_Range_OnlyStart(t *testing.T) {
	df := DateFilter{
		Enabled:   true,
		Mode:      "range",
		StartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	if !df.Matches("2026-06-15T10:00:00Z") {
		t.Error("Date after start (no end) should match")
	}
	if df.Matches("2026-05-15T10:00:00Z") {
		t.Error("Date before start (no end) should not match")
	}
}

func TestDateFilter_List_Match(t *testing.T) {
	df := DateFilter{
		Enabled: true,
		Mode:    "list",
		Dates:   []string{"2026-05-01", "2026-06-15", "2026-07-20"},
	}
	if !df.Matches("2026-06-15T10:00:00Z") {
		t.Error("Date in list should match")
	}
}

func TestDateFilter_List_NoMatch(t *testing.T) {
	df := DateFilter{
		Enabled: true,
		Mode:    "list",
		Dates:   []string{"2026-05-01", "2026-06-15"},
	}
	if df.Matches("2026-07-01T10:00:00Z") {
		t.Error("Date not in list should not match")
	}
}

func TestDateFilter_InvalidDate(t *testing.T) {
	df := DateFilter{Enabled: true}
	if df.Matches("invalid-date") {
		t.Error("Invalid date should not match")
	}
}

// --- formatDuration tests ---

func TestFormatDuration_HoursAndMinutes(t *testing.T) {
	if got := formatDuration(150); got != "2ч 30м" {
		t.Errorf("Expected '2ч 30м', got '%s'", got)
	}
}

func TestFormatDuration_OnlyHours(t *testing.T) {
	if got := formatDuration(480); got != "8ч" {
		t.Errorf("Expected '8ч', got '%s'", got)
	}
}

func TestFormatDuration_OnlyMinutes(t *testing.T) {
	if got := formatDuration(45); got != "45м" {
		t.Errorf("Expected '45м', got '%s'", got)
	}
}

func TestFormatDuration_Zero(t *testing.T) {
	if got := formatDuration(0); got != "0м" {
		t.Errorf("Expected '0м', got '%s'", got)
	}
}

// --- AppConfig.Validate tests ---

func TestValidate_EmptyConfig(t *testing.T) {
	cfg := &AppConfig{}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation error for empty config")
	}
	// Should have multiple errors
	expected := []string{
		"TELEGRAM_BOT_TOKEN",
		"TRAVELPAYOUTS_TOKEN",
		"TRAVELPAYOUTS_URL_PRICE",
		"ORIGIN_IATA",
		"DESTINATION_IATA",
	}
	for _, keyword := range expected {
		if !containsStr(err.Error(), keyword) {
			t.Errorf("Expected error to contain '%s', got: %s", keyword, err.Error())
		}
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &AppConfig{
		TelegramBotToken:      "test-token",
		TravelPayoutsToken:    "tp-token",
		TravelPayoutsUrlPrice: "https://api.example.com",
		OriginIATA:            []string{"OVB"},
		DestinationIATA:       "DPS",
		MaxPrice:              30000,
		MonthsToSearch:        6,
		MaxFlightTime:         1440,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Expected no error for valid config, got: %v", err)
	}
}

func TestValidate_InvalidPrice(t *testing.T) {
	cfg := &AppConfig{
		TelegramBotToken:      "test",
		TravelPayoutsToken:    "test",
		TravelPayoutsUrlPrice: "http://test",
		OriginIATA:            []string{"OVB"},
		DestinationIATA:       "DPS",
		MaxPrice:              -100,
		MonthsToSearch:        6,
		MaxFlightTime:         1440,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for negative price")
	}
	if !containsStr(err.Error(), "MAX_PRICE") {
		t.Errorf("Expected error about MAX_PRICE, got: %s", err)
	}
}

func TestValidate_MonthsOutOfRange(t *testing.T) {
	cfg := &AppConfig{
		TelegramBotToken:      "test",
		TravelPayoutsToken:    "test",
		TravelPayoutsUrlPrice: "http://test",
		OriginIATA:            []string{"OVB"},
		DestinationIATA:       "DPS",
		MaxPrice:              30000,
		MonthsToSearch:        13,
		MaxFlightTime:         1440,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for months > 12")
	}
	if !containsStr(err.Error(), "MONTHS_TO_SEARCH") {
		t.Errorf("Expected error about MONTHS_TO_SEARCH, got: %s", err)
	}
}

func TestValidate_EmptyOrigin(t *testing.T) {
	cfg := &AppConfig{
		TelegramBotToken:      "test",
		TravelPayoutsToken:    "test",
		TravelPayoutsUrlPrice: "http://test",
		OriginIATA:            []string{""},
		DestinationIATA:       "DPS",
		MaxPrice:              30000,
		MonthsToSearch:        6,
		MaxFlightTime:         1440,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected error for empty origin")
	}
	if !containsStr(err.Error(), "ORIGIN_IATA") {
		t.Errorf("Expected error about ORIGIN_IATA, got: %s", err)
	}
}

// --- getCityName tests ---

func TestGetCityName_KnownIATA(t *testing.T) {
	tests := map[string]string{
		"DPS": "Денпасар (Бали)",
		"BKK": "Бангкок",
		"SVO": "Москва (Шереметьево)",
		"JFK": "Нью-Йорк (Кеннеди)",
	}
	for iata, expected := range tests {
		if got := getCityName(iata); got != expected {
			t.Errorf("getCityName(%s) = '%s', expected '%s'", iata, got, expected)
		}
	}
}

func TestGetCityName_UnknownIATA(t *testing.T) {
	if got := getCityName("XXX"); got != "XXX" {
		t.Errorf("Expected 'XXX' for unknown IATA, got '%s'", got)
	}
}

// --- helpers ---

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
