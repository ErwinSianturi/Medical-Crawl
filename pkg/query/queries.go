package query

import (
	"fmt"
	"strings"
	"maps-scraper/pkg/model"
	"maps-scraper/pkg/normalizer"
)

// GetAllQueries returns the complete list of exactly 2865 granular discovery queries.
// Query numbering is 1-indexed (1 to 2865).
func GetAllQueries() []string {
	var queries []string

	// 1. Variations across 31 official Surabaya Kecamatans (67 variations * 31 kecamatans = 2077 queries)
	variations := []string{
		// Generic building & hardware (9)
		"toko bangunan", "toko bahan bangunan", "toko material", "toko alat bangunan",
		"toko besi", "toko cat", "toko keramik", "galangan", "toko pipa",

		// Cement & brand variations (26)
		"toko semen", "toko jual semen", "distributor semen", "agen semen", "grosir semen",
		"semen gresik", "semen tiga roda", "semen dynamix", "semen conch", "semen singa merah",
		"semen holcim", "semen padang", "semen tonasa", "semen bosowa", "semen merah putih",
		"semen mortar", "mortar utama", "drymix", "semen instan", "semen acian",
		"toko bangunan jual semen", "toko material semen", "depo semen", "pemasok semen",
		"toko pasir dan semen", "toko bata ringan dan semen",

		// Business Entities: UD / CV / TB / PT (32)
		"UD semen", "CV semen", "TB semen", "PT semen",
		"UD distributor semen", "CV distributor semen", "TB distributor semen", "PT distributor semen",
		"UD bangunan", "CV bangunan", "TB bangunan", "PT bangunan",
		"UD bahan bangunan", "CV bahan bangunan", "TB bahan bangunan", "PT bahan bangunan",
		"UD material", "CV material", "TB material", "PT material",
		"UD grosir semen", "CV grosir semen", "TB grosir semen", "PT grosir semen",
		"UD agen semen", "CV agen semen", "TB agen semen", "PT agen semen",
		"UD supplier semen", "CV supplier semen", "TB supplier semen", "PT supplier semen",
	}

	for _, kec := range normalizer.SurabayaKecamatans {
		for _, v := range variations {
			queries = append(queries, fmt.Sprintf("%s %s surabaya", v, kec))
		}
	}

	// 2. Kelurahan-level targeted queries (73 kelurahans * 6 variations = 438 queries)
	kelurahans := []string{
		"rungkut", "medokan", "wonokromo", "ngagel", "darmo", "sawahan", "banyu urip",
		"petemon", "wiyung", "babatan", "lakarsantri", "lidah wetan", "lidah kulon",
		"tandes", "manukan", "balongsari", "sukomanunggal", "tanjungsari", "simomulyo",
		"benowo", "sememi", "pakal", "babat jerawat", "kenjeran", "kedinding",
		"tambakwedi", "bulak", "mulyorejo", "kalisari", "sutorejo", "gubeng", "kertajaya",
		"pucang", "airlangga", "tegalsari", "kedungdoro", "keputran", "genteng",
		"kapasari", "peneleh", "bubutan", "baliwerti", "krembangan", "perak",
		"pabean cantian", "bongkaran", "semampir", "ampel", "pegirian", "sidotopo",
		"simokerto", "tambakrejo", "tambaksari", "ploso", "pacar keling", "sukolilo",
		"keputih", "semolowaru", "klampis", "menur", "karangpilang", "kebraon",
		"kedurus", "jambangan", "pagesangan", "karah", "gayungan", "ketintang",
		"menanggal", "wonocolo", "jemursari", "margorejo", "sidosermo",
	}

	for _, kel := range kelurahans {
		queries = append(queries, fmt.Sprintf("toko bangunan %s surabaya", kel))
		queries = append(queries, fmt.Sprintf("toko material %s surabaya", kel))
		queries = append(queries, fmt.Sprintf("toko semen %s surabaya", kel))
		queries = append(queries, fmt.Sprintf("toko besi %s surabaya", kel))
		queries = append(queries, fmt.Sprintf("galangan %s surabaya", kel))
		queries = append(queries, fmt.Sprintf("toko cat %s surabaya", kel))
	}

	// 3. Major Street level queries (70 streets * 5 variations = 350 queries)
	streets := []string{
		"baliwerti", "raden saleh", "kedungdoro", "kalianak", "margomulyo", "gembong", "kapasan",
		"duping", "kalibutuh", "tidar", "petemon", "banyu urip", "dharmawangsa", "kertajaya",
		"ngagel", "ngagel jaya", "jagir wonokromo", "ahmad yani", "jemursari", "prapen", "panjang jiwo",
		"rungkut industri", "rungkut madya", "rungkut asri", "gunung anyar timur", "wiguna", "medokan asri",
		"raya menganti", "wiyung", "babatan unesa", "lidah wetan", "lidah kulon", "lontar", "citraland",
		"sukomanunggal jaya", "tanjungsari", "simomulyo baru", "tandes lor", "manukan tama", "manukan tengah",
		"raya benowo", "klakah rejo", "sememi jaya", "pakal madya", "mastrip karangpilang", "kebraon utama",
		"kedurus", "pagesangan", "kebonsari", "ketintang madya", "gayungsari", "kutisari", "kendangsari",
		"sidosermo airdas", "bendul merisi", "raya mulyosari", "kalijudan", "kenjeran", "kedung cowek",
		"bulak banteng", "tanah kali kedinding", "sidotopo wetan", "pegirian", "nyamplungan", "rajawali",
		"krembangan barat", "gresik gadukan", "kalianak barat", "tambak mayor", "demak",
	}

	for _, st := range streets {
		queries = append(queries, fmt.Sprintf("toko bangunan jl %s surabaya", st))
		queries = append(queries, fmt.Sprintf("toko semen jl %s surabaya", st))
		queries = append(queries, fmt.Sprintf("toko material jl %s surabaya", st))
		queries = append(queries, fmt.Sprintf("toko besi jl %s surabaya", st))
		queries = append(queries, fmt.Sprintf("galangan jl %s surabaya", st))
	}

	return queries
}

