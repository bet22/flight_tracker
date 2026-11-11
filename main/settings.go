package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultScheduleTime = "10:00"

// Schedule — персональное расписание автоматического поиска.
type Schedule struct {
	Enabled bool   `json:"enabled"`
	Time    string `json:"time"` // "HH:MM" в часовом поясе сервера
}

// UserSettings — персональные настройки поиска и планировщика.
type UserSettings struct {
	OriginIATA      []string   `json:"origin_iata"`
	DestinationIATA string     `json:"destination_iata"`
	MaxPrice        int        `json:"max_price"`
	MonthsToSearch  int        `json:"months_to_search"`
	MaxFlightTime   int        `json:"max_flight_time"`
	DateFilter      DateFilter `json:"date_filter"`
	Schedule        Schedule   `json:"schedule"`
}

// Clone возвращает независимую копию настроек.
func (s UserSettings) Clone() UserSettings {
	c := s
	c.OriginIATA = slices.Clone(s.OriginIATA)
	c.DateFilter = s.DateFilter.Clone()
	return c
}

// Clone возвращает копию фильтра дат.
func (df DateFilter) Clone() DateFilter {
	c := df
	c.Dates = slices.Clone(df.Dates)
	return c
}

// DefaultUserSettings собирает настройки по умолчанию из переменных окружения.
func (config *AppConfig) DefaultUserSettings() UserSettings {
	return UserSettings{
		OriginIATA:      slices.Clone(config.OriginIATA),
		DestinationIATA: config.DestinationIATA,
		MaxPrice:        config.MaxPrice,
		MonthsToSearch:  config.MonthsToSearch,
		MaxFlightTime:   config.MaxFlightTime,
		DateFilter:      config.DateFilter.Clone(),
		Schedule: Schedule{
			Enabled: true,
			Time:    config.ScheduleTime,
		},
	}
}

// SettingsStore хранит настройки пользователей в JSON-файле.
type SettingsStore struct {
	mu       sync.RWMutex
	users    map[int64]UserSettings
	path     string
	defaults UserSettings
}

// NewSettingsStore загружает настройки из файла (если он есть).
func NewSettingsStore(path string, defaults UserSettings) (*SettingsStore, error) {
	store := &SettingsStore{
		users:    make(map[int64]UserSettings),
		path:     path,
		defaults: defaults.Clone(),
	}

	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *SettingsStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		log.Printf("Файл настроек %q не найден, создаём настройки по умолчанию", s.path)
		return nil
	}
	if err != nil {
		return fmt.Errorf("чтение файла настроек %q: %w", s.path, err)
	}
	if len(data) == 0 {
		return nil
	}

	var raw map[string]UserSettings
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("разбор файла настроек %q: %w", s.path, err)
	}

	for key, settings := range raw {
		userID, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			log.Printf("Пропускаем некорректный ID пользователя %q в файле настроек", key)
			continue
		}
		s.users[userID] = settings.Clone()
	}

	log.Printf("Загружены настройки %d пользователей из %q", len(s.users), s.path)
	return nil
}

// EnsureUser создаёт настройки пользователя или дополняет отсутствующие поля.
func (s *SettingsStore) EnsureUser(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings, exists := s.users[userID]
	if !exists {
		s.users[userID] = s.defaults.Clone()
		s.saveLocked()
		return
	}

	if fillDefaults(&settings, s.defaults) {
		s.users[userID] = settings
		s.saveLocked()
	}
}

// Get возвращает копию настроек пользователя.
func (s *SettingsStore) Get(userID int64) UserSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if settings, ok := s.users[userID]; ok {
		return settings.Clone()
	}
	return s.defaults.Clone()
}

// Update изменяет настройки пользователя и сразу сохраняет их на диск.
// При ошибке записи изменения откатываются.
func (s *SettingsStore) Update(userID int64, fn func(*UserSettings) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.users[userID]
	if !ok {
		current = s.defaults.Clone()
	}

	updated := current.Clone()
	if err := fn(&updated); err != nil {
		return err
	}

	s.users[userID] = updated
	if err := s.saveLocked(); err != nil {
		s.users[userID] = current
		return err
	}

	return nil
}

// All возвращает копии настроек всех пользователей.
func (s *SettingsStore) All() map[int64]UserSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[int64]UserSettings, len(s.users))
	for userID, settings := range s.users {
		result[userID] = settings.Clone()
	}
	return result
}

// saveLocked сохраняет настройки атомарно (временный файл + rename).
// Вызывать только при удерживаемой блокировке.
func (s *SettingsStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		log.Printf("Не удалось создать каталог настроек: %v", err)
		return err
	}

	raw := make(map[string]UserSettings, len(s.users))
	for userID, settings := range s.users {
		raw[strconv.FormatInt(userID, 10)] = settings
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		log.Printf("Не удалось сериализовать настройки: %v", err)
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".settings-*.json")
	if err != nil {
		log.Printf("Не удалось создать временный файл настроек: %v", err)
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		log.Printf("Не удалось записать настройки: %v", err)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, s.path); err != nil {
		log.Printf("Не удалось заменить файл настроек: %v", err)
		return err
	}

	return nil
}

