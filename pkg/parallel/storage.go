package parallel

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"maps-scraper/pkg/database"
	"maps-scraper/pkg/model"
)

type StorageManager struct {
	mu               sync.RWMutex
	outputDir        string
	mainOutputFile   string
	seenPlaceIDs     map[string]bool
	seenNameAddrs    map[string]bool
	totalStoresSaved int
	targetStores     int

	writeChan chan *model.StoreRecord
	closeChan chan struct{}
	wg        sync.WaitGroup
}

func NewStorageManager(outputDir string, baseCSVFile string) (*StorageManager, error) {
	return NewStorageManagerWithTarget(outputDir, baseCSVFile, 0)
}

func NewStorageManagerWithTarget(outputDir string, baseCSVFile string, targetStores int) (*StorageManager, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output dir %s: %w", outputDir, err)
	}

	mainFile := baseCSVFile
	if mainFile == "" {
		mainFile = filepath.Join(outputDir, "parallel_stores_combined.csv")
	}

	sm := &StorageManager{
		outputDir:      outputDir,
		mainOutputFile: mainFile,
		seenPlaceIDs:   make(map[string]bool),
		seenNameAddrs:  make(map[string]bool),
		totalStoresSaved: 0,
		targetStores:   targetStores,
		writeChan:      make(chan *model.StoreRecord, 5000),
		closeChan:      make(chan struct{}),
	}

	if baseCSVFile != "" {
		_ = sm.loadBaseline(baseCSVFile)
	}

	if _, err := os.Stat(mainFile); err == nil {
		_ = sm.loadBaseline(mainFile)
	}

	sm.wg.Add(1)
	go sm.writerWorker()

	return sm, nil
}

func (sm *StorageManager) loadBaseline(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = ';'
	reader.LazyQuotes = true

	_, err = reader.Read()
	if err != nil {
		return err
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(row) < 3 {
			continue
		}

		placeID := strings.TrimSpace(row[0])
		name := strings.TrimSpace(row[1])
		addr := strings.TrimSpace(row[2])

		if placeID != "" {
			sm.seenPlaceIDs[placeID] = true
		}
		if name != "" && addr != "" {
			key := strings.ToLower(name) + "||" + strings.ToLower(addr)
			sm.seenNameAddrs[key] = true
		}
	}
	return nil
}

func (sm *StorageManager) CheckAndSaveRecord(workerID int, rec *model.StoreRecord) (bool, error) {
	sm.mu.Lock()

	// Enforce strict Target Store Count limit: do not accept any new store if target reached
	if sm.targetStores > 0 && sm.totalStoresSaved >= sm.targetStores {
		sm.mu.Unlock()
		return false, nil
	}

	// Deduplication
	if rec.PlaceID != "" && sm.seenPlaceIDs[rec.PlaceID] {
		sm.mu.Unlock()
		return false, nil
	}

	nameAddrKey := strings.ToLower(rec.Name) + "||" + strings.ToLower(rec.Address)
	if sm.seenNameAddrs[nameAddrKey] {
		sm.mu.Unlock()
		return false, nil
	}

	// Mark as seen
	if rec.PlaceID != "" {
		sm.seenPlaceIDs[rec.PlaceID] = true
	}
	sm.seenNameAddrs[nameAddrKey] = true
	sm.totalStoresSaved++
	
	// Assign WorkerID to the record so writerWorker can use it
	rec.WorkerID = workerID
	
	sm.mu.Unlock()

	// Send to async writer
	sm.writeChan <- rec

	return true, nil
}

// writerWorker handles all disk I/O synchronously to avoid locking worker threads
func (sm *StorageManager) writerWorker() {
	defer sm.wg.Done()

	// Main combined file setup
	fileExists := false
	if _, err := os.Stat(sm.mainOutputFile); err == nil {
		fileExists = true
	}
	mainFile, err := os.OpenFile(sm.mainOutputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Printf("Failed to open main output file: %v\n", err)
		return
	}
	defer mainFile.Close()

	mainWriter := csv.NewWriter(mainFile)
	mainWriter.Comma = ';'
	if !fileExists {
		_ = mainWriter.Write(model.Headers())
		mainWriter.Flush()
	}

	// Worker files cache
	workerFiles := make(map[int]*os.File)
	workerWriters := make(map[int]*csv.Writer)
	
	defer func() {
		mainWriter.Flush()
		for _, w := range workerWriters {
			w.Flush()
		}
		for _, f := range workerFiles {
			f.Close()
		}
	}()

	for {
		select {
		case rec := <-sm.writeChan:
			// Write to combined file
			_ = mainWriter.Write(rec.ToRow())
			
			// Write to MySQL DB safely
			if database.DB != nil {
				if err := database.SaveRecord(rec); err != nil {
					fmt.Printf("[DB ERROR] Failed to save %s: %v\n", rec.Name, err)
				}
			}

			// Write to worker specific file
			w, exists := workerWriters[rec.WorkerID]
			if !exists {
				workerFilePath := filepath.Join(sm.outputDir, fmt.Sprintf("worker_%02d.csv", rec.WorkerID))
				wFileExists := false
				if _, err := os.Stat(workerFilePath); err == nil {
					wFileExists = true
				}
				wf, err := os.OpenFile(workerFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err == nil {
					w = csv.NewWriter(wf)
					w.Comma = ';'
					if !wFileExists {
						_ = w.Write(model.Headers())
					}
					workerFiles[rec.WorkerID] = wf
					workerWriters[rec.WorkerID] = w
				}
			}
			if w != nil {
				_ = w.Write(rec.ToRow())
			}

			// Flush occasionally based on queue length to batch I/O
			if len(sm.writeChan) == 0 {
				mainWriter.Flush()
				if w != nil {
					w.Flush()
				}
			}
			
		case <-sm.closeChan:
			// Drain remaining
			for len(sm.writeChan) > 0 {
				rec := <-sm.writeChan
				_ = mainWriter.Write(rec.ToRow())
				
				if database.DB != nil {
					_ = database.SaveRecord(rec)
				}

				w, exists := workerWriters[rec.WorkerID]
				if exists {
					_ = w.Write(rec.ToRow())
				}
			}
			return
		}
	}
}

func (sm *StorageManager) Close() {
	close(sm.closeChan)
	sm.wg.Wait()
}

func (sm *StorageManager) GetTotalSaved() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.totalStoresSaved
}
