package normalizer

import (
	"regexp"
	"sort"
	"strings"

	"maps-scraper/pkg/model"
)

var (
	// Matches patterns like "Kec. Wiyung", "Kecamatan Dukuh Pakis", "Sawahan", etc.
	kecamatanRegex = regexp.MustCompile(`(?i)(?:Kec\.?|Kecamatan)\s+([a-zA-Z\s]+?)(?:,|$|\d)`)
	cityRegex      = regexp.MustCompile(`(?i)(?:Kota|Kab\.?|Kabupaten)\s+([a-zA-Z\s]+?)(?:,|$|\d)`)
)

// List of 31 official Kecamatan in Kota Surabaya
var SurabayaKecamatans = []string{
	"Asemrowo", "Benowo", "Bubutan", "Bulak", "Dukuh Pakis", "Gayungan", "Genteng",
	"Gubeng", "Gunung Anyar", "Jambangan", "Karangpilang", "Kenjeran",
	"Krembangan", "Lakarsantri", "Mulyorejo", "Pabean Cantian", "Pakal", "Rungkut",
	"Sambikerep", "Sawahan", "Semampir", "Simokerto", "Sukolilo", "Sukomanunggal",
	"Tambaksari", "Tandes", "Tegalsari", "Tenggilis Mejoyo", "Wiyung", "Wonocolo",
	"Wonokromo",
}

// List of official Kecamatan in DKI Jakarta
var JakartaKecamatans = []string{
	// Jakarta Pusat
	"Cempaka Putih", "Gambir", "Johar Baru", "Kemayoran", "Menteng", "Sawah Besar", "Senen", "Tanah Abang",
	// Jakarta Utara
	"Cilincing", "Kelapa Gading", "Koja", "Pademangan", "Penjaringan", "Tanjung Priok",
	// Jakarta Barat
	"Cengkareng", "Grogol Petamburan", "Kalideres", "Kebon Jeruk", "Kembangan", "Palmerah", "Taman Sari", "Tambora",
	// Jakarta Selatan
	"Cilandak", "Jagakarsa", "Kebayoran Baru", "Kebayoran Lama", "Mampang Prapatan", "Pancoran", "Pasar Minggu", "Pesanggrahan", "Setiabudi", "Tebet",
	// Jakarta Timur
	"Cakung", "Cipayung", "Ciracas", "Duren Sawit", "Jatinegara", "Kramat Jati", "Makasar", "Matraman", "Pasar Rebo", "Pulo Gadung",
}

// IsInSurabaya verifies that the address or administrative data is strictly within Kota Surabaya
func IsInSurabaya(address string) bool {
	addrLower := strings.ToLower(address)

	// Reject non-Surabaya regencies / cities explicitly
	rejectRegions := []string{
		"sidoarjo", "gresik", "mojokerto", "lamongan", "pasuruan", "bangkalan",
		"malang", "tuban", "jombang", "kediri", "blitar", "probolinggo", "madiun",
	}
	for _, reg := range rejectRegions {
		if strings.Contains(addrLower, reg) {
			return false
		}
	}

	if strings.Contains(addrLower, "surabaya") {
		return true
	}

	for _, kec := range SurabayaKecamatans {
		if strings.Contains(addrLower, strings.ToLower(kec)) {
			return true
		}
	}

	return false
}

type CityBoundingBox struct {
	MinLat float64
	MaxLat float64
	MinLon float64
	MaxLon float64
}

var CityBoundingBoxes = map[string]CityBoundingBox{
	"Surabaya": {MinLat: -7.365, MaxLat: -7.170, MinLon: 112.600, MaxLon: 112.835},
	"Jakarta":  {MinLat: -6.375, MaxLat: -6.070, MinLon: 106.680, MaxLon: 106.980},
	"Bandung":  {MinLat: -6.970, MaxLat: -6.840, MinLon: 107.540, MaxLon: 107.720},
	"Semarang": {MinLat: -7.090, MaxLat: -6.930, MinLon: 110.300, MaxLon: 110.500},
	"Medan":    {MinLat: 3.480,  MaxLat: 3.780,  MinLon: 98.600,  MaxLon: 98.750},
}

