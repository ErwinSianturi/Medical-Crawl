package preprocess

import (
	"fmt"
	"sort"
	"strings"

	"maps-scraper/pkg/normalizer"
)

// DuplicateCandidate represents a pair of records evaluated for duplication
type DuplicateCandidate struct {
	IndexA          int
	IndexB          int
	RecordA         *ProcessedRecord
	RecordB         *ProcessedRecord
	Score           float64
	Status          ValidationStatus
	Reason          string
	ConfidenceScore float64
}

// Deduplicator manages duplicate detection and resolution
type Deduplicator struct {
	cfg Config
}

// NewDeduplicator creates a new Deduplicator
func NewDeduplicator(cfg Config) *Deduplicator {
	return &Deduplicator{cfg: cfg}
}

// EvaluatePair computes the duplicate confidence score between two records
func (d *Deduplicator) EvaluatePair(recA, recB *ProcessedRecord) (score float64, status ValidationStatus, reason string) {
	// Rule 1: Exact Place ID match
	pID_A := strings.TrimSpace(recA.Record.PlaceID)
	pID_B := strings.TrimSpace(recB.Record.PlaceID)
	if pID_A != "" && pID_B != "" && pID_A == pID_B {
		return 1.0, StatusHighConfidenceDup, "Identical Place ID"
	}

	// Normalizations
	normNameA := NormalizeStoreName(recA.Record.Name)
	normNameB := NormalizeStoreName(recB.Record.Name)
	nameSim := CombinedSimilarity(normNameA, normNameB)

	// Address Similarity
	normAddrA := NormalizeText(recA.Record.Address)
	normAddrB := NormalizeText(recB.Record.Address)
	addrSim := CombinedSimilarity(normAddrA, normAddrB)

	// Phone Match
	phoneA := CleanPhoneNumber(recA.Record.Phone)
	phoneB := CleanPhoneNumber(recB.Record.Phone)
	phoneMatch := (phoneA != "" && phoneB != "" && phoneA == phoneB)

	// Coordinate Distance
	if !recA.HasValidCoords && recA.Record.Latitude != "" && recA.Record.Longitude != "" {
		if latA, errLat := normalizer.ParseCoordinateFromDotted(recA.Record.Latitude); errLat == nil {
			if lonA, errLon := normalizer.ParseCoordinateFromDotted(recA.Record.Longitude); errLon == nil {
				recA.ParsedLat = latA
				recA.ParsedLon = lonA
				recA.HasValidCoords = true
			}
		}
	}
	if !recB.HasValidCoords && recB.Record.Latitude != "" && recB.Record.Longitude != "" {
		if latB, errLat := normalizer.ParseCoordinateFromDotted(recB.Record.Latitude); errLat == nil {
			if lonB, errLon := normalizer.ParseCoordinateFromDotted(recB.Record.Longitude); errLon == nil {
				recB.ParsedLat = latB
				recB.ParsedLon = lonB
				recB.HasValidCoords = true
			}
		}
	}

	hasBothCoords := recA.HasValidCoords && recB.HasValidCoords
	distanceMeters := -1.0
	if hasBothCoords {
		distanceMeters = HaversineDistance(recA.ParsedLat, recA.ParsedLon, recB.ParsedLat, recB.ParsedLon)
	}

	// Rule 2: Same Phone + High Name Similarity
	if phoneMatch && nameSim >= 0.70 {
		return 0.95, StatusHighConfidenceDup, fmt.Sprintf("Identical Phone (%s) and high name similarity (%.0f%%)", phoneA, nameSim*100)
	}

	// Rule 3: High Name Similarity + High Address Similarity (or very close coordinates)
	if nameSim >= 0.75 && (addrSim >= 0.70 || (hasBothCoords && distanceMeters <= d.cfg.StrictSameLocationMeters)) {
		distDesc := "same address"
		if distanceMeters >= 0 {
			distDesc = fmt.Sprintf("distance: %.0fm", distanceMeters)
		}
		return 0.95, StatusHighConfidenceDup, fmt.Sprintf("Highly similar name (%.0f%%) and %s (address sim: %.0f%%)", nameSim*100, distDesc, addrSim*100)
	}

	// Rule 4: Identical address + Moderate name similarity
	if addrSim >= 0.85 && nameSim >= 0.65 {
		return 0.90, StatusHighConfidenceDup, fmt.Sprintf("Identical address (%.0f%%) with similar name (%.0f%%)", addrSim*100, nameSim*100)
	}

	// Rule 5: Same/Nearby Coordinates (< 150m) + High Name Similarity
	if hasBothCoords && distanceMeters <= 150.0 && nameSim >= 0.80 {
		return 0.90, StatusHighConfidenceDup, fmt.Sprintf("Nearby coordinates (%.0fm) and similar name (%.0f%%)", distanceMeters, nameSim*100)
	}

	// Rule 6: Name similarity is high, but address/coordinate is moderately far (Ambiguous / Possible Duplicate)
	if nameSim >= 0.80 {
		if hasBothCoords && distanceMeters > d.cfg.MaxDupDistanceMeters {
			// Very far apart (e.g. > 1.5km) -> Multiple branches or different stores!
			// If it's a known generic chain (e.g. Mitra10, Alfamart), high distance means different branches
			return 0.40, "", "Different branches / far apart"
		}
		if hasBothCoords && distanceMeters > 300.0 && distanceMeters <= d.cfg.MaxDupDistanceMeters {
			return 0.70, StatusFlaggedPossibleDup, fmt.Sprintf("Similar name (%.0f%%) but distant coordinates (%.0fm)", nameSim*100, distanceMeters)
		}
		if !hasBothCoords && addrSim >= 0.40 && addrSim < 0.75 {
			return 0.65, StatusFlaggedPossibleDup, fmt.Sprintf("Similar name (%.0f%%) and moderately similar address (%.0f%%) without exact coordinates", nameSim*100, addrSim*100)
		}
	}

	// Rule 7: Moderate name similarity (65-80%) + Identical phone
	if phoneMatch && nameSim >= 0.50 {
		return 0.75, StatusFlaggedPossibleDup, fmt.Sprintf("Shared phone number (%s) with moderate name similarity (%.0f%%)", phoneA, nameSim*100)
	}

	return 0.0, "", "Distinct stores"
}

