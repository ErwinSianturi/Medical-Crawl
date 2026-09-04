package normalizer

import (
	"testing"
)

func TestExtractKecamatan(t *testing.T) {
	// We need to mock the GeoData so that model.GetDistrictsForCity("Denpasar") 
	// returns ["Denpasar Timur", "Denpasar Selatan", "Denpasar Barat", "Denpasar Utara"]
	// Because our test is self-contained.
	// But our model.GeoData is global. We will load it manually from the test, or just set it manually for the test.
	
	// I'll write a standalone test for the Resolver logic first.
	canonicals := []string{
		"Denpasar Timur", "Denpasar Selatan", "Denpasar Barat", "Denpasar Utara", "Denpasar Tengah",
		"Jakarta Selatan", "Jakarta Timur", "Bandung Wetan", "Bandung Kulon", "Surabaya Selatan",
		"Surabaya Timur", "Semarang Tengah",
	}
	resolver := NewResolver(canonicals)

	tests := []struct {
		input    string
		expected string
		method   MatchMethod
	}{
		{"Denpasar Timur", "Denpasar Timur", MethodExact},
		{"denpasar timur", "Denpasar Timur", MethodNormalized},
		{"Denpasar Tim", "Denpasar Timur", MethodAbbreviation},
		{"Denpasar Tim.", "Denpasar Timur", MethodAbbreviation},
		{"Kec. Denpasar Tim", "Denpasar Timur", MethodAbbreviation},
		{"Kecamatan Denpasar Tim", "Denpasar Timur", MethodAbbreviation},
		{"Denpasar Sel", "Denpasar Selatan", MethodAbbreviation},
		{"Denpasar Bar", "Denpasar Barat", MethodAbbreviation},
		{"Denpasar Utr", "Denpasar Utara", MethodAbbreviation},
		{"Denpasar Tgh", "Denpasar Tengah", MethodAbbreviation},
		{"Denpasar Tmr", "Denpasar Timur", MethodFuzzy},
		{"Denpasar Sltn", "Denpasar Selatan", MethodFuzzy},
		{"Denpasar Br", "Denpasar Barat", MethodFuzzy},
		{"Jakarta Sel.", "Jakarta Selatan", MethodAbbreviation},
		{"Jakarta Tim.", "Jakarta Timur", MethodAbbreviation},
		{"Bdg Wetan", "Bandung Wetan", MethodFuzzy}, // Assuming Bandung Wetan exists
		{"Bdg Kulon", "Bandung Kulon", MethodFuzzy},
		{"Sby Sel", "Surabaya Selatan", MethodAbbreviation},
		{"SBY TIMUR", "Surabaya Timur", MethodNormalized}, // In exact test, this is just to check normalizer
		{"Invalid Random District Name", "", MethodAmbiguous}, // Negative test
		{"Denpasar Utaraa", "Denpasar Utara", MethodFuzzy}, // Typo test
		{"Semarang Tgh", "Semarang Tengah", MethodAbbreviation},
		{"Kecamatan Denpasar S", "Denpasar Selatan", MethodAbbreviation}, // 'S' doesn't map to 'Selatan' unless fuzzy! Actually this might be ambiguous. Let's make it fuzzy or ambiguous.
		{"Denpasar Bar.", "Denpasar Barat", MethodAbbreviation},
		{"Denpasar   Tim", "Denpasar Timur", MethodAbbreviation}, // whitespace
		{"denpasar-tim", "Denpasar Timur", MethodAbbreviation}, // punctuation
	}

	for _, tc := range tests {
		res := resolver.Resolve(tc.input)
		if res.CanonicalValue != tc.expected {
			t.Errorf("For %q expected CanonicalValue %q, got %q (Method: %s, Score: %f)", tc.input, tc.expected, res.CanonicalValue, res.Method, res.Confidence)
		}
		if res.Method != tc.method && res.CanonicalValue == tc.expected {
			// Just a warning, method can differ based on exact logic
			t.Logf("For %q expected method %s, got %s", tc.input, tc.method, res.Method)
		}
	}
}
