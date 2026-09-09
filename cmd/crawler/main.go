package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"maps-scraper/pkg/crawler"
	"maps-scraper/pkg/model"
	"maps-scraper/pkg/source/detik"
	"maps-scraper/pkg/source/halodoc"
	"maps-scraper/pkg/source/kemenkes"
	"maps-scraper/pkg/source/kompas"
	"maps-scraper/pkg/source/medlineplus"
)

func main() {
	topicsFlag := flag.String("topics", "all", "Comma-separated search topics or 'all'")
	sourceFlag := flag.String("source", "all", "Filter sources: 'all', 'halodoc', 'detik', 'kemenkes', 'medlineplus'")
	outputFlag := flag.String("output", crawler.DefaultOutputPath, "Target output JSON path")
	limitFlag := flag.Int("limit", 9, "Maximum total articles to crawl and save")
	perSourceFlag := flag.Int("per-source", 3, "Target articles per source")
	workersFlag := flag.Int("workers", 3, "Number of concurrent processing workers")
	flag.Parse()

	fmt.Println("================================================================================")
	fmt.Println("              🏥 MEDICAL ARTICLE CRAWLER (TRUSTED SOURCES) 🏥                   ")
	fmt.Println("================================================================================")
	fmt.Printf("[CONFIG] Target Output : %s\n", *outputFlag)
	fmt.Printf("[CONFIG] Source Filter : %s\n", *sourceFlag)
	fmt.Printf("[CONFIG] Search Topics : %s\n", *topicsFlag)
	fmt.Printf("[CONFIG] Article Limit : %d\n", *limitFlag)
	fmt.Printf("[CONFIG] Workers       : %d\n", *workersFlag)
	fmt.Println("================================================================================")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 1. Initialize Storage
	storage, err := crawler.NewJSONStorage(*outputFlag, true)
	if err != nil {
		fmt.Printf("[FATAL] Failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	// 2. Initialize Processor (with AutoClassify and Clean & Dedup pipeline)
	procCfg := crawler.DefaultArticleProcessorConfig()
	procCfg.AutoClassify = true
	proc := crawler.NewArticleProcessor(procCfg)

	// 3. Initialize Source Adapters
	medlineAdapter := medlineplus.NewAdapter(nil)
	kemenkesAdapter := kemenkes.NewAdapter(nil)
	halodocAdapter := halodoc.NewAdapter(nil)
	detikAdapter := detik.NewAdapter(nil)
	kompasAdapter := kompas.NewAdapter(nil)

	type sourceRunner struct {
		name   string
		search func(ctx context.Context, query string, maxArticles int) ([]model.Article, error)
	}

	hSrc := sourceRunner{name: "Halodoc", search: halodocAdapter.SearchWithLimit}
	dSrc := sourceRunner{name: "detikHealth", search: detikAdapter.SearchWithLimit}
	kompasSrc := sourceRunner{name: "Kompas Health", search: kompasAdapter.SearchWithLimit}
	kSrc := sourceRunner{name: "Kemenkes RI", search: func(ctx context.Context, query string, maxArticles int) ([]model.Article, error) {
		return kemenkesAdapter.Search(ctx, query)
	}}
	mSrc := sourceRunner{name: "MedlinePlus", search: func(ctx context.Context, query string, maxArticles int) ([]model.Article, error) {
		return medlineAdapter.Search(ctx, query)
	}}

	var sources []sourceRunner
	sFilter := strings.ToLower(strings.TrimSpace(*sourceFlag))
	if sFilter == "all" || sFilter == "" {
		sources = []sourceRunner{hSrc, dSrc, kSrc, kompasSrc}
	} else if strings.Contains(sFilter, "halodoc") {
		sources = append(sources, hSrc)
	} else if strings.Contains(sFilter, "detik") {
		sources = append(sources, dSrc)
	} else if strings.Contains(sFilter, "kemenkes") {
		sources = append(sources, kSrc)
	} else if strings.Contains(sFilter, "medline") {
		sources = append(sources, mSrc)
	} else if strings.Contains(sFilter, "kompas") {
		sources = append(sources, kompasSrc)
	} else {
		sources = []sourceRunner{hSrc, dSrc, kSrc, kompasSrc}
	}

	topics := strings.Split(*topicsFlag, ",")
	savedCount := 0

	fmt.Println("[CRAWLER] Beginning article acquisition from approved sources...")

	for _, src := range sources {
		if savedCount >= *limitFlag {
			break
		}

		sourceSaved := 0
		neededForSource := *perSourceFlag
		for _, topic := range topics {
			if sourceSaved >= *perSourceFlag || savedCount >= *limitFlag {
				break
			}

			cleanTopic := strings.TrimSpace(topic)
			if cleanTopic == "" {
				continue
			}

			fmt.Printf("[SEARCH] [%s] Querying for topic: %q (requesting %d articles)\n", src.name, cleanTopic, neededForSource-sourceSaved)
			articles, err := src.search(ctx, cleanTopic, neededForSource-sourceSaved)
			if err != nil {
				fmt.Printf("[WARNING] Search failed on %s for topic %q: %v\n", src.name, cleanTopic, err)
				continue
			}

			fmt.Printf("[FETCH] Retrieved %d candidate articles from %s for %q\n", len(articles), src.name, cleanTopic)

			for _, raw := range articles {
				if sourceSaved >= *perSourceFlag || savedCount >= *limitFlag {
					break
				}

				// Clean, validate, and deduplicate
				cleaned, err := proc.Process(ctx, &raw, "")
				if err != nil {
					// Skip duplicates or invalid articles
					continue
				}

				if err := storage.Save(*cleaned); err != nil {
					fmt.Printf("[ERROR] Storage save failed: %v\n", err)
					continue
				}

				savedCount++
				sourceSaved++
				fmt.Printf("  [%s #%d | Total %02d/%02d] Saved: %q | Cat: [%s]\n", src.name, sourceSaved, savedCount, *limitFlag, cleaned.Title, cleaned.Category)
			}
		}
	}

	// Flush and finalize storage
	if err := storage.Close(); err != nil {
		fmt.Printf("[ERROR] Storage close failed: %v\n", err)
	}

	fmt.Println("================================================================================")
	fmt.Printf("[FINISH] Crawling complete. Total saved: %d\n", savedCount)
	fmt.Println("================================================================================")

	// 4. Programmatic Output Verification
	fmt.Println("[VERIFY] Verifying JSON file integrity and strict 4-field schema...")
	count, err := crawler.VerifyArticlesJSON(*outputFlag)
	if err != nil {
		fmt.Printf("[VERIFY FAILED] %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[VERIFY SUCCESS] Programmatically validated %d articles in %s\n", count, *outputFlag)
	fmt.Println("[SCHEMA] Every JSON object confirmed to contain STRICTLY:")
	fmt.Println("         - title")
	fmt.Println("         - category")
	fmt.Println("         - image")
	fmt.Println("         - description")
	fmt.Println("================================================================================")
}
