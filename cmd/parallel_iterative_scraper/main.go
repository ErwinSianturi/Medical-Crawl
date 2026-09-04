package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"maps-scraper/pkg/database"
	"maps-scraper/pkg/parallel"
	"maps-scraper/pkg/query"
)

func main() {
	inputCSV := flag.String("input", "Scrape_Iterative.csv", "Base CSV file for baseline deduplication")
	startQuery := flag.Int("start", 2865, "Starting query index (1 to 2865)")
	endQuery := flag.Int("end", 1, "Ending query index (1 to 2865)")
	direction := flag.String("direction", "reverse", "Direction of execution: reverse or forward")
	numWorkers := flag.Int("workers", 5, "Number of concurrent parallel scraper workers")
	limitStores := flag.Int("limit-stores", 200, "Automatically stop and save results after finding this many new verified stores (0 = no limit)")
	batchFlag := flag.Int("batch", 0, "Alias for limit-stores")
	headless := flag.Bool("headless", true, "Run Playwright browser in headless mode")
	minDelay := flag.Int("min-delay", 1000, "Minimum delay between requests in milliseconds")
	maxDelay := flag.Int("max-delay", 2500, "Maximum delay between requests in milliseconds")
	timeout := flag.Int("timeout", 35000, "Page navigation timeout in milliseconds")
	retries := flag.Int("retries", 3, "Maximum retry attempts per failed query")
	outputDir := flag.String("output-dir", "parallel_results", "Output directory for parallel worker results")
	logDir := flag.String("log-dir", "logs", "Directory to store log files")
	checkpointPath := flag.String("checkpoint", "parallel_progress.json", "Path to checkpoint JSON file")
	flag.Parse()

	// Initialize MySQL DB
	if err := database.InitDB(); err != nil {
		fmt.Printf("[WARNING] MySQL database initialization failed: %v\n", err)
		fmt.Println("[WARNING] Scraper will continue with CSV storage only.")
	}

	targetStores := *limitStores
	if *batchFlag > 0 {
		targetStores = *batchFlag
	}

	allQueries := query.GetAllQueries()
	totalQueries := len(allQueries)

	fmt.Println("================================================================================")
	fmt.Println("      🚀 ANTIGRAVITY PARALLEL ITERATIVE SCRAPING ENGINE (MULTI-WORKER) 🚀       ")
	fmt.Println("================================================================================")
	fmt.Printf("[SYSTEM] Total queries defined: %d\n", totalQueries)
	fmt.Printf("[PARAMS] Start: #%d | End: #%d | Direction: %s | Workers: %d\n", *startQuery, *endQuery, *direction, *numWorkers)
	if targetStores > 0 {
		fmt.Printf("[PARAMS] Stop Threshold: Automatically stop after %d new verified stores\n", targetStores)
	} else {
		fmt.Println("[PARAMS] Stop Threshold: Continuous until all queries are exhausted")
	}
	fmt.Printf("[PARAMS] Base Input: %s | Output Dir: %s | Checkpoint: %s\n", *inputCSV, *outputDir, *checkpointPath)
	fmt.Println("================================================================================")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Graceful Shutdown on CTRL+C / SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\n[SHUTDOWN] Interruption signal received. Gracefully finishing active jobs & saving checkpoint...")
		cancel()
	}()

	engineCfg := parallel.EngineConfig{
		StartID:        *startQuery,
		EndID:          *endQuery,
		Direction:      *direction,
		NumWorkers:     *numWorkers,
		Headless:       *headless,
		MinDelayMs:     *minDelay,
		MaxDelayMs:     *maxDelay,
		TimeoutMs:      *timeout,
		MaxRetries:     *retries,
		TargetStores:   targetStores,
		OutputDir:      *outputDir,
		LogDir:         *logDir,
		CheckpointPath: *checkpointPath,
		BaseCSVFile:    *inputCSV,
	}

	engine, err := parallel.NewEngine(engineCfg, allQueries)
	if err != nil {
		fmt.Printf("[FATAL ERROR] Engine initialization failed: %v\n", err)
		os.Exit(1)
	}

	if err := engine.Run(ctx); err != nil {
		fmt.Printf("[ERROR] Engine run terminated with error: %v\n", err)
		os.Exit(1)
	}
}