// IsInCityBoundingBox checks if (lat, lon) coordinates fall within the administrative bounding box of the target city
func IsInCityBoundingBox(lat, lon float64, cityName string) bool {
	if lat == 0 || lon == 0 || cityName == "" {
		return true // pass if coordinates unavailable
	}

	for cName, box := range CityBoundingBoxes {
		if strings.EqualFold(cityName, cName) {
			return lat >= box.MinLat && lat <= box.MaxLat && lon >= box.MinLon && lon <= box.MaxLon
		}
	}
	return true
}

// IsInTargetLocation verifies if an address or location text matches the configured geographic target.
// It supports any city, province, country, or custom location string worldwide.
func IsInTargetLocation(address, city, province, country, customLoc string) bool {
	if address == "" {
		return false
	}
	addrLower := strings.ToLower(address)

	// If no specific location configured, pass
	if city == "" && province == "" && country == "" && customLoc == "" {
		return false
	}

	// 1. Explicit rejection of non-target regencies when target is Surabaya
	if strings.EqualFold(city, "Surabaya") || strings.Contains(strings.ToLower(customLoc), "surabaya") {
		rejectRegions := []string{
			"gresik", "sidoarjo", "mojokerto", "lamongan", "pasuruan", "bangkalan",
			"malang", "tuban", "jombang", "kediri", "blitar", "probolinggo", "madiun",
		}
		for _, reg := range rejectRegions {
			if strings.Contains(addrLower, reg) {
				return false
			}
		}
	}

	// 2. Target City check
	if city != "" {
		cityLower := strings.ToLower(city)
		if strings.Contains(addrLower, cityLower) {
			return true
		}
		if strings.EqualFold(city, "Surabaya") {
			for _, kec := range SurabayaKecamatans {
				if strings.Contains(addrLower, strings.ToLower(kec)) {
					return true
				}
			}
			return false // Address has neither "surabaya" nor any Surabaya kecamatan!
		}
	}

	// 3. Custom Location check
	if customLoc != "" {
		customLower := strings.ToLower(customLoc)
		tokens := strings.FieldsFunc(customLower, func(r rune) bool {
			return r == ',' || r == ' ' || r == '-' || r == '/'
		})
		for _, token := range tokens {
			token = strings.TrimSpace(token)
			if len(token) >= 4 && strings.Contains(addrLower, token) {
				return true
			}
		}
	}

	// 4. Province check (ONLY when city is NOT specified)
	if province != "" && city == "" {
		provLower := strings.ToLower(province)
		if strings.Contains(addrLower, provLower) {
			return true
		}
	}

	// 5. Country check (ONLY when city and province are NOT specified)
	if country != "" && city == "" && province == "" {
		countryLower := strings.ToLower(country)
		if strings.Contains(addrLower, countryLower) {
			return true
		}
	}

	return false
}