// DefaultVariations returns the comprehensive list of 67 business, brand, and category variations
var DefaultVariations = []string{
	// Generic building & hardware (9)
	"toko bangunan", "toko bahan bangunan", "toko material", "toko alat bangunan",
	"toko besi", "toko cat", "toko keramik", "galangan", "toko pipa",

	// Cement & brand variations (26)
	"toko semen", "toko jual semen", "distributor semen", "agen semen", "grosir semen",
	"semen gresik", "semen tiga roda", "semen dynamix", "semen conch", "semen singa merah",
	"semen holcim", "semen padang", "semen tonasa", "semen bosowa", "semen merah putih",
	"semen mortar", "mortar utama", "drymix", "semen instan", "semen acian",
	"toko bangunan jual semen", "toko material semen", "depo semen", "pemasok semen",
	"toko pasir dan semen", "toko bata ringan dan semen",

	// Business Entities: UD / CV / TB / PT (32)
	"UD semen", "CV semen", "TB semen", "PT semen",
	"UD distributor semen", "CV distributor semen", "TB distributor semen", "PT distributor semen",
	"UD bangunan", "CV bangunan", "TB bangunan", "PT bangunan",
	"UD bahan bangunan", "CV bahan bangunan", "TB bahan bangunan", "PT bahan bangunan",
	"UD material", "CV material", "TB material", "PT material",
	"UD grosir semen", "CV grosir semen", "TB grosir semen", "PT grosir semen",
	"UD agen semen", "CV agen semen", "TB agen semen", "PT agen semen",
	"UD supplier semen", "CV supplier semen", "TB supplier semen", "PT supplier semen",
}

