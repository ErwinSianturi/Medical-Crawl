package preprocess

import (
	"math"
)

// Config contains all configurable thresholds and geographic bounds
type Config struct {
	// Coordinate boundaries for Surabaya
	MinLat float64
	MaxLat float64
	MinLon float64
	MaxLon float64

	// Similarity thresholds
	NameSimilarityThreshold    float64 // Threshold for name fuzzy match
	AddressSimilarityThreshold float64 // Threshold for address fuzzy match
	HighConfidenceDupThreshold float64 // Score >= this -> HIGH_CONFIDENCE_DUPLICATE
	PossibleDupThreshold       float64 // Score >= this and < HighConfidence -> FLAGGED_POSSIBLE_DUPLICATE
	MaxDupDistanceMeters       float64 // Distance in meters considered nearby
	StrictSameLocationMeters   float64 // Distance in meters considered identical spot (e.g. 50m)
}

// DefaultConfig returns the production default configuration
func DefaultConfig() Config {
	return Config{
		// Surabaya metropolitan geographic bounding box
		// Lat: -7.38 to -7.18, Lon: 112.58 to 112.85
		MinLat: -7.380000,
		MaxLat: -7.180000,
		MinLon: 112.580000,
		MaxLon: 112.850000,

		NameSimilarityThreshold:    0.80,
		AddressSimilarityThreshold: 0.70,
		HighConfidenceDupThreshold: 0.85,
		PossibleDupThreshold:       0.55,
		MaxDupDistanceMeters:       1500.0, // 1.5 km
		StrictSameLocationMeters:   60.0,   // 60 meters
	}
}

// SurabayaKecamatans contains the 31 official districts of Kota Surabaya
var SurabayaKecamatans = []string{
	"Asemrowo", "Benowo", "Bubutan", "Bulak", "Dukuh Pakis", "Gayungan", "Genteng",
	"Gubeng", "Gunung Anyar", "Jambangan", "Karangpilang", "Kenjeran",
	"Krembangan", "Lakarsantri", "Mulyorejo", "Pabean Cantian", "Pakal", "Rungkut",
	"Sambikerep", "Sawahan", "Semampir", "Simokerto", "Sukolilo", "Sukomanunggal",
	"Tambaksari", "Tandes", "Tegalsari", "Tenggilis Mejoyo", "Wiyung", "Wonocolo",
	"Wonokromo",
}

// OutsideRegencies is the list of neighboring regencies / non-Surabaya regions to detect outside entities
var OutsideRegencies = []string{
	"sidoarjo", "gresik", "mojokerto", "lamongan", "pasuruan", "bangkalan",
	"malang", "tuban", "jombang", "kediri", "blitar", "probolinggo", "madiun",
	"krian", "waru", "gedangan", "candi", "taman", "sedati", "sukodono",
	"driyorejo", "menganti", "kebomas", "manyar", "cerme",
}

// HaversineDistance calculates the distance in meters between two lat/lon coordinates
func HaversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371000.0 // meters
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	rLat1 := lat1 * (math.Pi / 180.0)
	rLat2 := lat2 * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rLat1)*math.Cos(rLat2)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadius * c
}
