package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"maps-scraper/pkg/cleaner"
	"maps-scraper/pkg/model"
)

func main() {
	jsonFiles := []string{
		"output/articles.json",
		"data/run_articles.json",
		"pkg/api/output/articles.json",
		"pkg/api/data/run_articles.json",
	}

	cl := cleaner.NewArticleCleaner(cleaner.DefaultCleanerConfig())

	totalFixed := 0

	for _, relPath := range jsonFiles {
		absPath, err := filepath.Abs(relPath)
		if err != nil {
			log.Printf("Could not resolve path %s: %v", relPath, err)
			continue
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("File not found, skipping: %s", relPath)
				continue
			}
			log.Printf("Error reading %s: %v", relPath, err)
			continue
		}

		var articles []model.Article
		if err := json.Unmarshal(data, &articles); err != nil {
			log.Printf("Error parsing JSON from %s: %v", relPath, err)
			continue
		}

		log.Printf("=== Auditing %s (%d articles) ===", relPath, len(articles))
		fileModifiedCount := 0

		for i := range articles {
			origDesc := articles[i].Description
			cleanedDesc := cl.CleanParagraphs(origDesc)

			origImg := articles[i].Image
			cleanedImg := cl.CleanImageURL(origImg)

			// Check if any change occurred
			changed := false
			if len(origDesc) != len(cleanedDesc) {
				changed = true
			} else {
				for j := range origDesc {
					if origDesc[j] != cleanedDesc[j] {
						changed = true
						break
					}
				}
			}

			if origImg != cleanedImg {
				changed = true
				log.Printf("\n[FIXED IMAGE] ID: %s | Title: %s\n  Before: %s\n  After:  %s", articles[i].ID, articles[i].Title, origImg, cleanedImg)
				articles[i].Image = cleanedImg
			}

			if changed {
				if len(origDesc) != len(cleanedDesc) {
					log.Printf("\n[FIXED ARTICLE] ID: %s | Title: %s", articles[i].ID, articles[i].Title)
					articles[i].Description = cleanedDesc
				}
				fileModifiedCount++
				totalFixed++
			}
		}

		if fileModifiedCount > 0 {
			updatedData, err := json.MarshalIndent(articles, "", "  ")
			if err != nil {
				log.Printf("Error encoding cleaned articles for %s: %v", relPath, err)
				continue
			}
			if err := os.WriteFile(absPath, updatedData, 0644); err != nil {
				log.Printf("Error writing updated file %s: %v", relPath, err)
				continue
			}
			log.Printf("Successfully updated %s (%d articles cleaned)", relPath, fileModifiedCount)
		} else {
			log.Printf("No article boundary issues found in %s", relPath)
		}
	}

	fmt.Printf("\nCleaning complete! Total articles fixed: %d\n", totalFixed)
}
