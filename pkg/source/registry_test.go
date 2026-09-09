package source_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maps-scraper/pkg/source"
)

func TestRegistry_DefaultSeedingAndPersistence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "registry_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storagePath := filepath.Join(tempDir, "trusted_sources.json")

	// 1. New Registry initializes with defaults
	reg := source.NewRegistry(storagePath)
	sources := reg.GetSources()

	if len(sources) != 4 {
		t.Fatalf("expected 4 default sources, got %d: %v", len(sources), sources)
	}

	expectedDefaults := []string{
		"https://www.halodoc.com/artikel",
		"https://ayosehat.kemkes.go.id/topik-az",
		"https://health.detik.com",
		"https://health.kompas.com/",
	}
	for i, exp := range expectedDefaults {
		if sources[i] != exp {
			t.Errorf("expected default source #%d to be %q, got %q", i, exp, sources[i])
		}
	}

	// 2. Verify file was persisted on disk
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		t.Fatalf("persistent file %s was not created", storagePath)
	}

	// 3. Reload from disk
	regReloaded := source.NewRegistry(storagePath)
	reloadedSources := regReloaded.GetSources()
	if len(reloadedSources) != 4 {
		t.Errorf("reloaded sources count mismatch: got %d", len(reloadedSources))
	}
}

func TestRegistry_URLValidationRejection(t *testing.T) {
	invalidInputs := []struct {
		input       string
		description string
	}{
		{"", "empty string"},
		{"   ", "whitespace string"},
		{"Halodoc", "plain name without URL scheme"},
		{"halodoc.com", "domain name without http/https scheme"},
		{"www.halodoc.com/artikel", "URL without http/https scheme"},
		{"ftp://www.halodoc.com/artikel", "unsupported ftp scheme"},
		{"http://localhost", "localhost without dot domain"},
		{"https:///no-host", "missing hostname"},
	}

	for _, tc := range invalidInputs {
		err := source.ValidateSourceURL(tc.input)
		if err == nil {
			t.Errorf("expected validation failure for %s (%q), got nil", tc.description, tc.input)
		} else if !strings.Contains(err.Error(), "http://") && !strings.Contains(err.Error(), "https://") && !strings.Contains(err.Error(), "URL") {
			t.Errorf("unexpected error message for %q: %v", tc.input, err)
		}
	}
}

func TestRegistry_AddAndDeleteSource(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "registry_crud_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	storagePath := filepath.Join(tempDir, "sources.json")
	reg := source.NewRegistry(storagePath)

	// Add URL with query parameters
	customURL := "https://www.halodoc.com/artikel?srsltid=AfmBOopyT-LxR0xJWEsdUpHKAmCeE8NYq6XR08QUzD12IMc1VrjZsgbG"
	if err := reg.AddSource(customURL); err != nil {
		t.Fatalf("failed to add valid URL with query params: %v", err)
	}

	sources := reg.GetSources()
	if len(sources) != 5 {
		t.Fatalf("expected 5 sources after add, got %d", len(sources))
	}

	found := false
	for _, s := range sources {
		if s == customURL {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("added URL not found in registry: %s", customURL)
	}

	// Reject duplicate
	if err := reg.AddSource(customURL); err == nil {
		t.Errorf("expected duplicate add to fail, got nil")
	}

	// Delete source
	if err := reg.DeleteSource(customURL); err != nil {
		t.Fatalf("failed to delete source: %v", err)
	}

	sourcesAfterDelete := reg.GetSources()
	if len(sourcesAfterDelete) != 4 {
		t.Errorf("expected 4 sources after delete, got %d", len(sourcesAfterDelete))
	}

	// Delete non-existent
	if err := reg.DeleteSource("https://example.com/nonexistent"); err == nil {
		t.Errorf("expected error deleting non-existent source, got nil")
	}
}
