package kompas

import (
	"context"
	"testing"
)

func TestKompasFetch(t *testing.T) {
	adapter := NewAdapter(nil)
	art, err := adapter.FetchAndParse(context.Background(), "https://health.kompas.com/read/26I07143200268/13-cara-melindungi-anak-dari-paparan-abu-vulkanik-menurut-idai", "")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	if art.Title == "" {
		t.Errorf("Title is empty")
	} else {
		t.Logf("Title: %s", art.Title)
	}
	
	if art.Category == "" {
		t.Errorf("Category is empty")
	} else {
		t.Logf("Category: %s", art.Category)
	}
	
	t.Logf("Content Length: %d paragraphs", len(art.Description))
	for i, p := range art.Description {
		t.Logf("P%d: %s", i, p)
	}
}
