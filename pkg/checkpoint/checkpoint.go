package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type QueryState string

const (
	StatePending         QueryState = "PENDING"
	StateRunning         QueryState = "RUNNING"
	StateSuccess         QueryState = "SUCCESS"
	StateFailed          QueryState = "FAILED"
	StateFailedPermanent QueryState = "FAILED_PERMANENT"
	StateRetry           QueryState = "RETRY"
)

type QueryStatusRecord struct {
	ID          int        `json:"id"`
	Query       string     `json:"query"`
	Status      QueryState `json:"status"`
	Attempts    int        `json:"attempts"`
	Error       string     `json:"error,omitempty"`
	StoresFound int        `json:"stores_found"`
	WorkerID    int        `json:"worker_id,omitempty"`
	StartedAt   time.Time  `json:"started_at,omitempty"`
	CompletedAt time.Time  `json:"completed_at,omitempty"`
}

type CheckpointData struct {
	JobID            string                       `json:"job_id,omitempty"`
	Location         string                       `json:"location,omitempty"`
	StartQueryID     int                          `json:"start_query_id"`
	EndQueryID       int                          `json:"end_query_id"`
	Direction        string                       `json:"direction"`
	TotalQueries     int                          `json:"total_queries"`
	CompletedCount   int                          `json:"completed_count"`
	FailedCount      int                          `json:"failed_count"`
	PendingCount     int                          `json:"pending_count"`
	RunningCount     int                          `json:"running_count"`
	StoresDiscovered int                          `json:"stores_discovered"`
	LastUpdated      time.Time                    `json:"last_updated"`
	Queries          map[string]*QueryStatusRecord `json:"queries"`
}

type CheckpointManager struct {
	filePath string
	mu       sync.RWMutex
	data     *CheckpointData
}

