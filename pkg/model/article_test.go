package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestArticleJSONSerialization(t *testing.T) {
	art := Article{
		Title:       "Understanding Hypertension",
		Image:       "https://example.com/images/hypertension.jpg",
		Category:    "Cardiology",
		Description: []string{
			"A comprehensive guide on symptoms, causes, and prevention of high blood pressure.",
			"Maintaining lifestyle changes can significantly lower long term cardiovascular risks.",
		},
	}

	data, err := json.Marshal(art)
	if err != nil {
		t.Fatalf("Failed to marshal Article: %v", err)
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(data, &rawMap); err != nil {
		t.Fatalf("Failed to unmarshal into map: %v", err)
	}

	expectedKeys := map[string]bool{
		"title":       true,
		"category":    true,
		"image":       true,
		"description": true,
	}

	if len(rawMap) != len(expectedKeys) {
		t.Errorf("Expected exactly %d fields in JSON, got %d: %v", len(expectedKeys), len(rawMap), rawMap)
	}

	// Verify description is serialized as a single string
	if descStr, ok := rawMap["description"].(string); !ok || descStr == "" {
		t.Errorf("Expected description to be a non-empty string in JSON, got %T (%v)", rawMap["description"], rawMap["description"])
	}

	for k := range rawMap {
		if !expectedKeys[k] {
			t.Errorf("Unexpected field in JSON output: %s", k)
		}
	}

	var unmarshaled Article
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal back to Article: %v", err)
	}

	if !reflect.DeepEqual(art, unmarshaled) {
		t.Errorf("Unmarshaled Article mismatch. Expected %+v, got %+v", art, unmarshaled)
	}

	// Test Image "-" fallback
	artNoImage := Article{
		Title:       "Diabetes Prevention",
		Image:       "-",
		Category:    "Diabetes",
		Description: []string{"Managing blood sugar through balanced nutrition."},
	}
	dataNoImg, err := json.Marshal(artNoImage)
	if err != nil {
		t.Fatalf("Failed to marshal Article with '-' image: %v", err)
	}
	var unmarshaledNoImg Article
	if err := json.Unmarshal(dataNoImg, &unmarshaledNoImg); err != nil {
		t.Fatalf("Failed to unmarshal back: %v", err)
	}
	if unmarshaledNoImg.Image != "-" {
		t.Errorf("Expected image '-', got %q", unmarshaledNoImg.Image)
	}

	// Test SourceURL inclusion
	artWithURL := Article{
		Title:       "Sample Title",
		Image:       "https://example.com/img.jpg",
		Category:    "Health",
		Description: ArticleDescription{"Para 1", "Para 2"},
		SourceURL:   "https://www.halodoc.com/artikel/sample-slug",
	}
	dataWithURL, err := json.Marshal(artWithURL)
	if err != nil {
		t.Fatalf("Marshal artWithURL failed: %v", err)
	}
	var rawWithURL map[string]interface{}
	_ = json.Unmarshal(dataWithURL, &rawWithURL)
	if rawWithURL["source_url"] != "https://www.halodoc.com/artikel/sample-slug" {
		t.Errorf("Expected source_url in JSON, got %v", rawWithURL["source_url"])
	}

	// Test unmarshaling from JSON array
	jsonArrayData := []byte(`{"title":"T","image":"-","category":"C","description":["P1","P2"]}`)
	var fromArray Article
	if err := json.Unmarshal(jsonArrayData, &fromArray); err != nil {
		t.Fatalf("Unmarshal from array failed: %v", err)
	}
	if len(fromArray.Description) != 2 || fromArray.Description[0] != "P1" {
		t.Errorf("Unexpected description from array: %v", fromArray.Description)
	}
}

func TestArticleIDAndScrapedAt(t *testing.T) {
	id1 := GenerateArticleID("Sample Title", "https://example.com/sample")
	id2 := GenerateArticleID("Sample Title", "https://example.com/sample")
	id3 := GenerateArticleID("Other Title", "https://example.com/sample")

	if id1 == "" || !reflect.DeepEqual(id1[:4], "art_") {
		t.Errorf("Expected ID to start with art_, got %q", id1)
	}
	if id1 != id2 {
		t.Errorf("GenerateArticleID should be deterministic for same inputs: %s != %s", id1, id2)
	}
	if id1 == id3 {
		t.Errorf("GenerateArticleID should produce different IDs for different inputs: %s == %s", id1, id3)
	}

	art := Article{
		ID:          id1,
		Title:       "Sample Title",
		Image:       "https://example.com/img.jpg",
		Category:    "Cardiology",
		Description: ArticleDescription{"Paragraph one"},
		SourceURL:   "https://example.com/sample",
		ScrapedAt:   "2026-09-07T10:00:00Z",
	}

	data, err := json.Marshal(art)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var parsed Article
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if parsed.ID != id1 {
		t.Errorf("Expected ID %q, got %q", id1, parsed.ID)
	}
	if parsed.ScrapedAt != "2026-09-07T10:00:00Z" {
		t.Errorf("Expected ScrapedAt '2026-09-07T10:00:00Z', got %q", parsed.ScrapedAt)
	}
}