// IsCementStoreRelevance classifies if the business is strictly a VALID_BUILDING_STORE.
// It strictly rejects tool/machine/hardware shops, electronics, minimarkets, workshops, etc.
func IsCementStoreRelevance(name, address, category string) bool {
	combined := strings.ToLower(name + " " + address + " " + category)
	nameLower := strings.ToLower(name)

	// 1. Strict Exclusion (NON_BUILDING_STORE)
	strictExclusions := []string{
		"alat mesin", "alat teknik", "alat pertukangan", "perkakas", "machinery",
		"engineering tools", "toko teknik", "teknik jaya", "teknik mandiri",
		"mesin jahit", "mesin cuci", "alat pres", "toko sparepart", "spare part",
		"bengkel", "salon", "restoran", "restaurant", "cafe", "kafe", "warung",
		"laundry", "apotek", "klinik", "hotel", "homestay", "fashion", "butik",
		"pakaian", "sepatu", "sekolah", "kursus", "gym", "spa", "minimarket",
		"supermarket", "indomaret", "alfamart", "alfamidi", "toko kelontong",
		"toko sembako", "toko plastik", "toko beras", "toko obat", "elektronik",
		"mebel", "furniture", "mebel jepara", "service center", "servis",
		"toko kaca mobil", "otomotif", "karoseri", "variasi mobil", "variasi motor",
	}

	for _, ex := range strictExclusions {
		if strings.Contains(nameLower, ex) || strings.Contains(combined, ex) {
			// Exception: If name explicitly says "toko bangunan" AND does NOT say "alat mesin" / "alat teknik"
			if strings.Contains(nameLower, "toko bangunan") && !strings.Contains(nameLower, "alat mesin") && !strings.Contains(nameLower, "alat teknik") && !strings.Contains(nameLower, "perkakas") {
				continue
			}
			return false
		}
	}

	// 2. High-confidence positive indicators (VALID_BUILDING_STORE)
	positiveKeywords := []string{
		"toko bangunan", "tb.", "tb ", "toko bahan bangunan", "bahan bangunan",
		"depo bangunan", "mitra10", "mitra 10", "toko semen", "distributor semen",
		"agen semen", "grosir semen", "toko material", "material bangunan",
		"bata ringan", "hebel", "genteng", "pasir cor", "batu bata", "besi beton",
		"baja ringan", "galvalum", "mortar", "semen gresik", "semen tiga roda",
		"semen dynamix", "semen conch", "semen padang", "semen tonasa", "semen singa merah",
		"ud. bangunan", "cv. bangunan", "pt. bangunan", "anugrah bangunan", "hokky bangunan",
		"norton - toko bangunan", "norton - showroom bangunan", "bravo bangunan",
        "toko cat", "toko besi", "toko keramik", "galangan", "toko kayu", "toko triplek",
        "toko gypsum", "toko asbes", "toko pipa", "toko sanitari", "depo", "supermarket bangunan",
	}

	for _, pos := range positiveKeywords {
		if strings.Contains(nameLower, pos) || strings.Contains(combined, pos) {
			return true
		}
	}

	// General building material context
	if strings.Contains(category, "building materials store") || strings.Contains(category, "toko bahan bangunan") {
		return true
	}

	return false
}

// ExtractCity parses Indonesian city/regency names dynamically from address or target city context
func ExtractCity(address, targetCity string) string {
	if address == "" {
		return "UNKNOWN"
	}

	addrLower := strings.ToLower(address)

	// Explicit Regex check "Kota <Name>" or "Kab. <Name>" or "Kabupaten <Name>"
	if matches := cityRegex.FindStringSubmatch(address); len(matches) > 1 {
		cityName := strings.TrimSpace(matches[1])
		cityName = strings.Trim(cityName, ",.- ")
		if len(cityName) > 2 {
			// Resolve against real cities to return canonical name
			allCities := model.GetAllCities()
			resolver := NewResolver(allCities)
			res := resolver.Resolve(cityName)
			if res.CanonicalValue != "" {
				return res.CanonicalValue
			}
			return strings.Title(strings.ToLower(cityName))
		}
	}

	// 3. Tokenized resolution (Best for Google Maps format: "Street, Kecamatan, City, Province")
	allCities := model.GetAllCities()
	resolver := NewResolver(allCities)
	
	parts := strings.Split(address, ",")
	for i := len(parts) - 1; i >= 0; i-- { // Iterate from end (Province -> City -> Kecamatan)
		part := strings.TrimSpace(parts[i])
		if len(part) < 3 {
			continue
		}
		res := resolver.Resolve(part)
		// We use 0.85 threshold to accept fuzzy/abbreviated city names (like Toba -> Toba Samosir)
		if res.CanonicalValue != "" && res.Confidence >= 0.85 {
			return res.CanonicalValue
		}
	}

	// 4. Whole string word boundary fallback (in case it's not comma separated)
	for _, city := range allCities {
		cityNorm := resolver.normalizeString(city)
		if len(cityNorm) < 4 {
			continue // Prevent false positives on short words like "Batu"
		}
		pattern := `(?i)\b` + regexp.QuoteMeta(cityNorm) + `\b`
		if matched, _ := regexp.MatchString(pattern, addrLower); matched {
			return city
		}
	}

	// 4. Check explicitly known Indonesian cities (Hardcoded fallback for extremely short/corrupted names)
	knownCities := []string{
		"Surabaya", "Jakarta", "Bandung", "Semarang", "Medan", "Makassar",
		"Palembang", "Tangerang", "Depok", "Bekasi", "Bogor", "Batam",
		"Pekanbaru", "Bandar Lampung", "Malang", "Yogyakarta", "Solo",
		"Surakarta", "Denpasar", "Balikpapan", "Samarinda", "Banjarmasin",
		"Pontianak", "Manado", "Mataram", "Kupang", "Jayapura", "Ambon",
		"Cirebon", "Sukabumi", "Tasikmalaya", "Magelang", "Kediri", "Blitar",
		"Probolinggo", "Pasuruan", "Mojokerto", "Madiun", "Batu", "Sidoarjo", "Gresik",
	}

	for _, c := range knownCities {
		if strings.Contains(addrLower, strings.ToLower(c)) {
			return c
		}
	}


	return "UNKNOWN"
}


