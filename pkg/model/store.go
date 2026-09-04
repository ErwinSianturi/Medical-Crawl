package model

import "strings"


// StoreRecord represents the exact contract schema of the reference CSV
type StoreRecord struct {
	PlaceID               string
	Name                  string
	Address               string
	City                  string
	Kecamatan             string
	Latitude              string
	Longitude             string
	Types                 string
	Rating                string
	Phone                 string
	Status                string
	OpeningHours          string
	PhotoURL              string
	WebsiteLinks          string
	SemenYangDijual       string
	LinkSetinganTitik     string
	LinkSetinganKoma      string
	WorkerID              int `json:"worker_id,omitempty"`
}

// Headers returns the exact 17 CSV headers in expected sequence
func Headers() []string {
	return []string{
		"Place ID",
		"Name",
		"Address",
		"City",
		"Kecamatan",
		"Latitude",
		"Longitude",
		"Types",
		"Rating",
		"Phone",
		"Status",
		"Opening Hours",
		"Photo URL",
		"Website/Links",
		"Semen Yang Dijual",
		"Link (Setingan Titik (.))",
		"Link (Setingan Koma (,))",
	}
}

// ToRow converts the record to a slice of strings in exact header order
func (r StoreRecord) ToRow() []string {
	return []string{
		r.PlaceID,
		r.Name,
		r.Address,
		r.City,
		r.Kecamatan,
		r.Latitude,
		r.Longitude,
		r.Types,
		r.Rating,
		r.Phone,
		r.Status,
		r.OpeningHours,
		r.PhotoURL,
		r.WebsiteLinks,
		r.SemenYangDijual,
		r.LinkSetinganTitik,
		r.LinkSetinganKoma,
	}
}

// LocationConfig defines multi-region target parameters
type LocationConfig struct {
	Country        string `json:"country"`
	Province       string `json:"province"`
	City           string `json:"city"`
	District       string `json:"district"`
	Neighborhood   string `json:"neighborhood"`
	CustomLocation string `json:"custom_location"`
}

func (l LocationConfig) GetSearchString() string {
	if strings.TrimSpace(l.CustomLocation) != "" {
		return strings.TrimSpace(l.CustomLocation)
	}
	parts := []string{}
	if strings.TrimSpace(l.District) != "" && !strings.EqualFold(l.District, "All Districts") {
		parts = append(parts, l.District)
	}
	if strings.TrimSpace(l.City) != "" {
		parts = append(parts, l.City)
	}
	if strings.TrimSpace(l.Province) != "" {
		parts = append(parts, l.Province)
	}
	if strings.TrimSpace(l.Country) != "" {
		parts = append(parts, l.Country)
	}
	return strings.Join(parts, ", ")
}

// AdvancedSettings controls scraping engine behaviors
type AdvancedSettings struct {
	SearchNearby          bool `json:"search_nearby"`
	SearchByDistrict      bool `json:"search_by_district"`
	SearchByNeighborhood  bool `json:"search_by_neighborhood"`
	ExpandSearch          bool `json:"expand_search"`
	RemoveDuplicates      bool `json:"remove_duplicates"`
	ValidateLocation      bool `json:"validate_location"`
	ValidateCategory      bool `json:"validate_category"`
	SkipPermanentlyClosed bool `json:"skip_permanently_closed"`
	CollectOpeningHours   bool `json:"collect_opening_hours"`
	CollectPhoneNumbers   bool `json:"collect_phone_numbers"`
	CollectRatings        bool `json:"collect_ratings"`
	CollectReviewCounts   bool `json:"collect_review_counts"`
	CollectCoordinates    bool `json:"collect_coordinates"`
}

func DefaultAdvancedSettings() AdvancedSettings {
	return AdvancedSettings{
		SearchNearby:          false,
		SearchByDistrict:      true,
		SearchByNeighborhood:  false,
		ExpandSearch:          true,
		RemoveDuplicates:      true,
		ValidateLocation:      true,
		ValidateCategory:      true,
		SkipPermanentlyClosed: false,
		CollectOpeningHours:   true,
		CollectPhoneNumbers:   true,
		CollectRatings:        true,
		CollectReviewCounts:   true,
		CollectCoordinates:    true,
	}
}

// JobConfig defines a complete scraping task specification
type JobConfig struct {
	ID             string           `json:"id"`
	Title          string           `json:"title"`
	Target         string           `json:"target"`
	Location       LocationConfig   `json:"location"`
	Queries        []string         `json:"queries"`
	TargetRecords  int              `json:"target_records"`
	Workers        int              `json:"workers"`
	BatchSize      int              `json:"batch_size"`
	Headless       bool             `json:"headless"`
	Retries        int              `json:"retries"`
	MinDelayMs     int              `json:"min_delay_ms"`
	MaxDelayMs     int              `json:"max_delay_ms"`
	TimeoutMs      int              `json:"timeout_ms"`
	Settings       AdvancedSettings `json:"settings"`
	SelectedFields []string         `json:"selected_fields"`
	OutputFile     string           `json:"output_file,omitempty"`
	CreatedAt      string           `json:"created_at"`
}

// JobStatus string enum
type JobStatus string

const (
	StatusQueued    JobStatus = "QUEUED"
	StatusRunning   JobStatus = "RUNNING"
	StatusPaused    JobStatus = "PAUSED"
	StatusCompleted JobStatus = "COMPLETED"
	StatusStopped   JobStatus = "STOPPED"
	StatusFailed    JobStatus = "FAILED"
)

// WorkerStatus enum for individual worker states
type WorkerStatus string

