package main

// seedCities — встроенный справочник городов.
// Используется только при первом запуске: далее бот читает и пишет
// JSON-файл (CITIES_FILE), который можно править без перезапуска.
func seedCities() []City {
	return []City{
		// Азия
		{Code: "DPS", Name: "Денпасар (Бали)", Aliases: []string{"бали", "денпасар"}},
		{Code: "BKK", Name: "Бангкок", Aliases: []string{"бангкок"}},
		{Code: "HKT", Name: "Пхукет", Aliases: []string{"пхукет"}},
		{Code: "SIN", Name: "Сингапур", Aliases: []string{"сингапур"}},
		{Code: "KUL", Name: "Куала-Лумпур", Aliases: []string{"куалалумпур", "куала-лумпур"}},
		{Code: "HAN", Name: "Ханой", Aliases: []string{"ханой"}},
		{Code: "SGN", Name: "Хошимин", Aliases: []string{"хошимин"}},
		{Code: "DAD", Name: "Дананг", Aliases: []string{"дананг"}},
		{Code: "CXR", Name: "Нячанг", Aliases: []string{"нячанг"}},
		{Code: "PQC", Name: "Фукуок", Aliases: []string{"фукуок"}},
		{Code: "NRT", Name: "Токио (Нарита)", Aliases: []string{"токио"}},
		{Code: "HND", Name: "Токио (Ханеда)", Aliases: []string{"токио"}},
		{Code: "ICN", Name: "Сеул", Aliases: []string{"сеул"}},
		{Code: "GMP", Name: "Сеул (Кимпхо)", Aliases: []string{"сеул"}},
		{Code: "PEK", Name: "Пекин", Aliases: []string{"пекин"}},
		{Code: "PVG", Name: "Шанхай", Aliases: []string{"шанхай"}},
		{Code: "DEL", Name: "Дели", Aliases: []string{"дели"}},
		{Code: "DXB", Name: "Дубай", Aliases: []string{"дубай"}},
		{Code: "IST", Name: "Стамбул", Aliases: []string{"стамбул"}},
		{Code: "SYD", Name: "Сидней", Aliases: []string{"сидней"}},
		{Code: "AKL", Name: "Окленд", Aliases: []string{"окленд"}},

		// Европа
		{Code: "FRA", Name: "Франкфурт", Aliases: []string{"франкфурт"}},
		{Code: "CDG", Name: "Париж (Шарль-де-Голль)", Aliases: []string{"париж"}},
		{Code: "ORY", Name: "Париж (Орли)", Aliases: []string{"париж"}},
		{Code: "LHR", Name: "Лондон (Хитроу)", Aliases: []string{"лондон"}},
		{Code: "LGW", Name: "Лондон (Гатвик)", Aliases: []string{"лондон"}},
		{Code: "STN", Name: "Лондон (Станстед)", Aliases: []string{"лондон"}},
		{Code: "BER", Name: "Берлин", Aliases: []string{"берлин"}},
		{Code: "AMS", Name: "Амстердам", Aliases: []string{"амстердам"}},
		{Code: "PRG", Name: "Прага", Aliases: []string{"прага"}},
		{Code: "FCO", Name: "Рим", Aliases: []string{"рим"}},
		{Code: "MXP", Name: "Милан (Мальпенса)", Aliases: []string{"милан"}},
		{Code: "LIN", Name: "Милан (Линате)", Aliases: []string{"милан"}},
		{Code: "MAD", Name: "Мадрид", Aliases: []string{"мадрид"}},
		{Code: "BCN", Name: "Барселона", Aliases: []string{"барселона"}},
		{Code: "VIE", Name: "Вена", Aliases: []string{"вена"}},
		{Code: "WAW", Name: "Варшава", Aliases: []string{"варшава"}},

		// Америка
		{Code: "JFK", Name: "Нью-Йорк (Кеннеди)", Aliases: []string{"нью-йорк", "ньюйорк"}},
		{Code: "LGA", Name: "Нью-Йорк (Ла-Гуардиа)", Aliases: []string{"нью-йорк", "ньюйорк"}},
		{Code: "EWR", Name: "Нью-Йорк (Ньюарк)", Aliases: []string{"нью-йорк", "ньюйорк"}},
		{Code: "LAX", Name: "Лос-Анджелес", Aliases: []string{"лос-анджелес"}},
		{Code: "MIA", Name: "Майами", Aliases: []string{"майами"}},
		{Code: "ORD", Name: "Чикаго (О'Хара)", Aliases: []string{"чикаго"}},
		{Code: "MDW", Name: "Чикаго (Мидуэй)", Aliases: []string{"чикаго"}},
		{Code: "YYZ", Name: "Торонто", Aliases: []string{"торонто"}},
		{Code: "YVR", Name: "Ванкувер", Aliases: []string{"ванкувер"}},

		// Россия и СНГ
		{Code: "SVO", Name: "Москва (Шереметьево)", Aliases: []string{"москва"}},
		{Code: "DME", Name: "Москва (Домодедово)", Aliases: []string{"москва"}},
		{Code: "VKO", Name: "Москва (Внуково)", Aliases: []string{"москва"}},
		{Code: "LED", Name: "Санкт-Петербург", Aliases: []string{"санктпетербург", "санкт-петербург", "петербург"}},
		{Code: "SVX", Name: "Екатеринбург", Aliases: []string{"екатеринбург"}},
		{Code: "KJA", Name: "Красноярск", Aliases: []string{"красноярск"}},
		{Code: "IKT", Name: "Иркутск", Aliases: []string{"иркутск"}},
		{Code: "VVO", Name: "Владивосток", Aliases: []string{"владивосток"}},
		{Code: "KHV", Name: "Хабаровск", Aliases: []string{"хабаровск"}},
		{Code: "ALA", Name: "Алматы", Aliases: []string{"алматы"}},
		{Code: "TAS", Name: "Ташкент", Aliases: []string{"ташкент"}},
		{Code: "FRU", Name: "Бишкек", Aliases: []string{"бишкек"}},
		{Code: "OVB", Name: "Новосибирск", Aliases: []string{"новосибирск"}},
		{Code: "BAX", Name: "Барнаул", Aliases: []string{"барнаул"}},
	}
}