// Mapping of kelurahan aliases and full names
var surabayaKelurahanMap = map[string]string{
	"gn. anyar":       "Gunung Anyar", "gn anyar": "Gunung Anyar", "gununganyar": "Gunung Anyar",
	"karang pilang":   "Karangpilang", "karangpilang": "Karangpilang",
	"dukuhpakis":      "Dukuh Pakis", "dukuh pakis": "Dukuh Pakis",
	"tenggilis mejoyo": "Tenggilis Mejoyo", "tenggilis": "Tenggilis Mejoyo",
	"pabean cantian":  "Pabean Cantian", "pabean cantikan": "Pabean Cantian", "pabean": "Pabean Cantian",
	"pacar keling": "Tambaksari", "pacarkeling": "Tambaksari", "ploso": "Tambaksari", "gading": "Tambaksari", "rangkah": "Tambaksari", "dukuh setro": "Tambaksari",
	"lontar": "Sambikerep", "sambikerep": "Sambikerep", "made": "Sambikerep", "bringin": "Sambikerep",
	"margorejo": "Wonocolo", "bendul merisi": "Wonocolo", "sidosermo": "Wonocolo", "siwalankerto": "Wonocolo", "jemur wonosari": "Wonocolo",
	"sawahan": "Sawahan", "petemon": "Sawahan", "kupang krajan": "Sawahan", "banyu urip": "Sawahan", "pakis": "Sawahan", "putat jaya": "Sawahan",
	"keputih": "Sukolilo", "gebang putih": "Sukolilo", "klampis ngasem": "Sukolilo", "menur pumpungan": "Sukolilo", "nginden jangkungan": "Sukolilo", "semolowaru": "Sukolilo", "medokan semampir": "Sukolilo",
	"rungkut kidul": "Rungkut", "rungkut lor": "Rungkut", "kalirungkut": "Rungkut", "kali rungkut": "Rungkut", "kedung baruk": "Rungkut", "wonorejo": "Rungkut", "medokan ayu": "Rungkut", "penjaringan sari": "Rungkut",
	"kebraon": "Karangpilang", "kedurus": "Karangpilang", "warugunung": "Karangpilang",
	"wiyung": "Wiyung", "babatan": "Wiyung", "balas klumprik": "Wiyung", "jajar tunggal": "Wiyung",
	"lidah wetan": "Lakarsantri", "lidah kulon": "Lakarsantri", "bangkingan": "Lakarsantri", "jeruk": "Lakarsantri", "sumur welut": "Lakarsantri",
	"manukan kulon": "Tandes", "manukan wetan": "Tandes", "banjar sugihan": "Tandes", "karang pohon": "Tandes", "balongsari": "Tandes", "tandes": "Tandes",
	"sukomanunggal": "Sukomanunggal", "tanjungsari": "Sukomanunggal", "sono kwijenan": "Sukomanunggal", "simomulyo": "Sukomanunggal",
	"babat jerawat": "Pakal", "pakal": "Pakal", "benowo": "Pakal", "sumberejo": "Pakal",
	"sememi": "Benowo", "kandangan": "Benowo", "romokalisari": "Benowo", "tambak osowilangon": "Benowo",
	"asemrowo": "Asemrowo", "genting kalianak": "Asemrowo", "tambak sarioso": "Asemrowo",
	"krembangan selatan": "Krembangan", "krembangan utara": "Krembangan", "kemayoran": "Krembangan", "perak barat": "Krembangan", "morokrembangan": "Krembangan",
	"perak timur": "Pabean Cantian", "perak utara": "Pabean Cantian", "bongkaran": "Pabean Cantian", "nyamplungan": "Pabean Cantian", "krembangan timur": "Pabean Cantian",
	"sidotopo": "Semampir", "pegirian": "Semampir", "ampel": "Semampir", "ujung": "Semampir", "wonokusumo": "Semampir",
	"simokerto": "Simokerto", "sidodadi": "Simokerto", "simolawang": "Simokerto", "kapasan": "Simokerto", "tambakrejo": "Simokerto",
	"kenjeran": "Bulak", "bulak": "Bulak", "kedung cowek": "Bulak", "sukolilo baru": "Bulak",
	"tanah kali kedinding": "Kenjeran", "sidotopo wetan": "Kenjeran", "tambakwedi": "Kenjeran",
	"mulyorejo": "Mulyorejo", "kalisari": "Mulyorejo", "dukuh sutorejo": "Mulyorejo", "kalijudan": "Mulyorejo", "kejawan putih tambak": "Mulyorejo", "manyar sabrangan": "Mulyorejo",
	"gubeng": "Gubeng", "mojokerto": "Gubeng", "airlangga": "Gubeng", "baratajaya": "Gubeng", "pucang sewu": "Gubeng", "kertajaya": "Gubeng",
	"tegalsari": "Tegalsari", "wonorejo tegalsari": "Tegalsari", "kedungdoro": "Tegalsari", "keputran": "Tegalsari", "dr. soetomo": "Tegalsari",
	"genteng": "Genteng", "embong kaliasin": "Genteng", "ketabang": "Genteng", "kapasari": "Genteng", "peneleh": "Genteng",
	"bubutan": "Bubutan", "alun-alun contong": "Bubutan", "jepara": "Bubutan", "gunung sari": "Dukuh Pakis", "dukuh kupang": "Dukuh Pakis", "pradah kalikendal": "Dukuh Pakis",
	"gayungan": "Gayungan", "ketintang": "Gayungan", "menanggal": "Gayungan", "dukuhtang": "Gayungan",
	"jambangan": "Jambangan", "karah": "Jambangan", "kebonsari": "Jambangan", "pagesangan": "Jambangan",
	"gunung anyar": "Gunung Anyar", "rungkut menanggal": "Gunung Anyar", "rungkut tengah": "Gunung Anyar", "gunung anyar tambak": "Gunung Anyar",
	"darmo": "Wonokromo", "sawunggaling": "Wonokromo", "jagir": "Wonokromo", "ngagel": "Wonokromo", "ngagelrejo": "Wonokromo",
}

