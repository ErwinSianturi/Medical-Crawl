package preprocess

import (
	"fmt"
	"regexp"
	"strings"

	"maps-scraper/pkg/model"
	"maps-scraper/pkg/normalizer"
)

var (
	kecamatanWordRegex = regexp.MustCompile(`(?i)(?:Kec\.?|Kecamatan)\s+([a-zA-Z\s]+?)(?:,|$|\d)`)
)

// ValidationStatus represents the outcome of row-level location & format validation
type ValidationStatus string

const (
	StatusValid                     ValidationStatus = "VALID"
	StatusInvalidOutsideSurabaya    ValidationStatus = "INVALID_OUTSIDE_SURABAYA"
	StatusFlaggedLocationConflict   ValidationStatus = "FLAGGED_LOCATION_CONFLICT"
	StatusFlaggedInsufficientLoc    ValidationStatus = "FLAGGED_INSUFFICIENT_LOCATION"
	StatusInvalidData               ValidationStatus = "INVALID_DATA"
	StatusHighConfidenceDup         ValidationStatus = "HIGH_CONFIDENCE_DUPLICATE"
	StatusFlaggedPossibleDup        ValidationStatus = "FLAGGED_POSSIBLE_DUPLICATE"
	StatusPrimaryRecord             ValidationStatus = "PRIMARY_RECORD"
	StatusDuplicateRemoved          ValidationStatus = "DUPLICATE_REMOVED"
)

// ProcessedRecord wraps model.StoreRecord with tracking metadata
type ProcessedRecord struct {
	OriginalLine      int
	Record            model.StoreRecord
	ParsedLat         float64
	ParsedLon         float64
	HasValidCoords    bool
	Status            ValidationStatus
	Reason            string
	ConfidenceScore   float64
	RelatedLine       int
	DuplicateGroupID  string
	RecommendedAction string
	CompletenessScore int
}

// Validator handles geography and data consistency checks
type Validator struct {
	cfg Config
}

// NewValidator creates a new Validator with the given configuration
func NewValidator(cfg Config) *Validator {
	return &Validator{cfg: cfg}
}

