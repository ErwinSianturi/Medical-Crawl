package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultStoragePath defines the persistent file location for trusted sources.
const DefaultStoragePath = "data/trusted_sources.json"

// InitialDefaultSources contains the mandatory seed medical source URLs.
var InitialDefaultSources = []string{
	"https://www.halodoc.com/artikel",
	"https://ayosehat.kemkes.go.id/topik-az",
	"https://health.detik.com",
	"https://health.kompas.com/",
}


// TrustedSourceItem represents a single trusted source URL entry with metadata.
type TrustedSourceItem struct {
	URL      string `json:"url"`
	Hostname string `json:"hostname"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
}

// Registry manages thread-safe persistent configuration of trusted medical source URLs.
type Registry struct {
	mu          sync.RWMutex
	storagePath string
	sources     []string
}

var (
	defaultRegistry     *Registry
	defaultRegistryOnce sync.Once
)

// GetRegistry returns the singleton Registry instance.
func GetRegistry() *Registry {
	defaultRegistryOnce.Do(func() {
		defaultRegistry = NewRegistry(DefaultStoragePath)
	})
	return defaultRegistry
}

// SetRegistry sets the active default Registry (used for testing and configuration).
func SetRegistry(r *Registry) {
	defaultRegistry = r
}

// NewRegistry creates a new Registry loading from the specified storage path.
func NewRegistry(storagePath string) *Registry {
	if storagePath == "" {
		storagePath = DefaultStoragePath
	}

	r := &Registry{
		storagePath: storagePath,
		sources:     make([]string, 0),
	}

	if err := r.load(); err != nil || len(r.sources) == 0 {
		r.sources = make([]string, len(InitialDefaultSources))
		copy(r.sources, InitialDefaultSources)
		_ = r.saveLocked()
	} else {
		r.mu.Lock()
		modified := false
		for _, def := range InitialDefaultSources {
			found := false
			for _, s := range r.sources {
				if strings.EqualFold(strings.TrimRight(s, "/"), strings.TrimRight(def, "/")) {
					found = true
					break
				}
			}
			if !found {
				r.sources = append(r.sources, def)
				modified = true
			}
		}
		if modified {
			_ = r.saveLocked()
		}
		r.mu.Unlock()
	}

	return r
}

func (r *Registry) load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.storagePath)
	if err != nil {
		return err
	}

	var loaded []string
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}

	r.sources = make([]string, 0, len(loaded))
	for _, u := range loaded {
		trimmed := strings.TrimSpace(u)
		if trimmed != "" && r.validateURL(trimmed) == nil {
			r.sources = append(r.sources, trimmed)
		}
	}

	return nil
}

func (r *Registry) saveLocked() error {
	dir := filepath.Dir(r.storagePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", r.storagePath, err)
		}
	}

	data, err := json.MarshalIndent(r.sources, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmpFile := r.storagePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}

	_ = os.Remove(r.storagePath)
	if err := os.Rename(tmpFile, r.storagePath); err != nil {
		_ = os.Remove(tmpFile)
		return os.WriteFile(r.storagePath, data, 0644)
	}

	return nil
}

// ValidateSourceURL checks that an input is a valid HTTP/HTTPS URL and not a plain site name or domain.
func ValidateSourceURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return errors.New("URL tidak boleh kosong")
	}

	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return errors.New("URL tidak valid. Harus diawali dengan http:// atau https:// (contoh: https://www.halodoc.com/artikel)")
	}

	u, err := url.ParseRequestURI(trimmed)
	if err != nil {
		return fmt.Errorf("format URL tidak valid: %w", err)
	}

	host := strings.ToLower(u.Hostname())
	if host == "" || !strings.Contains(host, ".") || host == "localhost" {
		return errors.New("nama host/domain URL tidak valid")
	}

	return nil
}

func (r *Registry) validateURL(rawURL string) error {
	return ValidateSourceURL(rawURL)
}

// GetSources returns a copy of all active trusted source URLs.
func (r *Registry) GetSources() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]string, len(r.sources))
	copy(result, r.sources)
	return result
}

// GetSourceItems returns enriched metadata for each active trusted source.
func (r *Registry) GetSourceItems() []TrustedSourceItem {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]TrustedSourceItem, 0, len(r.sources))
	for _, u := range r.sources {
		parsed, _ := url.Parse(u)
		host := ""
		name := u
		if parsed != nil {
			host = parsed.Hostname()
			if strings.Contains(host, "halodoc.com") {
				name = "Halodoc"
			} else if strings.Contains(host, "detik.com") {
				name = "Detik Health"
			} else if strings.Contains(host, "kemkes.go.id") {
				name = "Kemenkes RI"
			} else if strings.Contains(host, "medlineplus.gov") {
				name = "NIH MedlinePlus"
			} else if strings.Contains(host, "kompas.com") {
				name = "Kompas Health"
			} else {
				name = host
			}
		}

		items = append(items, TrustedSourceItem{
			URL:      u,
			Hostname: host,
			Name:     name,
			IsActive: true,
		})
	}
	return items
}

// AddSource adds a new trusted URL to the persistent registry.
func (r *Registry) AddSource(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if err := ValidateSourceURL(trimmed); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if already registered (case-insensitive on URL)
	for _, s := range r.sources {
		if strings.EqualFold(s, trimmed) {
			return fmt.Errorf("URL sumber sudah terdaftar: %s", trimmed)
		}
	}

	r.sources = append(r.sources, trimmed)
	return r.saveLocked()
}

// DeleteSource removes a URL from the active trusted registry.
func (r *Registry) DeleteSource(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return errors.New("URL tidak boleh kosong")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	found := false
	newSources := make([]string, 0, len(r.sources))
	for _, s := range r.sources {
		if strings.EqualFold(s, trimmed) {
			found = true
			continue
		}
		newSources = append(newSources, s)
	}

	if !found {
		return fmt.Errorf("URL sumber tidak ditemukan: %s", trimmed)
	}

	r.sources = newSources
	return r.saveLocked()
}

// ResetToDefaults resets the registry back to initial seed default sources.
func (r *Registry) ResetToDefaults() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sources = make([]string, len(InitialDefaultSources))
	copy(r.sources, InitialDefaultSources)
	return r.saveLocked()
}
