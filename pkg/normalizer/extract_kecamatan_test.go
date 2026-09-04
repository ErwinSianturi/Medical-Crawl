package normalizer

import (
	"testing"
)

func TestExtractKecamatanWrapper(t *testing.T) {
	// Our model package now has a fallback for Surabaya.
	city := "Surabaya"
	
	tests := []struct {
		input    string
		expected string
	}{
		{"Jl. Raya Menganti, Kec. Wiyung", "Wiyung"},
		{"Kecamatan Wiyung", "Wiyung"},
		{"Wiyung", "Wiyung"}, // Substring fallback
		{"Kec. Wiyung", "Wiyung"},
		{"Kec. Dukuh Pakis", "Dukuh Pakis"},
		{"Gubeng", "Gubeng"},
	}

	for _, tc := range tests {
		res := ExtractKecamatan(tc.input, city)
		if res != tc.expected {
			t.Errorf("ExtractKecamatan(%q, %q) = %q; expected %q", tc.input, city, res, tc.expected)
		}
	}
}