const (
	WorkerStatusQueued    WorkerStatus = "QUEUED"
	WorkerStatusStarting  WorkerStatus = "STARTING"
	WorkerStatusRunning   WorkerStatus = "RUNNING"
	WorkerStatusPaused    WorkerStatus = "PAUSED"
	WorkerStatusCompleted WorkerStatus = "COMPLETED"
	WorkerStatusStopped   WorkerStatus = "STOPPED"
	WorkerStatusFailed    WorkerStatus = "FAILED"
	WorkerStatusIdle      WorkerStatus = "IDLE"
)

// WorkerState represents the real-time execution telemetry of an individual Playwright worker
type WorkerState struct {
	ID                 int          `json:"id"`
	Status             WorkerStatus `json:"status"`
	Progress           float64      `json:"progress"`
	RecordsFound       int          `json:"records_found"`
	RecordsValid       int          `json:"records_valid"`
	Duplicates         int          `json:"duplicates"`
	Errors             int          `json:"errors"`
	CurrentQuery       string       `json:"current_query"`
	CurrentLocation    string       `json:"current_location"`
	CurrentBatch       int          `json:"current_batch"`
	Speed              float64      `json:"records_per_min"`
	ElapsedTime        string       `json:"elapsed_time"`
	ETA                string       `json:"eta"`
	ErrorMessage       string       `json:"error_message,omitempty"`
	RetryCount         int          `json:"retry_count"`
	MaxRetries         int          `json:"max_retries"`
	LastScrapedAt      string       `json:"last_scraped_at,omitempty"`
}

// JobStats holds counts & execution statistics
type JobStats struct {
	TotalQueries     int     `json:"total_queries"`
	CompletedQueries int     `json:"completed_queries"`
	Found            int     `json:"found"`
	Valid            int     `json:"valid"`
	Duplicates       int     `json:"duplicates"`
	Invalid          int     `json:"invalid"`
	Errors           int     `json:"errors"`
	RequestsPerMin   float64 `json:"requests_per_min"`
	RecordsPerMin    float64 `json:"records_per_min"`
	AvgResponseMs    float64 `json:"avg_response_ms"`
	ActiveWorkers    int     `json:"active_workers"`
	CPUUsage         float64 `json:"cpu_usage"`
	MemoryUsage      float64 `json:"memory_usage"`
	MemoryUsageMB    float64 `json:"memory_usage_mb"`
}

// IterationStatus enum
type IterationStatus string

const (
	IterationPending     IterationStatus = "PENDING"
	IterationRunning     IterationStatus = "RUNNING"
	IterationPaused      IterationStatus = "PAUSED"
	IterationCompleted   IterationStatus = "COMPLETED"
	IterationFailed      IterationStatus = "FAILED"
	IterationInterrupted IterationStatus = "INTERRUPTED"
	IterationPartial     IterationStatus = "PARTIAL"
)

// IterationState represents a single execution session of a job
type IterationState struct {
	ID               string          `json:"iteration_id"`
	JobID            string          `json:"job_id"`
	Number           int             `json:"iteration_number"`
	Strategy         string          `json:"strategy"` // "query" or "stores"
	StartQuery       int             `json:"start_query"`
	EndQuery         int             `json:"end_query"`
	TargetQueries    int             `json:"target_queries"`
	TargetStores     int             `json:"target_stores"`
	QueriesProcessed int             `json:"queries_processed"`
	QueriesSuccess   int             `json:"queries_success"`
	QueriesFailed    int             `json:"queries_failed"`
	StoresFound      int             `json:"stores_found"`
	StoresSaved      int             `json:"stores_saved"`
	DuplicateCount   int             `json:"duplicate_count"`
	Status           IterationStatus `json:"status"`
	StartedAt        string          `json:"started_at"`
	CompletedAt      string          `json:"completed_at,omitempty"`
	PausedAt         string          `json:"paused_at,omitempty"`
	Error            string          `json:"error,omitempty"`
}

// JobState represents the runtime state of a job
type JobState struct {
	Config            JobConfig        `json:"config"`
	Status            JobStatus        `json:"status"`
	Progress          float64          `json:"progress"`
	CurrentCount      int              `json:"current_count"`
	TargetCount       int              `json:"target_count"`
	ElapsedTime       string           `json:"elapsed_time"`
	ETA               string           `json:"eta"`
	ErrorMessage      string           `json:"error_message,omitempty"`
	OutputFile        string           `json:"output_file,omitempty"`
	Stats             JobStats         `json:"stats"`
	Workers           []WorkerState    `json:"workers"`
	Records           []StoreRecord    `json:"-"`
	Logs              []LogEntry       `json:"logs,omitempty"`
	ActiveIterationID string           `json:"active_iteration_id,omitempty"`
	Iterations        []IterationState `json:"iterations"`
	StartedAt         string           `json:"started_at"`
	EndedAt           string           `json:"ended_at,omitempty"`
}

// RecordsResponse defines paginated response with CSV metadata
type RecordsResponse struct {
	JobID         string        `json:"job_id"`
	Total         int           `json:"total"`
	Page          int           `json:"page"`
	Limit         int           `json:"limit"`
	TotalPages    int           `json:"total_pages"`
	OutputFile    string        `json:"output_file"`
	FileExists    bool          `json:"file_exists"`
	FileSizeMB    float64       `json:"file_size_mb"`
	Records       []StoreRecord `json:"records"`
	Diagnostic    string        `json:"diagnostic,omitempty"`
}



type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"` // INFO, WARN, ERROR
	Message   string `json:"message"`
}

type PresetConfig struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Target      string         `json:"target"`
	Location    LocationConfig `json:"location"`
	Queries     []string       `json:"queries"`
	Workers     int            `json:"workers"`
	BatchSize   int            `json:"batch_size"`
	Headless    bool           `json:"headless"`
}