var jakartaKelurahanMap = map[string]string{
	// Jakarta Pusat
	"cempaka putih": "Cempaka Putih", "gambir": "Gambir", "johar baru": "Johar Baru", "kemayoran": "Kemayoran", 
	"menteng": "Menteng", "sawah besar": "Sawah Besar", "senen": "Senen", "tanah abang": "Tanah Abang",
	"cikini": "Menteng", "gondangdia": "Menteng", "kebon sirih": "Menteng", "pegangsaan": "Menteng",
	"bungur": "Senen", "kenari": "Senen", "kramat": "Senen", "kwitang": "Senen", "paseban": "Senen",
	"kartini": "Sawah Besar", "karang anyar": "Sawah Besar", "mangga dua selatan": "Sawah Besar", "pasar baru": "Sawah Besar",
	"bendungan hilir": "Tanah Abang", "karet tengsin": "Tanah Abang", "kebon kacang": "Tanah Abang", "kebon melati": "Tanah Abang", "petamburan": "Tanah Abang", "gelora": "Tanah Abang",
	
	// Jakarta Selatan
	"cilandak": "Cilandak", "jagakarsa": "Jagakarsa", "kebayoran baru": "Kebayoran Baru", "kebayoran lama": "Kebayoran Lama", 
	"mampang prapatan": "Mampang Prapatan", "pancoran": "Pancoran", "pasar minggu": "Pasar Minggu", "pesanggrahan": "Pesanggrahan", 
	"setiabudi": "Setiabudi", "tebet": "Tebet",
	"cipete": "Cilandak", "gandaria": "Cilandak", "lebak bulus": "Cilandak", "pondok labu": "Cilandak",
	"ciganjur": "Jagakarsa", "cipedak": "Jagakarsa", "lenteng agung": "Jagakarsa", "srengseng sawah": "Jagakarsa", "tanjung barat": "Jagakarsa",
	"cipete utara": "Kebayoran Baru", "gandaria utara": "Kebayoran Baru", "gunung": "Kebayoran Baru", "kramat pela": "Kebayoran Baru", "melawai": "Kebayoran Baru", "petogogan": "Kebayoran Baru", "pulo": "Kebayoran Baru", "rawa barat": "Kebayoran Baru", "selong": "Kebayoran Baru", "senayan": "Kebayoran Baru",
	"cipulir": "Kebayoran Lama", "grogol selatan": "Kebayoran Lama", "grogol utara": "Kebayoran Lama", "pondok pinang": "Kebayoran Lama",
	"bangka": "Mampang Prapatan", "kuningan barat": "Mampang Prapatan", "pela mampang": "Mampang Prapatan", "tegal parang": "Mampang Prapatan",
	"cikoko": "Pancoran", "duren tiga": "Pancoran", "kalibata": "Pancoran", "pengadegan": "Pancoran", "rawa jati": "Pancoran",
	"cilandak timur": "Pasar Minggu", "jati padang": "Pasar Minggu", "kebagusan": "Pasar Minggu", "pejaten": "Pasar Minggu", "ragunan": "Pasar Minggu",
	"bintaro": "Pesanggrahan", "petukangan": "Pesanggrahan", "ulujami": "Pesanggrahan",
	"guntur": "Setiabudi", "karet": "Setiabudi", "kuningan timur": "Setiabudi", "menteng atas": "Setiabudi", "pasar manggis": "Setiabudi",
	"bukit duri": "Tebet", "kebon baru": "Tebet", "manggarai": "Tebet", "menteng dalam": "Tebet",
	
	// Jakarta Barat
	"cengkareng": "Cengkareng", "grogol petamburan": "Grogol Petamburan", "kalideres": "Kalideres", "kebon jeruk": "Kebon Jeruk", 
	"kembangan": "Kembangan", "palmerah": "Palmerah", "taman sari": "Taman Sari", "tambora": "Tambora",
	"duri kosambi": "Cengkareng", "kapuk": "Cengkareng", "kedaung kali angke": "Cengkareng", "rawa buaya": "Cengkareng",
	"grogol": "Grogol Petamburan", "jelambar": "Grogol Petamburan", "tanjung duren": "Grogol Petamburan", "tomang": "Grogol Petamburan", "wijaya kusuma": "Grogol Petamburan",
	"kamal": "Kalideres", "pegadungan": "Kalideres", "semanan": "Kalideres", "tegal alur": "Kalideres",
	"duri kepa": "Kebon Jeruk", "kedoya": "Kebon Jeruk", "kelapa dua": "Kebon Jeruk", "sukabumi utara": "Kebon Jeruk", "sukabumi selatan": "Kebon Jeruk",
	"joglo": "Kembangan", "meruya": "Kembangan", "srengseng": "Kembangan",
	"jatipulo": "Palmerah", "kemanggisan": "Palmerah", "kota bambu": "Palmerah", "slipi": "Palmerah",
	"glodok": "Taman Sari", "keagungan": "Taman Sari", "krukut": "Taman Sari", "mangga besar": "Taman Sari", "maphar": "Taman Sari", "pinangsia": "Taman Sari", "tangki": "Taman Sari",
	"angke": "Tambora", "duri selatan": "Tambora", "duri utara": "Tambora", "jembatan besi": "Tambora", "jembatan lima": "Tambora", "kali anyar": "Tambora", "krendang": "Tambora", "pekojan": "Tambora", "roa malaka": "Tambora", "tanah sereal": "Tambora",
	
	// Jakarta Timur
	"cakung": "Cakung", "cipayung": "Cipayung", "ciracas": "Ciracas", "duren sawit": "Duren Sawit", "jatinegara": "Jatinegara", 
	"kramat jati": "Kramat Jati", "makasar": "Makasar", "matraman": "Matraman", "pasar rebo": "Pasar Rebo", "pulo gadung": "Pulo Gadung",
	"penggilingan": "Cakung", "pulo gebang": "Cakung", "rawa terate": "Cakung", "ujung menteng": "Cakung",
	"bambu apus": "Cipayung", "ceger": "Cipayung", "cilangkap": "Cipayung", "lubang buaya": "Cipayung", "munjul": "Cipayung", "pondok ranggon": "Cipayung", "setu": "Cipayung",
	"cibubur": "Ciracas", "kelapa dua wetan": "Ciracas", "rambutan": "Ciracas", "susukan": "Ciracas",
	"klender": "Duren Sawit", "malaka jaya": "Duren Sawit", "malaka sari": "Duren Sawit", "pondok bambu": "Duren Sawit", "pondok kelapa": "Duren Sawit", "pondok kopi": "Duren Sawit",
	"bali mester": "Jatinegara", "bidara cina": "Jatinegara", "cipinang cempedak": "Jatinegara", "cipinang besar": "Jatinegara", "cipinang muara": "Jatinegara", "kampung melayu": "Jatinegara", "rawa bunga": "Jatinegara",
	"bale kambang": "Kramat Jati", "batu ampar": "Kramat Jati", "cawang": "Kramat Jati", "cililitan": "Kramat Jati", "dukuh": "Kramat Jati", "tengah": "Kramat Jati",
	"halim perdana kusuma": "Makasar", "kebon pala": "Makasar", "pinang ranti": "Makasar", "cipinang melayu": "Makasar",
	"kayu manis": "Matraman", "kebon manggis": "Matraman", "pal meriam": "Matraman", "pisangan baru": "Matraman", "utan kayu": "Matraman",
	"baru": "Pasar Rebo", "cijantung": "Pasar Rebo", "gedong": "Pasar Rebo", "kalisari": "Pasar Rebo", "pekayon": "Pasar Rebo",
	"cipinang": "Pulo Gadung", "jati": "Pulo Gadung", "kayu putih": "Pulo Gadung", "pisangan timur": "Pulo Gadung", "rawamangun": "Pulo Gadung",

	// Jakarta Utara
	"cilincing": "Cilincing", "kelapa gading": "Kelapa Gading", "koja": "Koja", "pademangan": "Pademangan", "penjaringan": "Penjaringan", "tanjung priok": "Tanjung Priok",
	"kali baru": "Cilincing", "marunda": "Cilincing", "rorotan": "Cilincing", "semper": "Cilincing", "sukapura": "Cilincing",
	"pegangsaan dua": "Kelapa Gading",
	"rawa badak": "Koja", "tugu utara": "Koja", "tugu selatan": "Koja", "lagoa": "Koja",
	"ancol": "Pademangan",
	"kamal muara": "Penjaringan", "kapuk muara": "Penjaringan", "pejagalan": "Penjaringan", "pluit": "Penjaringan",
	"kebon bawang": "Tanjung Priok", "papanggo": "Tanjung Priok", "sungai bambu": "Tanjung Priok", "sunter": "Tanjung Priok", "warakas": "Tanjung Priok",
}

var sortedSurabayaKeys []string
var sortedJakartaKeys []string

func init() {
	for k := range surabayaKelurahanMap {
		sortedSurabayaKeys = append(sortedSurabayaKeys, k)
	}
	for k := range jakartaKelurahanMap {
		sortedJakartaKeys = append(sortedJakartaKeys, k)
	}
	
	// Sort by length descending to match longest kelurahan first
	sortFn := func(keys []string) func(i, j int) bool {
		return func(i, j int) bool {
			if len(keys[i]) == len(keys[j]) {
				return keys[i] < keys[j]
			}
			return len(keys[i]) > len(keys[j])
		}
	}
	sort.Slice(sortedSurabayaKeys, sortFn(sortedSurabayaKeys))
	sort.Slice(sortedJakartaKeys, sortFn(sortedJakartaKeys))
}