// GenerateMultiRegionQueries generates a list of targeted discovery search queries for any location and target.
// It automatically constructs granular queries across districts/areas producing >= 2,000 queries if data allows.
func GenerateMultiRegionQueries(cfg model.JobConfig) []string {
	cityLower := strings.ToLower(cfg.Location.City)
	targetLower := strings.ToLower(cfg.Target)
	isBuildingCement := (targetLower == "" || strings.Contains(targetLower, "bangunan") || strings.Contains(targetLower, "semen") || strings.Contains(targetLower, "material"))
	
	// 1. If Surabaya and building/cement store target, return the complete 2865 granular discovery queries
	if (strings.Contains(cityLower, "surabaya")) && isBuildingCement {
		return GetAllQueries()
	}

	// 2. Extract base search terms
	var baseTerms []string
	if len(cfg.Queries) > 0 {
		baseTerms = cfg.Queries
	} else if cfg.Target != "" {
		for _, bt := range strings.Split(cfg.Target, ",") {
			btTrim := strings.TrimSpace(bt)
			if btTrim != "" {
				baseTerms = append(baseTerms, btTrim)
			}
		}
	}

	if isBuildingCement && len(baseTerms) <= 5 {
		// Use comprehensive 67 variations for building/cement
		baseTerms = DefaultVariations
	} else if len(baseTerms) == 0 {
		baseTerms = []string{
			"toko semen", "toko bangunan", "supplier semen", "distributor semen",
			"toko material bangunan", "grosir semen", "agen semen", "depo semen",
		}
	}

	// 3. Resolve geographic areas (districts / sub-districts)
	var districts []string
	if cfg.Location.City != "" {
		districts = model.GetDistrictsForCity(cfg.Location.City)
	}
	if len(districts) == 0 && cfg.Location.Province != "" {
		districts = model.GetDistrictsForProvince(cfg.Location.Province)
	}

	// 4. Build expanded query pool
	var queries []string
	seen := make(map[string]bool)

	cityLabel := cfg.Location.City
	if cityLabel == "" {
		cityLabel = cfg.Location.Province
	}
	if cityLabel == "" {
		cityLabel = cfg.Location.Country
	}
	if cityLabel == "" {
		cityLabel = "Indonesia"
	}

	if len(districts) > 0 {
		// District-level expansion
		for _, dist := range districts {
			for _, term := range baseTerms {
				q := fmt.Sprintf("%s %s %s", term, dist, cityLabel)
				q = strings.Join(strings.Fields(q), " ")
				if !seen[q] {
					seen[q] = true
					queries = append(queries, q)
				}
			}
		}

		// Additional prefix/qualifier permutations if pool is under 2000 to maximize coverage
		if len(queries) < 2000 && isBuildingCement {
			prefixes := []string{"toko", "agen", "distributor", "grosir", "pusat", "depo", "supplier", "jual"}
			coreProducts := []string{"semen", "material", "bangunan", "bata ringan", "besi", "cat", "pasir", "mortar", "keramik", "pipa"}
			entityTypes := []string{"", "UD", "TB", "CV", "PT"}
			
			for _, dist := range districts {
				for _, ent := range entityTypes {
					for _, pfx := range prefixes {
						for _, prd := range coreProducts {
							var q string
							if ent != "" {
								q = fmt.Sprintf("%s %s %s %s %s", ent, pfx, prd, dist, cityLabel)
							} else {
								q = fmt.Sprintf("%s %s %s %s", pfx, prd, dist, cityLabel)
							}
							q = strings.Join(strings.Fields(q), " ")
							if !seen[q] {
								seen[q] = true
								queries = append(queries, q)
							}
							if len(queries) >= 3000 {
								break
							}
						}
						if len(queries) >= 3000 { break }
					}
					if len(queries) >= 3000 { break }
				}
				if len(queries) >= 3000 { break }
			}
		}
	} else {
		// Fallback when no districts are registered in DB
		for _, term := range baseTerms {
			q := fmt.Sprintf("%s %s", term, cityLabel)
			q = strings.Join(strings.Fields(q), " ")
			if !seen[q] {
				seen[q] = true
				queries = append(queries, q)
			}
		}
	}

	return queries
}

// EnsureLocationInQueries guarantees that every query string includes the target location context.
// If raw queries like "toko bangunan" are provided without a city/location suffix,
// this function appends the location string (e.g. "toko bangunan Surabaya, Jawa Timur")
// to prevent Google Maps from defaulting to headless browser IP location (Jakarta).
func EnsureLocationInQueries(rawQueries []string, loc model.LocationConfig) []string {
	locStr := loc.GetSearchString()
	if locStr == "" {
		locStr = "Indonesia"
	}

	var processed []string
	seen := make(map[string]bool)

	for _, q := range rawQueries {
		qTrim := strings.TrimSpace(q)
		if qTrim == "" {
			continue
		}

		// Process placeholders
		finalQ := qTrim
		hasPlaceholder := false

		if strings.Contains(finalQ, "{LOCATION}") {
			finalQ = strings.ReplaceAll(finalQ, "{LOCATION}", locStr)
			hasPlaceholder = true
		}
		if strings.Contains(finalQ, "{CITY}") {
			cityStr := loc.City
			if cityStr == "" { cityStr = locStr } // Fallback
			finalQ = strings.ReplaceAll(finalQ, "{CITY}", cityStr)
			hasPlaceholder = true
		}
		if strings.Contains(finalQ, "{PROVINCE}") {
			provStr := loc.Province
			if provStr == "" { provStr = locStr } // Fallback
			finalQ = strings.ReplaceAll(finalQ, "{PROVINCE}", provStr)
			hasPlaceholder = true
		}
		if strings.Contains(finalQ, "{KECAMATAN}") {
			kecStr := loc.District
			if kecStr == "" { kecStr = locStr } // Fallback
			finalQ = strings.ReplaceAll(finalQ, "{KECAMATAN}", kecStr)
			hasPlaceholder = true
		}

		// Only append location if no placeholder was used AND it doesn't already contain the location
		if !hasPlaceholder {
			qLower := strings.ToLower(finalQ)
			hasLocation := false
			if loc.City != "" && strings.Contains(qLower, strings.ToLower(loc.City)) {
				hasLocation = true
			} else if loc.CustomLocation != "" && strings.Contains(qLower, strings.ToLower(loc.CustomLocation)) {
				hasLocation = true
			} else if strings.Contains(qLower, strings.ToLower(locStr)) {
				hasLocation = true
			}

			if !hasLocation {
				finalQ = fmt.Sprintf("%s %s", finalQ, locStr)
			}
		}
		
		finalQ = strings.Join(strings.Fields(finalQ), " ") // clean multiple spaces

		if !seen[finalQ] {
			seen[finalQ] = true
			processed = append(processed, finalQ)
		}
	}

	return processed
}