// NewCheckpointManager initializes or loads an existing checkpoint
func NewCheckpointManager(filePath string, allQueries []string, startID, endID int, direction string) (*CheckpointManager, error) {
	cm := &CheckpointManager{
		filePath: filePath,
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil && filepath.Dir(filePath) != "." && filepath.Dir(filePath) != "" {
		return nil, fmt.Errorf("failed to create checkpoint dir: %w", err)
	}

	if _, err := os.Stat(filePath); err == nil {
		// Load existing
		if err := cm.load(allQueries); err != nil {
			return nil, fmt.Errorf("failed to load checkpoint: %w", err)
		}
	} else {
		// Initialize new
		cm.data = &CheckpointData{
			StartQueryID: startID,
			EndQueryID:   endID,
			Direction:    direction,
			TotalQueries: len(allQueries),
			Queries:      make(map[string]*QueryStatusRecord),
			LastUpdated:  time.Now(),
		}

		for idx, q := range allQueries {
			id := idx + 1
			cm.data.Queries[strconv.Itoa(id)] = &QueryStatusRecord{
				ID:       id,
				Query:    q,
				Status:   StatePending,
				Attempts: 0,
			}
		}
		cm.recalculateCounts()
		if err := cm.save(); err != nil {
			return nil, fmt.Errorf("failed to save initial checkpoint: %w", err)
		}
	}

	return cm, nil
}

func (cm *CheckpointManager) load(allQueries []string) error {
	bytes, err := os.ReadFile(cm.filePath)
	if err != nil {
		return err
	}

	var data CheckpointData
	if err := json.Unmarshal(bytes, &data); err != nil {
		return err
	}

	if data.Queries == nil {
		data.Queries = make(map[string]*QueryStatusRecord)
	}

	// Ensure all queries are registered and reset any lingering 'RUNNING' or canceled status back to 'PENDING'
	for idx, q := range allQueries {
		id := idx + 1
		key := strconv.Itoa(id)
		if rec, exists := data.Queries[key]; exists {
			if rec.Status == StateRunning || (rec.Status == StateFailedPermanent && strings.Contains(strings.ToLower(rec.Error), "context cancel")) {
				rec.Status = StatePending
				rec.Error = ""
			}
		} else {
			data.Queries[key] = &QueryStatusRecord{
				ID:       id,
				Query:    q,
				Status:   StatePending,
				Attempts: 0,
			}
		}
	}

	cm.data = &data
	cm.recalculateCounts()
	return cm.save()
}

func (cm *CheckpointManager) recalculateCounts() {
	completed := 0
	failed := 0
	pending := 0
	running := 0
	stores := 0

	for _, rec := range cm.data.Queries {
		stores += rec.StoresFound
		switch rec.Status {
		case StateSuccess:
			completed++
		case StateFailedPermanent:
			failed++
		case StateRunning:
			running++
		default:
			pending++
		}
	}

	cm.data.CompletedCount = completed
	cm.data.FailedCount = failed
	cm.data.PendingCount = pending
	cm.data.RunningCount = running
	cm.data.StoresDiscovered = stores
	cm.data.LastUpdated = time.Now()
}

func (cm *CheckpointManager) GetData() CheckpointData {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return *cm.data
}

func (cm *CheckpointManager) MarkRunning(id int, workerID int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	key := strconv.Itoa(id)
	if rec, ok := cm.data.Queries[key]; ok {
		rec.Status = StateRunning
		rec.WorkerID = workerID
		rec.StartedAt = time.Now()
		rec.Attempts++
		cm.recalculateCounts()
		_ = cm.save()
	}
}

func (cm *CheckpointManager) MarkSuccess(id int, storesFound int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	key := strconv.Itoa(id)
	if rec, ok := cm.data.Queries[key]; ok {
		rec.Status = StateSuccess
		rec.StoresFound = storesFound
		rec.Error = ""
		rec.CompletedAt = time.Now()
		cm.recalculateCounts()
		_ = cm.save()
	}
}

func (cm *CheckpointManager) MarkFailed(id int, errReason string, permanent bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	key := strconv.Itoa(id)
	if rec, ok := cm.data.Queries[key]; ok {
		if permanent {
			rec.Status = StateFailedPermanent
		} else {
			rec.Status = StateRetry
		}
		rec.Error = errReason
		rec.CompletedAt = time.Now()
		cm.recalculateCounts()
		_ = cm.save()
	}
}

func (cm *CheckpointManager) MarkPending(id int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	key := strconv.Itoa(id)
	if rec, ok := cm.data.Queries[key]; ok {
		rec.Status = StatePending
		rec.Error = ""
		cm.recalculateCounts()
		_ = cm.save()
	}
}

func (cm *CheckpointManager) GetRecord(id int) (*QueryStatusRecord, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	rec, ok := cm.data.Queries[strconv.Itoa(id)]
	if !ok {
		return nil, false
	}
	// return copy
	cp := *rec
	return &cp, true
}

func (cm *CheckpointManager) SetMetadata(jobID, location string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.data != nil {
		if jobID != "" {
			cm.data.JobID = jobID
		}
		if location != "" {
			cm.data.Location = location
		}
		_ = cm.save()
	}
}

func (cm *CheckpointManager) GetRemainingQueryCount(startID, endID int) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.data == nil {
		return 0
	}

	if startID > endID {
		startID, endID = endID, startID
	}

	remaining := 0
	for id := startID; id <= endID; id++ {
		rec, exists := cm.data.Queries[strconv.Itoa(id)]
		if !exists || (rec.Status != StateSuccess && rec.Status != StateFailedPermanent) {
			remaining++
		}
	}
	return remaining
}

func (cm *CheckpointManager) GetSummary() (completed, running, pending, failed, stores int) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.data.CompletedCount, cm.data.RunningCount, cm.data.PendingCount, cm.data.FailedCount, cm.data.StoresDiscovered
}

func (cm *CheckpointManager) save() error {
	dataBytes, err := json.MarshalIndent(cm.data, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := cm.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, dataBytes, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, cm.filePath)
}
