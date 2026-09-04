package query_test

import (
	"strings"
	"testing"

	"maps-scraper/pkg/model"
	"maps-scraper/pkg/query"
)

func TestQueryGeneration_LargePool(t *testing.T) {
	model.LoadGeoData("../../data/indonesia_geo.json")

	// Test Surabaya large pool
	surabayaCfg := model.JobConfig{
		Target: "toko bangunan",
		Location: model.LocationConfig{
			Country:  "Indonesia",
			Province: "Jawa Timur",
			City:     "Surabaya",
		},
	}
	surabayaQueries := query.GenerateMultiRegionQueries(surabayaCfg)
	if len(surabayaQueries) < 2000 {
		t.Fatalf("Expected >= 2000 queries for Surabaya, got %d", len(surabayaQueries))
	}
	// Check deduplication
	seen := make(map[string]bool)
	for _, q := range surabayaQueries {
		if seen[q] {
			t.Fatalf("Duplicate query found in pool: %s", q)
		}
		seen[q] = true
	}

	// Test Non-Surabaya Region with districts (e.g. Sidoarjo or Medan or Bandung)
	medanCfg := model.JobConfig{
		Target: "toko bangunan, toko semen",
		Location: model.LocationConfig{
			Country:  "Indonesia",
			Province: "Sumatera Utara",
			City:     "KOTA MEDAN",
		},
	}
	medanQueries := query.GenerateMultiRegionQueries(medanCfg)
	if len(medanQueries) < 2000 {
		t.Fatalf("Expected >= 2000 queries for Medan, got %d", len(medanQueries))
	}
	seenMedan := make(map[string]bool)
	for _, q := range medanQueries {
		if seenMedan[q] {
			t.Fatalf("Duplicate query found in Medan pool: %s", q)
		}
		seenMedan[q] = true
	}
}

func TestQueryPool_RegionIsolation(t *testing.T) {
	model.LoadGeoData("../../data/indonesia_geo.json")

	jobSurabaya := model.JobConfig{
		Target: "toko bangunan",
		Location: model.LocationConfig{City: "Surabaya", Province: "Jawa Timur"},
	}
	jobBandung := model.JobConfig{
		Target: "toko bangunan",
		Location: model.LocationConfig{City: "KOTA BANDUNG", Province: "Jawa Barat"},
	}

	qSurabaya := query.GenerateMultiRegionQueries(jobSurabaya)
	qBandung := query.GenerateMultiRegionQueries(jobBandung)

	if len(qSurabaya) == 0 || len(qBandung) == 0 {
		t.Fatalf("Query generation returned empty pools")
	}

	// Make sure no queries cross-pollinate
	for _, q := range qSurabaya {
		if strings.Contains(strings.ToLower(q), "bandung") {
			t.Fatalf("Surabaya pool contains Bandung query: %s", q)
		}
	}
	for _, q := range qBandung {
		if strings.Contains(strings.ToLower(q), "surabaya") {
			t.Fatalf("Bandung pool contains Surabaya query: %s", q)
		}
	}
}