// DetectDuplicates runs spatial and key-based blocking to find duplicate groups efficiently
func (d *Deduplicator) DetectDuplicates(records []*ProcessedRecord) []DuplicateCandidate {
	var candidates []DuplicateCandidate

	// 1. Block by Place ID
	placeIDIndex := make(map[string][]int)
	for i, r := range records {
		pID := strings.TrimSpace(r.Record.PlaceID)
		if pID != "" {
			placeIDIndex[pID] = append(placeIDIndex[pID], i)
		}
	}

	// Evaluate Place ID matches
	seenPairs := make(map[string]bool)
	for _, indices := range placeIDIndex {
		if len(indices) > 1 {
			for i := 0; i < len(indices); i++ {
				for j := i + 1; j < len(indices); j++ {
					idxA, idxB := indices[i], indices[j]
					pairKey := fmt.Sprintf("%d-%d", idxA, idxB)
					seenPairs[pairKey] = true

					recA := records[idxA]
					recB := records[idxB]
					score, status, reason := d.EvaluatePair(recA, recB)
					if status != "" {
						candidates = append(candidates, DuplicateCandidate{
							IndexA:          idxA,
							IndexB:          idxB,
							RecordA:         recA,
							RecordB:         recB,
							Score:           score,
							Status:          status,
							Reason:          reason,
							ConfidenceScore: score,
						})
					}
				}
			}
		}
	}

	// 2. Block by Name prefix / first significant word + Kecamatan
	blockIndex := make(map[string][]int)
	for i, r := range records {
		normName := NormalizeStoreName(r.Record.Name)
		words := strings.Fields(normName)
		var keyPrefix string
		if len(words) > 0 {
			keyPrefix = words[0]
			if len(keyPrefix) > 4 {
				keyPrefix = keyPrefix[:4]
			}
		}

		// Also block by Phone if available
		phone := CleanPhoneNumber(r.Record.Phone)
		if phone != "" && len(phone) >= 6 {
			phoneKey := "phone:" + phone
			blockIndex[phoneKey] = append(blockIndex[phoneKey], i)
		}

		if keyPrefix != "" {
			nameKey := "name:" + keyPrefix
			blockIndex[nameKey] = append(blockIndex[nameKey], i)
		}

		// Spatial Grid Block (0.01 deg ~= 1.1 km grid)
		if r.HasValidCoords {
			gridLat := int(r.ParsedLat * 100)
			gridLon := int(r.ParsedLon * 100)
			gridKey := fmt.Sprintf("grid:%d:%d", gridLat, gridLon)
			blockIndex[gridKey] = append(blockIndex[gridKey], i)
		}
	}

	for _, indices := range blockIndex {
		if len(indices) <= 1 {
			continue
		}
		// Sort indices
		for i := 0; i < len(indices); i++ {
			for j := i + 1; j < len(indices); j++ {
				idxA, idxB := indices[i], indices[j]
				if idxA > idxB {
					idxA, idxB = idxB, idxA
				}
				pairKey := fmt.Sprintf("%d-%d", idxA, idxB)
				if seenPairs[pairKey] {
					continue
				}
				seenPairs[pairKey] = true

				recA := records[idxA]
				recB := records[idxB]

				score, status, reason := d.EvaluatePair(recA, recB)
				if status != "" {
					candidates = append(candidates, DuplicateCandidate{
						IndexA:          idxA,
						IndexB:          idxB,
						RecordA:         recA,
						RecordB:         recB,
						Score:           score,
						Status:          status,
						Reason:          reason,
						ConfidenceScore: score,
					})
				}
			}
		}
	}

	return candidates
}

// DisjointSet for connected component grouping
type DisjointSet struct {
	parent map[int]int
}

