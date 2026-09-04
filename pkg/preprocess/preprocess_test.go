package preprocess_test

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"testing"

	"maps-scraper/pkg/model"
	"maps-scraper/pkg/preprocess"
)

// Helper to hash a file to ensure raw data immutability
func hashFile(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Test 1: Record clearly in Surabaya -> VALID
func TestSurabayaValidation_Valid(t *testing.T) {
	cfg := preprocess.DefaultConfig()
	v := preprocess.NewValidator(cfg)

	pr := &preprocess.ProcessedRecord{
		OriginalLine: 10,
		Record: model.StoreRecord{
			Name:      "Toko Bangunan Sinar Rejeki",
			Address:   "Jl. Kertajaya No.45, Kertajaya, Kec. Gubeng, Surabaya, Jawa Timur 60282",
			City:      "Surabaya",
			Kecamatan: "Gubeng",
			Latitude:  "-7.275.123",
			Longitude: "112.754.456",
			Types:     "toko bahan bangunan, store",
		},
	}

	v.ValidateRow(pr)
	if pr.Status != preprocess.StatusValid {
		t.Errorf("Expected status %s, got %s (Reason: %s)", preprocess.StatusValid, pr.Status, pr.Reason)
	}
}

// Test 2: Record in Sidoarjo -> INVALID_OUTSIDE_SURABAYA
func TestSurabayaValidation_OutsideSurabaya(t *testing.T) {
	cfg := preprocess.DefaultConfig()
	v := preprocess.NewValidator(cfg)

	pr := &preprocess.ProcessedRecord{
		OriginalLine: 20,
		Record: model.StoreRecord{
			Name:      "TB Berkah Abadi Sidoarjo",
			Address:   "Jl. Raya Waru No.12, Waru, Kec. Waru, Kabupaten Sidoarjo, Jawa Timur 61256",
			City:      "Sidoarjo",
			Kecamatan: "Waru",
			Latitude:  "-7.445.000",
			Longitude: "112.720.000",
			Types:     "toko bahan bangunan, store",
		},
	}

	v.ValidateRow(pr)
	if pr.Status != preprocess.StatusInvalidOutsideSurabaya {
		t.Errorf("Expected status %s, got %s (Reason: %s)", preprocess.StatusInvalidOutsideSurabaya, pr.Status, pr.Reason)
	}
}

// Test 3: Same Store Name + Same Place ID -> HIGH_CONFIDENCE_DUPLICATE
func TestDeduplication_SamePlaceID(t *testing.T) {
	cfg := preprocess.DefaultConfig()
	d := preprocess.NewDeduplicator(cfg)

	recA := &preprocess.ProcessedRecord{
		OriginalLine: 100,
		Record: model.StoreRecord{
			PlaceID:   "0x2dd7fb884619284f:0x6998b9b720594b0a",
			Name:      "Mitra10 Ahmad Yani",
			Address:   "Jl. Ahmad Yani No.270, Surabaya",
			Latitude:  "-7.341.047",
			Longitude: "112.728.197",
		},
	}
	recB := &preprocess.ProcessedRecord{
		OriginalLine: 105,
		Record: model.StoreRecord{
			PlaceID:   "0x2dd7fb884619284f:0x6998b9b720594b0a",
			Name:      "Mitra 10 - Ahmad Yani Surabaya",
			Address:   "Jl. A Yani 270, Surabaya",
			Latitude:  "-7.341.047",
			Longitude: "112.728.197",
		},
	}

	score, status, _ := d.EvaluatePair(recA, recB)
	if status != preprocess.StatusHighConfidenceDup || score < 0.95 {
		t.Errorf("Expected HIGH_CONFIDENCE_DUPLICATE with score >= 0.95, got status=%s, score=%.2f", status, score)
	}
}

// Test 4: Similar Name + Same Address -> HIGH_CONFIDENCE_DUPLICATE
func TestDeduplication_SimilarNameSameAddress(t *testing.T) {
	cfg := preprocess.DefaultConfig()
	d := preprocess.NewDeduplicator(cfg)

	recA := &preprocess.ProcessedRecord{
		OriginalLine: 200,
		Record: model.StoreRecord{
			Name:      "Toko Bangunan Hokky Menganti",
			Address:   "Jl. Raya Menganti Babatan No.38, Wiyung, Surabaya",
			Latitude:  "-7.310.718",
			Longitude: "112.684.020",
		},
	}
	recB := &preprocess.ProcessedRecord{
		OriginalLine: 205,
		Record: model.StoreRecord{
			Name:      "TB Hokky Menganti Surabaya",
			Address:   "Jl Raya Menganti Babatan No 38 Wiyung Surabaya",
			Latitude:  "-7.310.718",
			Longitude: "112.684.020",
		},
	}

	score, status, _ := d.EvaluatePair(recA, recB)
	if status != preprocess.StatusHighConfidenceDup {
		t.Errorf("Expected HIGH_CONFIDENCE_DUPLICATE, got %s (score=%.2f)", status, score)
	}
}

// Test 5: Similar Name but different distant location -> FLAGGED_POSSIBLE_DUPLICATE / NOT_DUPLICATE
func TestDeduplication_SimilarNameDistantLocation(t *testing.T) {
	cfg := preprocess.DefaultConfig()
	d := preprocess.NewDeduplicator(cfg)

	recA := &preprocess.ProcessedRecord{
		OriginalLine:   300,
		ParsedLat:      -7.311206,
		ParsedLon:      112.680353,
		HasValidCoords: true,
		Record: model.StoreRecord{
			Name:      "Mitra10 Wiyung Surabaya",
			Address:   "Jl. Raya Menganti Babatan No.477, Wiyung, Surabaya",
			Latitude:  "-7.311.206",
			Longitude: "112.680.353",
		},
	}
	recB := &preprocess.ProcessedRecord{
		OriginalLine:   305,
		ParsedLat:      -7.341047,
		ParsedLon:      112.728197,
		HasValidCoords: true,
		Record: model.StoreRecord{
			Name:      "Mitra10 Ahmad Yani Surabaya",
			Address:   "Jl. Ahmad Yani No.270, Gayungan, Surabaya",
			Latitude:  "-7.341.047",
			Longitude: "112.728.197",
		},
	}

	_, status, _ := d.EvaluatePair(recA, recB)
	// Because they are ~6km apart, they should NOT be merged as duplicate (distinct chain branches)
	if status == preprocess.StatusHighConfidenceDup {
		t.Errorf("Distant branch stores must not be resolved as HIGH_CONFIDENCE_DUPLICATE! Got %s", status)
	}
}

// Test 6: Address says Surabaya but coordinate is outside Surabaya -> FLAGGED_LOCATION_CONFLICT
func TestSurabayaValidation_LocationConflict(t *testing.T) {
	cfg := preprocess.DefaultConfig()
	v := preprocess.NewValidator(cfg)

	pr := &preprocess.ProcessedRecord{
		OriginalLine: 400,
		Record: model.StoreRecord{
			Name:      "Toko Bangunan Wijaya",
			Address:   "Jl. Kertajaya Indah No.10, Sukolilo, Surabaya, Jawa Timur",
			City:      "Surabaya",
			Kecamatan: "Sukolilo",
			Latitude:  "-7.500.000", // Clearly outside bounds (Sidoarjo/Pasuruan)
			Longitude: "112.750.000",
			Types:     "toko bahan bangunan, store",
		},
	}

	v.ValidateRow(pr)
	if pr.Status != preprocess.StatusFlaggedLocationConflict {
		t.Errorf("Expected %s, got %s (Reason: %s)", preprocess.StatusFlaggedLocationConflict, pr.Status, pr.Reason)
	}
}

// Test 7: Insufficient address & empty coordinate -> FLAGGED_INSUFFICIENT_LOCATION
func TestSurabayaValidation_InsufficientLocation(t *testing.T) {
	cfg := preprocess.DefaultConfig()
	v := preprocess.NewValidator(cfg)

	pr := &preprocess.ProcessedRecord{
		OriginalLine: 500,
		Record: model.StoreRecord{
			Name:      "Toko Besi Jaya",
			Address:   "Jl. Merak", // Too short, no city/district info
			City:      "",
			Kecamatan: "",
			Latitude:  "",
			Longitude: "",
			Types:     "toko bahan bangunan, store",
		},
	}

	v.ValidateRow(pr)
	if pr.Status != preprocess.StatusFlaggedInsufficientLoc {
		t.Errorf("Expected %s, got %s (Reason: %s)", preprocess.StatusFlaggedInsufficientLoc, pr.Status, pr.Reason)
	}
}

// Test 8: Completeness resolution (higher completeness becomes PRIMARY_RECORD)
func TestDeduplication_CompletenessResolution(t *testing.T) {
	cfg := preprocess.DefaultConfig()
	d := preprocess.NewDeduplicator(cfg)

	recA := &preprocess.ProcessedRecord{
		OriginalLine: 600,
		Record: model.StoreRecord{
			PlaceID: "0x2dd7f0001:0x12345",
			Name:    "Toko Bangunan Makmur",
			Address: "Jl. Diponegoro No.10, Wonokromo, Surabaya",
			// Missing phone, opening hours, photo, website
		},
	}
	recB := &preprocess.ProcessedRecord{
		OriginalLine: 605,
		Record: model.StoreRecord{
			PlaceID:      "0x2dd7f0001:0x12345",
			Name:         "Toko Bangunan Makmur Jaya",
			Address:      "Jl. Diponegoro No.10, Wonokromo, Kec. Wonokromo, Surabaya, Jawa Timur 60241",
			City:         "Surabaya",
			Kecamatan:    "Wonokromo",
			Latitude:     "-7.291.000",
			Longitude:    "112.735.000",
			Phone:        "0812-3456-7890",
			OpeningHours: "Senin,08.00-17.00 | Selasa,08.00-17.00 | Rabu,08.00-17.00 | Kamis,08.00-17.00 | Jumat,08.00-17.00 | Sabtu,08.00-17.00 | Minggu,Tutup",
			PhotoURL:     "https://example.com/photo.jpg",
			WebsiteLinks: "https://makmurjaya.com",
			Types:        "toko bahan bangunan, store",
		},
	}

	scoreA := preprocess.CalculateCompletenessScore(recA.Record)
	scoreB := preprocess.CalculateCompletenessScore(recB.Record)

	if scoreB <= scoreA {
		t.Errorf("Expected record B to have higher completeness score than record A (scoreA=%d, scoreB=%d)", scoreA, scoreB)
	}

	records := []*preprocess.ProcessedRecord{recA, recB}
	candidates := d.DetectDuplicates(records)
	d.ResolveDuplicateGroups(records, candidates)

	if recB.Status != preprocess.StatusValid {
		t.Errorf("Expected complete record B to remain VALID primary record, got %s", recB.Status)
	}
	if recA.Status != preprocess.StatusHighConfidenceDup || recA.RelatedLine != 605 {
		t.Errorf("Expected incomplete record A to be HIGH_CONFIDENCE_DUPLICATE related to line 605, got %s (rel: %d)", recA.Status, recA.RelatedLine)
	}
}

// Test 9: Raw input file immutability test
func TestPipeline_RawDataImmutability(t *testing.T) {
	testCSV := "../../newest_repaired.csv"
	if _, err := os.Stat(testCSV); os.IsNotExist(err) {
		testCSV = "newest_repaired.csv"
		if _, err := os.Stat(testCSV); os.IsNotExist(err) {
			t.Skip("newest_repaired.csv not present")
		}
	}

	hashBefore, err := hashFile(testCSV)
	if err != nil {
		t.Fatalf("Failed to hash file before: %v", err)
	}

	cfg := preprocess.DefaultConfig()
	p := preprocess.NewPipeline(cfg)

	// Run process into temp files
	_, _, err = p.ProcessFile(testCSV, "test_clean.csv", "test_flagged.csv", "test_rejected.csv", "test_report.txt")
	if err != nil {
		t.Fatalf("ProcessFile failed: %v", err)
	}
	defer os.Remove("test_clean.csv")
	defer os.Remove("test_flagged.csv")
	defer os.Remove("test_rejected.csv")
	defer os.Remove("test_report.txt")

	hashAfter, err := hashFile(testCSV)
	if err != nil {
		t.Fatalf("Failed to hash file after: %v", err)
	}

	if hashBefore != hashAfter {
		t.Fatalf("CRITICAL: Raw input data was mutated during preprocessing! (Hash before: %s, Hash after: %s)", hashBefore, hashAfter)
	}
}
