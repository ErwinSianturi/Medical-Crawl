package normalizer_test

import (
	"testing"

	"maps-scraper/pkg/normalizer"
)

func TestNormalizeOpeningHoursCommaFormat(t *testing.T) {
	input := "Senin\n08.30–16.00\nSelasa\n08.30–16.00\nRabu\n08.30–16.00\nKamis\n08.30–16.00\nJumat\n08.30–16.00\nSabtu\n08.30–14.00\nMinggu\nTutup"
	expected := "Senin,08.30–16.00 | Selasa,08.30–16.00 | Rabu,08.30–16.00 | Kamis,08.30–16.00 | Jumat,08.30–16.00 | Sabtu,08.30–14.00 | Minggu,Tutup"

	got := normalizer.NormalizeOpeningHours(input)
	if got != expected {
		t.Fatalf("NormalizeOpeningHours() =\n%q\nexpected:\n%q", got, expected)
	}
}
