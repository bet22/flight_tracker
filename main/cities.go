package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// City — запись справочника городов/аэропортов.
type City struct {
	Code    string   `json:"code"`              // IATA-код аэропорта
	Name    string   `json:"name"`              // читаемое название
	Aliases []string `json:"aliases,omitempty"` // варианты названия для поиска
}

// CityStore хранит справочник городов в JSON-файле.
// При каждом чтении проверяет mtime/размер файла и перечитывает его,
// поэтому города можно добавлять/удалять без перезапуска бота.
type CityStore struct {
	mu     sync.RWMutex
	path   string
	cities []City
	byCode map[string]City
	mtime  time.Time
	size   int64
}

var (
	citiesGlobalMu sync.RWMutex
	citiesGlobal   *CityStore

	citiesFallbackOnce sync.Once
	citiesFallback     *CityStore
)

// InitCities инициализирует глобальный справочник городов.
func InitCities(path string, seed []City) error {
	store, err := NewCityStore(path, seed)
	if err != nil {
		return err
	}

	citiesGlobalMu.Lock()
	citiesGlobal = store
	citiesGlobalMu.Unlock()

	return nil
}

// cityStore возвращает глобальный справочник.
// Если он не инициализирован (например, в тестах), используется встроенный.
func cityStore() *CityStore {
	citiesGlobalMu.RLock()
	store := citiesGlobal
	citiesGlobalMu.RUnlock()

	if store != nil {
		return store
	}

	citiesFallbackOnce.Do(func() {
		citiesFallback, _ = NewCityStore("", seedCities())
	})
	return citiesFallback
}

// NewCityStore загружает справочник из файла.
// Если файла нет — создаёт его из seed.
func NewCityStore(path string, seed []City) (*CityStore, error) {
	store := &CityStore{
		path:   path,
		cities: sanitizeCities(seed),
	}
	store.rebuildLocked()

	if path == "" {
		return store, nil
	}

	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		log.Printf("Файл городов %q не найден, создаём из встроенного справочника (%d записей)", path, len(store.cities))
		if err := store.saveLocked(); err != nil {
			return nil, err
		}
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("чтение файла городов %q: %w", path, err)
	}

	if err := store.reloadLocked(); err != nil {
		return nil, err
	}
	store.mtime = info.ModTime()
	store.size = info.Size()

	log.Printf("Загружены города из %q: %d записей", path, len(store.cities))
	return store, nil
}

// All возвращает копию списка городов в порядке файла.
func (s *CityStore) All() []City {
	s.refresh()

	s.mu.RLock()
	defer s.mu.RUnlock()

	return append([]City(nil), s.cities...)
}

// Name возвращает читаемое название по IATA-коду.
func (s *CityStore) Name(code string) string {
	s.refresh()

	s.mu.RLock()
	defer s.mu.RUnlock()

	if city, ok := s.byCode[strings.ToUpper(strings.TrimSpace(code))]; ok {
		return city.Name
	}
	return code
}

// Lookup ищет аэропорты по коду, названию или алиасу.
// Возвращает коды, читаемое имя первого совпадения и признак успеха.
func (s *CityStore) Lookup(query string) ([]string, string, bool) {
	s.refresh()

	s.mu.RLock()
	defer s.mu.RUnlock()

	trimmed := strings.ToUpper(strings.TrimSpace(query))
	if city, ok := s.byCode[trimmed]; ok {
		return []string{city.Code}, city.Name, true
	}

	normalized := normalizeCityQuery(query)
	if normalized == "" {
		return nil, "", false
	}

	// Сначала точное совпадение по алиасу или названию.
	codes := s.matchLocked(normalized, false)
	if len(codes) == 0 {
		// Затем частичное совпадение.
		codes = s.matchLocked(normalized, true)
	}
	if len(codes) == 0 {
		return nil, "", false
	}

	return codes, s.byCode[codes[0]].Name, true
}

// matchLocked собирает коды по алиасам/названиям.
// Вызывать при удерживаемой блокировке.
func (s *CityStore) matchLocked(normalized string, partial bool) []string {
	var codes []string
	seen := make(map[string]bool)

	for _, city := range s.cities {
		if seen[city.Code] || !cityMatches(city, normalized, partial) {
			continue
		}
		seen[city.Code] = true
		codes = append(codes, city.Code)
	}

	return codes
}

func cityMatches(city City, normalized string, partial bool) bool {
	candidates := append([]string{city.Name}, city.Aliases...)
	for _, candidate := range candidates {
		value := normalizeCityQuery(candidate)
		if value == "" {
			continue
		}
		if value == normalized {
			return true
		}
		if partial && (strings.Contains(value, normalized) || strings.Contains(normalized, value)) {
			return true
		}
	}
	return false
}

// Add добавляет город или обновляет существующий по коду.
func (s *CityStore) Add(city City) error {
	s.refresh()
	s.mu.Lock()
	defer s.mu.Unlock()

	city = sanitizeCity(city)
	if city.Code == "" {
		return errors.New("не указан код аэропорта")
	}

	previous := append([]City(nil), s.cities...)

	updated := false
	for i := range s.cities {
		if s.cities[i].Code == city.Code {
			city.Aliases = mergeAliases(s.cities[i].Aliases, city.Aliases)
			s.cities[i] = city
			updated = true
			break
		}
	}
	if !updated {
		s.cities = append(s.cities, city)
	}
	s.rebuildLocked()

	if err := s.saveLocked(); err != nil {
		s.cities = previous
		s.rebuildLocked()
		return err
	}

	return nil
}