// ValidateRow validates a single record against Surabaya boundary, address, kecamatan, and coordinates
func (v *Validator) ValidateRow(pr *ProcessedRecord) {
	rec := pr.Record

	// Check core data presence
	if strings.TrimSpace(rec.Name) == "" && strings.TrimSpace(rec.Address) == "" {
		pr.Status = StatusInvalidData
		pr.Reason = "Missing store name and address"
		return
	}

	// Parse Coordinates if present
	latStr := strings.TrimSpace(rec.Latitude)
	lonStr := strings.TrimSpace(rec.Longitude)

	if latStr != "" && lonStr != "" {
		lat, errLat := normalizer.ParseCoordinateFromDotted(latStr)
		lon, errLon := normalizer.ParseCoordinateFromDotted(lonStr)
		if errLat == nil && errLon == nil {
			pr.ParsedLat = lat
			pr.ParsedLon = lon
			pr.HasValidCoords = true
		}
	}

	addrLower := strings.ToLower(rec.Address)
	kecLower := strings.ToLower(rec.Kecamatan)
	cityLower := strings.ToLower(rec.City)

	// Step 1: Address check for outside regencies
	addressOutsideFound := ""
	for _, reg := range OutsideRegencies {
		if strings.Contains(addrLower, reg) || strings.Contains(kecLower, reg) || strings.Contains(cityLower, reg) {
			// Check if it specifically contains "Surabaya" or a Surabaya district
			// E.g. "Jl. Raya Sidoarjo, Surabaya" vs "Kabupaten Sidoarjo"
			if strings.Contains(addrLower, "kabupaten "+reg) ||
				strings.Contains(addrLower, "kab. "+reg) ||
				strings.Contains(addrLower, "kec. "+reg) ||
				strings.Contains(addrLower, ", "+reg) ||
				strings.Contains(cityLower, reg) ||
				strings.Contains(kecLower, reg) {
				addressOutsideFound = reg
				break
			}
		}
	}

	// Step 2: Coordinate boundary check
	coordInSurabaya := false
	coordOutsideSurabaya := false
	if pr.HasValidCoords {
		if pr.ParsedLat >= v.cfg.MinLat && pr.ParsedLat <= v.cfg.MaxLat &&
			pr.ParsedLon >= v.cfg.MinLon && pr.ParsedLon <= v.cfg.MaxLon {
			coordInSurabaya = true
		} else {
			coordOutsideSurabaya = true
		}
	}

	// Step 3: Surabaya district (Kecamatan) check
	kecInSurabayaList := false
	if rec.Kecamatan != "" {
		for _, validKec := range SurabayaKecamatans {
			if strings.EqualFold(rec.Kecamatan, validKec) {
				kecInSurabayaList = true
				break
			}
		}
	}

	// Check if address mentions Surabaya or valid kecamatan
	addrMentionsSurabaya := strings.Contains(addrLower, "surabaya")
	addrMentionsSurabayaKec := false
	for _, validKec := range SurabayaKecamatans {
		if strings.Contains(addrLower, strings.ToLower(validKec)) {
			addrMentionsSurabayaKec = true
			break
		}
	}

	// Decision Matrix:

	// 1. Explicit Location Conflict:
	// Address explicitly says Surabaya / Surabaya district, but coordinates are clearly outside Surabaya
	if (addrMentionsSurabaya || addrMentionsSurabayaKec || kecInSurabayaList) && coordOutsideSurabaya {
		pr.Status = StatusFlaggedLocationConflict
		pr.Reason = fmt.Sprintf("Address indicates Surabaya (%s) but coordinates [%.6f, %.6f] are outside Surabaya boundary", rec.Kecamatan, pr.ParsedLat, pr.ParsedLon)
		pr.ConfidenceScore = 0.90
		pr.RecommendedAction = "Manual verification of actual physical location vs coordinates"
		return
	}

	// Coordinates inside Surabaya, but address explicitly mentions outside regency
	if coordInSurabaya && addressOutsideFound != "" && !addrMentionsSurabaya {
		pr.Status = StatusFlaggedLocationConflict
		pr.Reason = fmt.Sprintf("Coordinates [%.6f, %.6f] are inside Surabaya, but address/city indicates %s", pr.ParsedLat, pr.ParsedLon, addressOutsideFound)
		pr.ConfidenceScore = 0.85
		pr.RecommendedAction = "Verify if business is near border or misplaced coordinate"
		return
	}

	// 2. Clearly Outside Surabaya:
	if addressOutsideFound != "" && !addrMentionsSurabaya && !addrMentionsSurabayaKec && !coordInSurabaya {
		pr.Status = StatusInvalidOutsideSurabaya
		pr.Reason = fmt.Sprintf("Store is located in %s (outside Kota Surabaya)", addressOutsideFound)
		pr.ConfidenceScore = 0.98
		return
	}

	if coordOutsideSurabaya && !addrMentionsSurabaya && !addrMentionsSurabayaKec {
		pr.Status = StatusInvalidOutsideSurabaya
		pr.Reason = fmt.Sprintf("Coordinates [%.6f, %.6f] are outside Kota Surabaya boundary", pr.ParsedLat, pr.ParsedLon)
		pr.ConfidenceScore = 0.95
		return
	}

	// If Kecamatan is explicitly given but NOT in Surabaya 31 kecamatan list, and coordinates not confirmed
	if rec.Kecamatan != "" && !kecInSurabayaList && !addrMentionsSurabaya && !coordInSurabaya {
		pr.Status = StatusInvalidOutsideSurabaya
		pr.Reason = fmt.Sprintf("Kecamatan '%s' is not in the 31 official Surabaya districts", rec.Kecamatan)
		pr.ConfidenceScore = 0.92
		return
	}

	// 3. Insufficient Location:
	// Address too short or missing key details, and no valid coordinates to verify
	if !pr.HasValidCoords {
		if len(strings.TrimSpace(rec.Address)) < 15 && !addrMentionsSurabaya && !addrMentionsSurabayaKec {
			pr.Status = StatusFlaggedInsufficientLoc
			pr.Reason = "Address is too brief and coordinates are missing or invalid"
			pr.ConfidenceScore = 0.80
			pr.RecommendedAction = "Search Google Maps to retrieve exact coordinates and full address"
			return
		}
	}

	// 4. Building Store Relevance Check (Soft Validation)
	// (Note: Store semantic validation - if totally non-relevant, can be flagged/invalid)
	if !normalizer.IsCementStoreRelevance(rec.Name, rec.Address, rec.Types) {
		pr.Status = StatusInvalidData
		pr.Reason = fmt.Sprintf("Business '%s' is not a building material store / relevant business", rec.Name)
		pr.ConfidenceScore = 0.90
		return
	}

	// 5. Valid Surabaya Store
	if coordInSurabaya || addrMentionsSurabaya || addrMentionsSurabayaKec || kecInSurabayaList {
		pr.Status = StatusValid
		pr.Reason = "Verified Surabaya location"
		pr.ConfidenceScore = 0.95
		return
	}

	// Default fallback: Insufficient location proof
	pr.Status = StatusFlaggedInsufficientLoc
	pr.Reason = "Insufficient administrative or geographic evidence to confirm Surabaya location"
	pr.ConfidenceScore = 0.70
	pr.RecommendedAction = "Manual inspection of store address"
}
