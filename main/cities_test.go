package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCityStoreCreatesFileFromSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "cities.json")

	store, err := NewCityStore(path, seedCities())
	if err != nil {
		t.Fatalf("NewCityStore: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("файл городов не создан: %v", err)
	}
	if len(store.All()) != len(seedCities()) {
		t.Fatalf("ожидалось %d городов, получено %d", len(seedCities()), len(store.All()))
	}

	codes, name, ok := store.Lookup("пхукет")
	if !ok || name != "Пхукет" || len(codes) != 1 || codes[0] != "HKT" {
		t.Fatalf("Lookup(пхукет) = (%v, %q, %v)", codes, name, ok)
	}

	if got := store.Name("HKT"); got != "Пхукет" {
		t.Fatalf("Name(HKT) = %q", got)
	}
}

func TestCityStoreLookupVariants(t *testing.T) {
	store, err := NewCityStore("", seedCities())
	if err != nil {
		t.Fatalf("NewCityStore: %v", err)
	}

	tests := []struct {
		query string
		want  string
	}{
		{"BKK", "BKK"},
		{"bkk", "BKK"},
		{"Бангкок", "BKK"},
		{"санкт петербург", "LED"},
		{"Санкт-Петербург", "LED"},
		{"ньюйорк", "JFK"},
	}

	for _, tt := range tests {
		codes, _, ok := store.Lookup(tt.query)
		if !ok || len(codes) == 0 || codes[0] != tt.want {
			t.Errorf("Lookup(%q) = %v, want %s", tt.query, codes, tt.want)
		}
	}

	if _, _, ok := store.Lookup("нетакогогорода"); ok {
		t.Fatal("неизвестный город не должен находиться")
	}
}

func TestCityStoreAddAliasRemovePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cities.json")

	store, err := NewCityStore(path, seedCities())
	if err != nil {
		t.Fatalf("NewCityStore: %v", err)
	}

	if err := store.Add(City{Code: "TBS", Name: "Тбилиси"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	city, found, err := store.AddAliases("TBS", []string{"тбилиси", "тифлис"})
	if err != nil || !found {
		t.Fatalf("AddAliases: found=%v err=%v", found, err)
	}
	if len(city.Aliases) != 2 {
		t.Fatalf("ожидалось 2 алиаса без дублей, получено %v", city.Aliases)
	}

	reloaded, err := NewCityStore(path, seedCities())
	if err != nil {
		t.Fatalf("NewCityStore reload: %v", err)
	}

	codes, name, ok := reloaded.Lookup("тифлис")
	if !ok || name != "Тбилиси" || len(codes) != 1 || codes[0] != "TBS" {
		t.Fatalf("Lookup(тифлис) = (%v, %q, %v)", codes, name, ok)
	}

	removed, err := reloaded.Remove("TBS")
	if err != nil || !removed {
		t.Fatalf("Remove: removed=%v err=%v", removed, err)
	}

	again, err := NewCityStore(path, seedCities())
	if err != nil {
		t.Fatalf("NewCityStore reload: %v", err)
	}
	if _, _, ok := again.Lookup("TBS"); ok {
		t.Fatal("удалённый город всё ещё находится")
	}
}

func TestCityStoreHotReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cities.json")

	store, err := NewCityStore(path, seedCities())
	if err != nil {
		t.Fatalf("NewCityStore: %v", err)
	}

	// Имитируем правку файла вручную и меняем mtime в будущее,
	// чтобы перезагрузка не зависела от разрешения файловой системы.
	data := []byte(`[{"code":"TBS","name":"Тбилиси","aliases":["тифлис"]}]`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	codes, name, ok := store.Lookup("тифлис")
	if !ok || name != "Тбилиси" || len(codes) != 1 || codes[0] != "TBS" {
		t.Fatalf("после перезагрузки Lookup(тифлис) = (%v, %q, %v)", codes, name, ok)
	}

	if len(store.All()) != 1 {
		t.Fatalf("после перезагрузки ожидался 1 город, получено %d", len(store.All()))
	}
}