// AddAliases добавляет поисковые алиасы существующему городу.
// Возвращает обновлённую запись и признак того, что код найден.
func (s *CityStore) AddAliases(code string, aliases []string) (City, bool, error) {
	s.refresh()
	s.mu.Lock()
	defer s.mu.Unlock()

	code = strings.ToUpper(strings.TrimSpace(code))
	for i := range s.cities {
		if s.cities[i].Code != code {
			continue
		}

		previous := append([]City(nil), s.cities...)
		s.cities[i].Aliases = mergeAliases(s.cities[i].Aliases, aliases)
		s.rebuildLocked()

		if err := s.saveLocked(); err != nil {
			s.cities = previous
			s.rebuildLocked()
			return City{}, false, err
		}

		return s.cities[i], true, nil
	}

	return City{}, false, nil
}

// Remove удаляет город по IATA-коду.
func (s *CityStore) Remove(code string) (bool, error) {
	s.refresh()
	s.mu.Lock()
	defer s.mu.Unlock()

	code = strings.ToUpper(strings.TrimSpace(code))
	index := -1
	for i := range s.cities {
		if s.cities[i].Code == code {
			index = i
			break
		}
	}
	if index < 0 {
		return false, nil
	}

	previous := append([]City(nil), s.cities...)
	s.cities = append(s.cities[:index], s.cities[index+1:]...)
	s.rebuildLocked()

	if err := s.saveLocked(); err != nil {
		s.cities = previous
		s.rebuildLocked()
		return false, err
	}

	return true, nil
}

// refresh перечитывает файл, если он изменился извне.
func (s *CityStore) refresh() {
	if s.path == "" {
		return
	}

	info, err := os.Stat(s.path)
	if err != nil {
		return
	}

	s.mu.RLock()
	unchanged := info.ModTime().Equal(s.mtime) && info.Size() == s.size
	s.mu.RUnlock()
	if unchanged {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	info, err = os.Stat(s.path)
	if err != nil {
		return
	}
	if info.ModTime().Equal(s.mtime) && info.Size() == s.size {
		return
	}

	if err := s.reloadLocked(); err != nil {
		log.Printf("Не удалось перечитать файл городов %q: %v", s.path, err)
		// Запоминаем mtime, чтобы не спамить в лог на каждый запрос.
		s.mtime = info.ModTime()
		s.size = info.Size()
		return
	}

	s.mtime = info.ModTime()
	s.size = info.Size()
	log.Printf("Справочник городов перезагружен из %q: %d записей", s.path, len(s.cities))
}

// reloadLocked читает и разбирает файл. Вызывать при удерживаемой блокировке.
func (s *CityStore) reloadLocked() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("чтение файла городов %q: %w", s.path, err)
	}
	if len(data) == 0 {
		return errors.New("файл городов пуст")
	}

	var loaded []City
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("разбор файла городов %q: %w", s.path, err)
	}

	s.cities = sanitizeCities(loaded)
	s.rebuildLocked()
	return nil
}

// rebuildLocked перестраивает индекс по коду.
func (s *CityStore) rebuildLocked() {
	s.byCode = make(map[string]City, len(s.cities))
	for _, city := range s.cities {
		s.byCode[city.Code] = city
	}
}

// saveLocked записывает справочник атомарно. Вызывать при удерживаемой блокировке.
func (s *CityStore) saveLocked() error {
	if s.path == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("создание каталога городов: %w", err)
	}

	data, err := json.MarshalIndent(s.cities, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация городов: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".cities-*.json")
	if err != nil {
		return fmt.Errorf("создание временного файла городов: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("запись файла городов: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("синхронизация файла городов: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("закрытие файла городов: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("замена файла городов: %w", err)
	}

	if info, err := os.Stat(s.path); err == nil {
		s.mtime = info.ModTime()
		s.size = info.Size()
	}

	return nil
}

// GetCityList возвращает список городов для сообщения.
func (s *CityStore) GetCityList() string {
	cities := s.All()
	sort.Slice(cities, func(i, j int) bool {
		if cities[i].Name == cities[j].Name {
			return cities[i].Code < cities[j].Code
		}
		return cities[i].Name < cities[j].Name
	})

	lines := make([]string, 0, len(cities))
	for _, city := range cities {
		lines = append(lines, fmt.Sprintf("%s — %s", city.Code, city.Name))
	}
	return strings.Join(lines, "\n")
}

// normalizeCityQuery приводит запрос к виду без регистра, пробелов и дефисов.
func normalizeCityQuery(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "ё", "е")
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "")
	return replacer.Replace(value)
}

func sanitizeCities(cities []City) []City {
	result := make([]City, 0, len(cities))
	seen := make(map[string]bool)

	for _, city := range cities {
		city = sanitizeCity(city)
		if city.Code == "" || seen[city.Code] {
			continue
		}
		seen[city.Code] = true
		result = append(result, city)
	}

	return result
}

func sanitizeCity(city City) City {
	city.Code = strings.ToUpper(strings.TrimSpace(city.Code))
	city.Name = strings.TrimSpace(city.Name)
	if city.Name == "" {
		city.Name = city.Code
	}
	city.Aliases = mergeAliases(nil, city.Aliases)
	return city
}

// mergeAliases объединяет алиасы без дублей, сохраняя порядок.
func mergeAliases(existing, extra []string) []string {
	result := make([]string, 0, len(existing)+len(extra))
	seen := make(map[string]bool)

	for _, alias := range append(append([]string(nil), existing...), extra...) {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		key := normalizeCityQuery(alias)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, alias)
	}

	return result
}
