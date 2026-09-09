package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Server represents the HTTP API and frontend server for the Medical Article Crawler.
type Server struct {
	controller *MedicalCrawlerController
	clients    map[chan string]bool
	clientsMu  sync.RWMutex
}

// NewServer creates an initialized Server instance.
func NewServer() *Server {
	s := &Server{
		controller: GetMedicalCrawlerController(),
		clients:    make(map[chan string]bool),
	}
	return s
}

// Broadcast sends a server-sent event or message to all connected clients.
func (s *Server) Broadcast(msg string) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	for ch := range s.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// RegisterRoutes registers all Medical Article Crawler endpoints and static frontend assets.
func (s *Server) RegisterRoutes(mux *http.ServeMux, webDir string) {
	// Medical Article Crawler API
	mux.HandleFunc("/api/sources", s.handleSources)
	mux.HandleFunc("/api/crawl", s.handleCrawl)
	mux.HandleFunc("/api/crawl/status", s.handleCrawlStatus)
	mux.HandleFunc("/api/crawl/sessions", s.handleCrawlSessions)
	mux.HandleFunc("/api/crawl/stop", s.handleCrawlStop)

	// Crawling Runs & Timeline History
	mux.HandleFunc("/api/runs", s.handleRuns)
	mux.HandleFunc("/api/runs/today", s.handleRunsToday)
	mux.HandleFunc("/api/runs/days", s.handleRunDays)
	mux.HandleFunc("/api/runs/", s.handleRunDetail)

	// Scheduler / Daily CRON
	mux.HandleFunc("/api/scheduler/status", s.handleSchedulerStatus)
	mux.HandleFunc("/api/scheduler/trigger", s.handleSchedulerTrigger)
	mux.HandleFunc("/api/scheduler/config", s.handleSchedulerConfig)

	mux.HandleFunc("/api/articles", s.handleArticles)
	mux.HandleFunc("/api/articles/", s.handleArticles)
	mux.HandleFunc("/api/download", s.handleDownloadArticles)
	mux.HandleFunc("/api/export/articles.json", s.handleDownloadArticles)

	// Server-Sent Events (Telemetry Stream)
	mux.HandleFunc("/api/events", s.handleSSE)

	// Health Check / System Status
	mux.HandleFunc("/api/system/status", s.handleSystemStatus)

	// Serve downloaded local article images
	_ = os.MkdirAll("gambar", 0755)
	mux.HandleFunc("/gambar/", s.handleGambar)

	// Serve Static Frontend Assets
	if webDir != "" {
		fileServer := http.FileServer(http.Dir(webDir))
		mux.Handle("/", fileServer)
	}
}

func (s *Server) handleGambar(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/gambar/")
	fullPath := filepath.Join("gambar", relPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		// Fallback to sanitized filename on Windows NTFS
		sanitized := regexp.MustCompile(`[\\/:*?"<>|]`).ReplaceAllString(relPath, "-")
		data, err = os.ReadFile(filepath.Join("gambar", sanitized))
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write(data)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	clientChan := make(chan string, 32)
	s.clientsMu.Lock()
	s.clients[clientChan] = true
	s.clientsMu.Unlock()

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, clientChan)
		s.clientsMu.Unlock()
		close(clientChan)
	}()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg, ok := <-clientChan:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ctrl := GetMedicalCrawlerController()
	state := ctrl.GetStatus()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"service":   "Medical Article Crawler",
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"crawler":   state.Status,
		"collected": state.Collected,
	})
}