// fillDefaults заполняет пустые поля значениями по умолчанию.
// Возвращает true, если что-то изменилось.
func fillDefaults(settings *UserSettings, defaults UserSettings) bool {
	changed := false

	if len(settings.OriginIATA) == 0 {
		settings.OriginIATA = slices.Clone(defaults.OriginIATA)
		changed = true
	}
	if settings.DestinationIATA == "" {
		settings.DestinationIATA = defaults.DestinationIATA
		changed = true
	}
	if settings.MaxPrice <= 0 {
		settings.MaxPrice = defaults.MaxPrice
		changed = true
	}
	if settings.MonthsToSearch < 1 || settings.MonthsToSearch > 12 {
		settings.MonthsToSearch = defaults.MonthsToSearch
		changed = true
	}
	if settings.MaxFlightTime <= 0 {
		settings.MaxFlightTime = defaults.MaxFlightTime
		changed = true
	}
	if _, ok := normalizeScheduleTime(settings.Schedule.Time); !ok {
		settings.Schedule.Time = defaults.Schedule.Time
		settings.Schedule.Enabled = defaults.Schedule.Enabled
		changed = true
	}

	return changed
}

// normalizeScheduleTime приводит время к формату "HH:MM".
func normalizeScheduleTime(value string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return "", false
	}

	hour, errHour := strconv.Atoi(parts[0])
	minute, errMinute := strconv.Atoi(parts[1])
	if errHour != nil || errMinute != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return "", false
	}

	return fmt.Sprintf("%02d:%02d", hour, minute), true
}

// ScheduleInfo возвращает человекочитаемое описание расписания.
func (s UserSettings) ScheduleInfo() string {
	if !s.Schedule.Enabled {
		return "⏹️ выключен"
	}
	return fmt.Sprintf("✅ ежедневно в <b>%s</b> (время сервера)", s.Schedule.Time)
}

// SetDateRange включает фильтр по диапазону дат.
func (s *UserSettings) SetDateRange(startDate, endDate time.Time) {
	s.DateFilter = DateFilter{
		Enabled:   true,
		Mode:      "range",
		StartDate: startDate,
		EndDate:   endDate,
	}
}

// AddDate добавляет конкретную дату в фильтр-список.
func (s *UserSettings) AddDate(dateStr string) bool {
	if _, err := time.Parse("2006-01-02", dateStr); err != nil {
		return false
	}

	if s.DateFilter.Mode != "list" {
		s.DateFilter = DateFilter{
			Enabled: true,
			Mode:    "list",
			Dates:   []string{},
		}
	}

	for _, existing := range s.DateFilter.Dates {
		if existing == dateStr {
			return false
		}
	}

	s.DateFilter.Dates = append(s.DateFilter.Dates, dateStr)
	s.DateFilter.StartDate = time.Time{}
	s.DateFilter.EndDate = time.Time{}
	return true
}

// RemoveDate удаляет дату из фильтра-списка.
func (s *UserSettings) RemoveDate(dateStr string) bool {
	if s.DateFilter.Mode != "list" {
		return false
	}

	for i, existing := range s.DateFilter.Dates {
		if existing == dateStr {
			s.DateFilter.Dates = append(s.DateFilter.Dates[:i], s.DateFilter.Dates[i+1:]...)
			if len(s.DateFilter.Dates) == 0 {
				s.DateFilter.Enabled = false
			}
			return true
		}
	}

	return false
}

// ClearDateFilter полностью сбрасывает фильтр дат.
func (s *UserSettings) ClearDateFilter() {
	s.DateFilter = DateFilter{}
}

// DisableDateFilter выключает фильтр, сохраняя настройки.
func (s *UserSettings) DisableDateFilter() {
	s.DateFilter.Enabled = false
}

// DateFilterInfo возвращает описание текущего фильтра дат.
func (s UserSettings) DateFilterInfo() string {
	if !s.DateFilter.Enabled {
		return "❌ <b>Фильтр дат:</b> отключен\nПоиск по всем датам."
	}

	switch s.DateFilter.Mode {
	case "range":
		start := "…"
		if !s.DateFilter.StartDate.IsZero() {
			start = s.DateFilter.StartDate.Format("02.01.2006")
		}
		end := "…"
		if !s.DateFilter.EndDate.IsZero() {
			end = s.DateFilter.EndDate.Format("02.01.2006")
		}
		return fmt.Sprintf("📅 <b>Фильтр дат:</b> диапазон\n   с <code>%s</code> по <code>%s</code>", start, end)

	case "list":
		if len(s.DateFilter.Dates) == 0 {
			return "❌ <b>Фильтр дат:</b> пустой список"
		}
		var sb strings.Builder
		for _, date := range s.DateFilter.Dates {
			t, _ := time.Parse("2006-01-02", date)
			sb.WriteString(fmt.Sprintf("\n   • <code>%s</code> (%s)", date, getRussianDayOfWeek(t.Weekday())))
		}
		return "📋 <b>Фильтр дат:</b> конкретные даты" + sb.String()

	default:
		return "❓ <b>Фильтр дат:</b> неизвестный режим"
	}
}
