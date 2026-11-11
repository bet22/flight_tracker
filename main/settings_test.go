package main

import (
	"path/filepath"
	"testing"
	"time"
)

func testDefaults() UserSettings {
	return UserSettings{
		OriginIATA:      []string{"OVB"},
		DestinationIATA: "BKK",
		MaxPrice:        30000,
		MonthsToSearch:  3,
		MaxFlightTime:   1440,
		Schedule:        Schedule{Enabled: true, Time: defaultScheduleTime},
	}
}

func TestNormalizeScheduleTime(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"10:00", "10:00", true},
		{"9:5", "09:05", true},
		{"23:59", "23:59", true},
		{"24:00", "", false},
		{"10:60", "", false},
		{"abc", "", false},
		{"10", "", false},
	}

	for _, tt := range tests {
		got, ok := normalizeScheduleTime(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Errorf("normalizeScheduleTime(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSettingsStorePersistsPerUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "settings.json")

	store, err := NewSettingsStore(path, testDefaults())
	if err != nil {
		t.Fatalf("NewSettingsStore: %v", err)
	}

	store.EnsureUser(1)
	store.EnsureUser(2)

	if err := store.Update(1, func(s *UserSettings) error {
		s.MaxPrice = 12345
		s.DestinationIATA = "HKT"
		s.Schedule.Time = "07:15"
		s.AddDate("2027-06-15")
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Повторно читаем файл — настройки должны сохраниться.
	reloaded, err := NewSettingsStore(path, testDefaults())
	if err != nil {
		t.Fatalf("NewSettingsStore reload: %v", err)
	}

	first := reloaded.Get(1)
	if first.MaxPrice != 12345 || first.DestinationIATA != "HKT" || first.Schedule.Time != "07:15" {
		t.Fatalf("настройки пользователя 1 не сохранились: %+v", first)
	}
	if len(first.DateFilter.Dates) != 1 || first.DateFilter.Dates[0] != "2027-06-15" {
		t.Fatalf("фильтр дат не сохранился: %+v", first.DateFilter)
	}

	// Второй пользователь не должен получить чужие настройки.
	second := reloaded.Get(2)
	if second.MaxPrice != testDefaults().MaxPrice || second.DestinationIATA != testDefaults().DestinationIATA {
		t.Fatalf("настройки пользователя 2 изменились: %+v", second)
	}
}

func TestSettingsStoreIndependentCopies(t *testing.T) {
	store, err := NewSettingsStore(filepath.Join(t.TempDir(), "settings.json"), testDefaults())
	if err != nil {
		t.Fatalf("NewSettingsStore: %v", err)
	}

	store.EnsureUser(1)

	settings := store.Get(1)
	settings.OriginIATA[0] = "LED"
	settings.DateFilter.Dates = append(settings.DateFilter.Dates, "2027-01-01")

	got := store.Get(1)
	if got.OriginIATA[0] != "OVB" || len(got.DateFilter.Dates) != 0 {
		t.Fatalf("Get вернул ссылку на внутренние данные: %+v", got)
	}
}

func TestDateFilterRangeIncludesLastDay(t *testing.T) {
	start := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2027, 6, 15, 0, 0, 0, 0, time.UTC)
	filter := DateFilter{Enabled: true, Mode: "range", StartDate: start, EndDate: end}

	if !filter.Matches("2027-06-15T10:30:00+07:00") {
		t.Fatal("последний день диапазона должен включаться")
	}
	if filter.Matches("2027-06-16T00:00:00+07:00") {
		t.Fatal("день после диапазона не должен включаться")
	}
	if filter.Matches("bad") {
		t.Fatal("некорректная дата не должна проходить фильтр")
	}
}
