package normalizer_test

import (
	"testing"
	"maps-scraper/pkg/normalizer"
)

func TestCleanAddressResult(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedAddr  string
		expectedPhone string
	}{
		{
			"WA admin + address",
			"Wa admin; O&99I5OOO29, Jl. Kendangsari Gg. Lebar No.72, Kendangsari",
			"Jl. Kendangsari Gg. Lebar No.72, Kendangsari",
			"", // O&99I5OOO29 is not caught by standard regex, but stripped by obfuscation rule
		},
		{
			"WhatsApp + address",
			"WhatsApp 08123456789, Jl. Darmo 123",
			"Jl. Darmo 123",
			"08123456789",
		},
		{
			"Telp + address",
			"Telp/WA: +62812345678, Jl. Pahlawan",
			"Jl. Pahlawan",
			"+62812345678",
		},
		{
			"Contact admin prefix inside string",
			"Contact admin: 081234567, Jl. Raya",
			"Jl. Raya",
			"081234567",
		},
		{
			"URL + address",
			"Visit www.toko.com, Jl. Mawar",
			"Jl. Mawar",
			"",
		},
		{
			"Social media + address",
			"Instagram @toko, Jl. Melati",
			"Jl. Melati",
			"",
		},
		{
			"Promotional text + address",
			"Gratis ongkir COD, Jl. Kenanga",
			"Jl. Kenanga",
			"",
		},
		{
			"Address tanpa contamination",
			"Jl. Sukomanunggal No.34, Sukomanunggal",
			"Jl. Sukomanunggal No.34, Sukomanunggal",
			"",
		},
		{
			"Contact info at the end",
			"Jl. A. Yani, Surabaya, WA 08123456",
			"Jl. A. Yani, Surabaya",
			"08123456",
		},
		{
			"Merged contact string before jl",
			"WA admin Jl. Raya Darmo",
			"Jl. Raya Darmo",
			"", // "WA admin" is stripped
		},
		{
			"Empty",
			"",
			"",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := normalizer.CleanAddressResult(tt.input)
			if res.CleanedAddress != tt.expectedAddr {
				t.Errorf("CleanedAddress = %q; expected %q", res.CleanedAddress, tt.expectedAddr)
			}
			if res.ExtractedPhone != tt.expectedPhone {
				t.Errorf("ExtractedPhone = %q; expected %q", res.ExtractedPhone, tt.expectedPhone)
			}
		})
	}
}
func TestNormalizeOpeningHours(t *testing.T) {
	rawTable := "Senin,08.30-16.00\nSelasa,08.30-16.00\nRabu,08.30-16.00\nKamis,08.30-16.00\nJumat,08.30-16.00\nSabtu,08.30-14.00\nMinggu,Tutup"
	expected := "Senin,08.30–16.00 | Selasa,08.30–16.00 | Rabu,08.30–16.00 | Kamis,08.30–16.00 | Jumat,08.30–16.00 | Sabtu,08.30–14.00 | Minggu,Tutup"
	got := normalizer.NormalizeOpeningHours(rawTable)
	if got != expected {
		t.Errorf("NormalizeOpeningHours() = %s, expected %s", got, expected)
	}
}