func newDisjointSet() *DisjointSet {
	return &DisjointSet{parent: make(map[int]int)}
}

func (ds *DisjointSet) find(i int) int {
	if _, ok := ds.parent[i]; !ok {
		ds.parent[i] = i
		return i
	}
	if ds.parent[i] == i {
		return i
	}
	ds.parent[i] = ds.find(ds.parent[i])
	return ds.parent[i]
}

func (ds *DisjointSet) union(i, j int) {
	rootI := ds.find(i)
	rootJ := ds.find(j)
	if rootI != rootJ {
		ds.parent[rootI] = rootJ
	}
}

// ResolveDuplicateGroups groups high confidence duplicates and chooses the best primary record
func (d *Deduplicator) ResolveDuplicateGroups(records []*ProcessedRecord, candidates []DuplicateCandidate) {
	// 1. Group HIGH_CONFIDENCE_DUPLICATE records
	ds := newDisjointSet()
	highConfCandidates := make(map[int][]DuplicateCandidate)

	for _, cand := range candidates {
		if cand.Status == StatusHighConfidenceDup {
			ds.union(cand.IndexA, cand.IndexB)
			highConfCandidates[cand.IndexA] = append(highConfCandidates[cand.IndexA], cand)
			highConfCandidates[cand.IndexB] = append(highConfCandidates[cand.IndexB], cand)
		}
	}

	// Cluster by root
	clusters := make(map[int][]int)
	for i := range records {
		if _, ok := ds.parent[i]; ok {
			root := ds.find(i)
			clusters[root] = append(clusters[root], i)
		}
	}

	groupID := 1
	for _, memberIndices := range clusters {
		if len(memberIndices) <= 1 {
			continue
		}

		grpIDStr := fmt.Sprintf("DUP-GRP-%04d", groupID)
		groupID++

		// Calculate completeness score for all members
		for _, idx := range memberIndices {
			records[idx].CompletenessScore = CalculateCompletenessScore(records[idx].Record)
			records[idx].DuplicateGroupID = grpIDStr
		}

		// Sort members by completeness descending, then by original line ascending
		sort.Slice(memberIndices, func(i, j int) bool {
			idxI := memberIndices[i]
			idxJ := memberIndices[j]
			if records[idxI].CompletenessScore != records[idxJ].CompletenessScore {
				return records[idxI].CompletenessScore > records[idxJ].CompletenessScore
			}
			return records[idxI].OriginalLine < records[idxJ].OriginalLine
		})

		primaryIdx := memberIndices[0]
		primaryRec := records[primaryIdx]

		// Set primary record
		primaryRec.Status = StatusValid // Remains valid in clean dataset

		// Set duplicate removed records
		for _, dupIdx := range memberIndices[1:] {
			dupRec := records[dupIdx]
			dupRec.Status = StatusHighConfidenceDup
			dupRec.RelatedLine = primaryRec.OriginalLine
			dupRec.ConfidenceScore = 1.0

			// Find specific candidate reason if exists
			reason := "Duplicate of Line " + fmt.Sprintf("%d (less complete or identical record)", primaryRec.OriginalLine)
			for _, c := range highConfCandidates[dupIdx] {
				if (c.IndexA == primaryIdx && c.IndexB == dupIdx) || (c.IndexB == primaryIdx && c.IndexA == dupIdx) {
					reason = fmt.Sprintf("Duplicate of Line %d: %s", primaryRec.OriginalLine, c.Reason)
					dupRec.ConfidenceScore = c.ConfidenceScore
					break
				}
			}
			dupRec.Reason = reason
		}
	}

	// 2. Handle FLAGGED_POSSIBLE_DUPLICATE candidates (do NOT merge/remove, flag both or the secondary)
	for _, cand := range candidates {
		if cand.Status == StatusFlaggedPossibleDup {
			recA := records[cand.IndexA]
			recB := records[cand.IndexB]

			// Only flag if not already deduplicated as HIGH_CONFIDENCE_DUPLICATE
			if recA.Status != StatusHighConfidenceDup && recB.Status != StatusHighConfidenceDup {
				if recA.Status == StatusValid {
					recA.Status = StatusFlaggedPossibleDup
					recA.RelatedLine = recB.OriginalLine
					recA.Reason = fmt.Sprintf("Possible duplicate with Line %d: %s", recB.OriginalLine, cand.Reason)
					recA.ConfidenceScore = cand.ConfidenceScore
					recA.RecommendedAction = "Manual review to verify if stores are distinct or same entity"
				}
				if recB.Status == StatusValid {
					recB.Status = StatusFlaggedPossibleDup
					recB.RelatedLine = recA.OriginalLine
					recB.Reason = fmt.Sprintf("Possible duplicate with Line %d: %s", recA.OriginalLine, cand.Reason)
					recB.ConfidenceScore = cand.ConfidenceScore
					recB.RecommendedAction = "Manual review to verify if stores are distinct or same entity"
				}
			}
		}
	}
}
