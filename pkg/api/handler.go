package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"maps-scraper/pkg/job"
	"maps-scraper/pkg/model"
)

type Server struct {
	manager    *job.Manager
	clients    map[chan string]bool
	clientsMu  sync.RWMutex
	httpServer *http.Server
}

func NewServer(manager *job.Manager) *Server {
	s := &Server{
		manager: manager,
		clients: make(map[chan string]bool),
	}

	// Register job manager broadcast callbacks
	manager.SetCallbacks(
		func(jobID string, entry model.LogEntry) {
			data, _ := json.Marshal(map[string]interface{}{
				"type":   "log",
				"job_id": jobID,
				"data":   entry,
			})
			s.Broadcast(string(data))
		},
		func(jobID string, record model.StoreRecord) {
			data, _ := json.Marshal(map[string]interface{}{
				"type":   "record",
				"job_id": jobID,
				"data":   record,
			})
			s.Broadcast(string(data))
		},
		func(jobID string, state model.JobState) {
			data, _ := json.Marshal(map[string]interface{}{
				"type":   "state",
				"job_id": jobID,
				"data":   state,
			})
			s.Broadcast(string(data))
		},
	)

	return s
}

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

func (s *Server) RegisterRoutes(mux *http.ServeMux, webDir string) {
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/jobs/", s.handleJobDetail)
	mux.HandleFunc("/api/presets", s.handlePresets)
	mux.HandleFunc("/api/system/metrics", s.handleMetrics)
	mux.HandleFunc("/api/system/wipe", s.handleWipeData)
	mux.HandleFunc("/api/events", s.handleSSE)
	mux.HandleFunc("/api/export/all", s.handleExportAll)
	mux.HandleFunc("/api/locations/provinces", s.handleProvinces)
	mux.HandleFunc("/api/locations/cities", s.handleCities)
	mux.HandleFunc("/api/locations/districts", s.handleDistricts)

	// Serve Static Frontend UI files if webDir is provided
	if webDir != "" {
		fileServer := http.FileServer(http.Dir(webDir))
		mux.Handle("/", fileServer)
	}
}

func (s *Server) handleExportAll(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	validOnly := r.URL.Query().Get("valid_only") == "true"

	content, filename, mimeType, err := s.manager.ExportAllData(format, validOnly)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}


func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == http.MethodGet {
		jobs := s.manager.GetAllJobs()
		_ = json.NewEncoder(w).Encode(jobs)
		return
	}

	if r.Method == http.MethodPost {
		var cfg model.JobConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON payload: %v", err), http.StatusBadRequest)
			return
		}

		if cfg.Settings.ValidateLocation {
			if err := ValidateLocationHierarchy(cfg.Location.Province, cfg.Location.City, cfg.Location.District); err != nil {
				http.Error(w, fmt.Sprintf("location validation failed: %v", err), http.StatusBadRequest)
				return
			}
		}

		state, err := s.manager.CreateJob(cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(state)
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleJobDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	trimmedPath := strings.Trim(path, "/")
	if trimmedPath == "" {
		http.Error(w, "job ID required", http.StatusBadRequest)
		return
	}

	parts := strings.Split(trimmedPath, "/")
	jobID := parts[0]

	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			state, exists := s.manager.GetJob(jobID)
			if !exists {
				http.Error(w, "job not found", http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(state)
			return
		}

		if r.Method == http.MethodDelete {
			if err := s.manager.DeleteJob(jobID); err != nil {
				if strings.Contains(err.Error(), "job not found") {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": jobID})
			return
		}
	}

	if len(parts) >= 2 {
		action := parts[1]

		switch action {
		case "start":
			if err := s.manager.StartJob(jobID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "started", "id": jobID})

		case "pause":
			if err := s.manager.PauseJob(jobID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "paused", "id": jobID})

		case "resume":
			if err := s.manager.ResumeJob(jobID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "resumed", "id": jobID})

		case "stop":
			if err := s.manager.StopJob(jobID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped", "id": jobID})

		case "status":
			state, exists := s.manager.GetJob(jobID)
			if !exists {
				http.Error(w, "job not found", http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(state)

		case "workers":
			state, exists := s.manager.GetJob(jobID)
			if !exists {
				http.Error(w, "job not found", http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(state.Workers)

		case "records":
			pageStr := r.URL.Query().Get("page")
			limitStr := r.URL.Query().Get("limit")
			search := r.URL.Query().Get("search")
			workerStr := r.URL.Query().Get("worker")

			page := 1
			limit := 50
			worker := 0

			if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
				page = p
			}
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
			if wId, err := strconv.Atoi(workerStr); err == nil && wId > 0 {
				worker = wId
			}

			resp := s.manager.GetPaginatedRecords(jobID, page, limit, search, worker)
			_ = json.NewEncoder(w).Encode(resp)


		case "logs":
			state, exists := s.manager.GetJob(jobID)
			if !exists {
				http.Error(w, "job not found", http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(state.Logs)

		case "export":
			format := r.URL.Query().Get("format")
			if format == "" {
				format = "csv"
			}
			validOnly := r.URL.Query().Get("valid_only") == "true"

			content, filename, mimeType, err := s.manager.ExportData(jobID, format, validOnly)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", mimeType)
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)

		case "iterations":
			if len(parts) >= 3 {
				iterAction := parts[2]
				if iterAction == "start" {
					var req model.IterationState
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					iter, err := s.manager.StartIteration(jobID, req)
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					_ = json.NewEncoder(w).Encode(iter)
					return
				}

				if len(parts) >= 4 {
					iterID := parts[2]
					iterAction = parts[3]
					
					switch iterAction {
					case "pause":
						if err := s.manager.PauseIteration(jobID, iterID); err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						_ = json.NewEncoder(w).Encode(map[string]string{"status": "paused", "id": iterID})
					case "resume":
						if err := s.manager.ResumeIteration(jobID, iterID); err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						_ = json.NewEncoder(w).Encode(map[string]string{"status": "resumed", "id": iterID})
					case "stop":
						if err := s.manager.StopIteration(jobID, iterID); err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped", "id": iterID})
					case "retry":
						if err := s.manager.RetryIteration(jobID, iterID); err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						_ = json.NewEncoder(w).Encode(map[string]string{"status": "retrying", "id": iterID})
					default:
						http.Error(w, "unknown iteration action", http.StatusNotFound)
					}
					return
				}
			}
			http.Error(w, "invalid iterations path", http.StatusBadRequest)

		default:
			http.Error(w, "unknown action", http.StatusNotFound)
		}
		return
	}
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodGet {
		presets := s.manager.GetPresets()
		_ = json.NewEncoder(w).Encode(presets)
		return
	}

	if r.Method == http.MethodPost {
		var p model.PresetConfig
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.manager.SavePreset(p)
		_ = json.NewEncoder(w).Encode(p)
		return
	}
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	metrics := s.manager.GetSystemMetrics()
	_ = json.NewEncoder(w).Encode(metrics)
}

func (s *Server) handleWipeData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == http.MethodPost || r.Method == http.MethodDelete {
		if err := s.manager.WipeAllData(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "All scraping records, CSV exports, and job history wiped successfully.",
		})
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}


func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	ch := make(chan string, 100)
	s.clientsMu.Lock()
	s.clients[ch] = true
	s.clientsMu.Unlock()

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, ch)
		s.clientsMu.Unlock()
		close(ch)
	}()

	// Send initial connection heartbeat
	_, _ = fmt.Fprintf(w, "data: {\"type\":\"ping\"}\n\n")
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"ping\"}\n\n")
			flusher.Flush()
		}
	}
}
