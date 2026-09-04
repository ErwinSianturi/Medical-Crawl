package normalizer

import (
	"testing"
	"maps-scraper/pkg/model"
)

func TestExtractCityBalige(t *testing.T) {
	model.LoadGeoData("../../data/indonesia_geo.json")
	address := "83PG+V54, Jalan, Sibola Hotangsas, Balige, Toba, North Sumatra"
	
	// Test City
	city := ExtractCity(address, "")
	if city != "KABUPATEN TOBA SAMOSIR" {
		t.Errorf("Expected City to be KABUPATEN TOBA SAMOSIR, got %s", city)
	}

	// Test Kecamatan
	kec := ExtractKecamatan(address, city)
	if kec != "BALIGE" && kec != "Balige" {
		t.Errorf("Expected Kecamatan to be BALIGE, got %s", kec)
	}
}
