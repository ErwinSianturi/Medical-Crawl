package normalizer

import (
	"testing"
)

func TestEvaluateLocation(t *testing.T) {
	tests := []struct {
		name           string
		address        string
		city           string
		kecamatan      string
		lat, lon       float64
		targetCity     string
		targetProv     string
		targetRegency  string
		targetDist     string
		customLoc      string
		expectedValid  bool
	}{
		{
			name:          "Surabaya explicit address match",
			address:       "Jl. Ngagel Jaya Selatan No. 123, Surabaya",
			city:          "Surabaya",
			targetCity:    "Surabaya",
			expectedValid: true,
		},
		{
			name:          "Balige variation 1",
			address:       "Jl. Mulia Raja No. 26, Balige",
			targetRegency: "Toba",
			targetDist:    "Balige",
			expectedValid: true,
		},
		{
			name:          "Balige variation 2",
			address:       "Jalan Mulia Raja No 26 Balige",
			targetRegency: "Toba",
			targetDist:    "Balige",
			expectedValid: true,
		},
		{
			name:          "Toba variations in address",
			address:       "Kab. Toba",
			targetRegency: "Toba",
			expectedValid: true,
		},
		{
			name:          "Kecamatan Balige variation",
			address:       "Kecamatan Balige, Kab. Toba",
			targetRegency: "Toba",
			expectedValid: true,
		},
		{
			name:          "Invalid location - Jakarta instead of Surabaya",
			address:       "Jl. Sudirman, Jakarta",
			city:          "Jakarta",
			targetCity:    "Surabaya",
			expectedValid: false,
		},
		{
			name:          "Surabaya rejects Sidoarjo (Bug reproduction test)",
			address:       "NO WA O&2I3OOO1I8l, Jl. Mandala No.557, Semawalang, Semambung, Kec. Gedangan, Kabupaten Sidoarjo, Jawa Timur 61253",
			city:          "KABUPATEN SIDOARJO",
			kecamatan:     "GEDANGAN",
			lat:           -7.379223,
			lon:           112.748300,
			targetCity:    "KOTA SURABAYA",
			expectedValid: false,
		},
		{
			name:          "Sidoarjo accepts Sidoarjo",
			address:       "NO WA O&2I3OOO1I8l, Jl. Mandala No.557, Semawalang, Semambung, Kec. Gedangan, Kabupaten Sidoarjo, Jawa Timur 61253",
			city:          "KABUPATEN SIDOARJO",
			kecamatan:     "GEDANGAN",
			lat:           -7.379223,
			lon:           112.748300,
			targetCity:    "KABUPATEN SIDOARJO",
			expectedValid: true,
		},
		{
			name:          "Gresik accepts Gresik",
			address:       "Jl. Veteran, Gresik",
			city:          "Gresik",
			targetCity:    "KABUPATEN GRESIK",
			expectedValid: true,
		},
		{
			name:          "Gresik rejects Surabaya",
			address:       "Jl. Ngagel, Surabaya",
			city:          "Surabaya",
			targetCity:    "KABUPATEN GRESIK",
			expectedValid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := EvaluateLocation(tc.address, tc.city, tc.kecamatan, tc.lat, tc.lon, tc.targetCity, tc.targetProv, tc.targetRegency, tc.targetDist, tc.customLoc)
			if res.IsValid != tc.expectedValid {
				t.Errorf("expected IsValid=%v, got %v (Reason: %s, Confidence: %d)", tc.expectedValid, res.IsValid, res.Reason, res.Confidence)
			}
		})
	}
}
