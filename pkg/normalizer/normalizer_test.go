package normalizer_test

import (
	"testing"

	"maps-scraper/pkg/normalizer"
)

func TestFormatCoordinate(t *testing.T) {
	tests := []struct {
		input    float64
		expected string
	}{
		{-7.268714, "-7.268714"},
		{112.655352, "112.655352"},
		{-7.3, "-7.300000"},
	}

	for _, tc := range tests {
		actual := normalizer.FormatCoordinate(tc.input)
		if actual != tc.expected {
			t.Errorf("FormatCoordinate(%f) = %s, expected %s", tc.input, actual, tc.expected)
		}
	}
}

func TestGenerateMapsQueryLinks(t *testing.T) {
	lat := -7.268714
	lon := 112.655352
	dotLink, commaLink := normalizer.GenerateMapsQueryLinks(lat, lon)

	expectedDot := "https://www.google.com/maps?q=-7.268714,112.655352"
	expectedComma := "https://www.google.com/maps?q=-7,268714,112,655352"

	if dotLink != expectedDot {
		t.Errorf("dotLink = %s, expected %s", dotLink, expectedDot)
	}
	if commaLink != expectedComma {
		t.Errorf("commaLink = %s, expected %s", commaLink, expectedComma)
	}
}

func TestDetectSemenBrand(t *testing.T) {
	if brand := normalizer.DetectSemenBrand("PT. BIG MARGOMULYO (SEMEN GRESIK)", ""); brand != "Semen Gresik" {
		t.Errorf("expected Semen Gresik, got %s", brand)
	}
	if brand := normalizer.DetectSemenBrand("TB. Sumber Makmur", ""); brand != "" {
		t.Errorf("expected empty brand, got %s", brand)
	}
}
