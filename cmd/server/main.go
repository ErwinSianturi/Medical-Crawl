package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"maps-scraper/pkg/api"
	"maps-scraper/pkg/database"
	"maps-scraper/pkg/job"
)

func main() {
	port := flag.Int("port", 8080, "HTTP Server Port")
	host := flag.String("host", "0.0.0.0", "HTTP Server Host Interface")
	dataDir := flag.String("data-dir", "data", "Data directory for persistent states & exports")
	logsDir := flag.String("log-dir", "logs", "Logs directory for worker & job logs")
	webDir := flag.String("web-dir", "web", "Directory containing built Web UI frontend assets")
	flag.Parse()

	if err := database.InitDB(); err != nil {
		log.Printf("[WARNING] Database initialization failed: %v", err)
	} else {
		log.Println("[DATABASE] Connection established")
	}

	log.Println("================================================================================")
	log.Println("       🚀 GOOGLE MAPS SCRAPING CONTROL CENTER — SERVER ENGINE 🚀       ")
	log.Println("================================================================================")
	log.Printf("[SERVER] Listening on http://%s:%d\n", *host, *port)
	log.Printf("[STORAGE] Data Directory: %s | Logs Directory: %s\n", *dataDir, *logsDir)
	log.Printf("[FRONTEND] Web Directory: %s\n", *webDir)
	log.Println("================================================================================")

	// Ensure directories exist
	_ = os.MkdirAll(*dataDir, 0755)
	_ = os.MkdirAll(*logsDir, 0755)
	_ = os.MkdirAll(*webDir, 0755)

	// Initialize Job Manager
	mgr, err := job.NewManager(*dataDir, *logsDir)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize Job Manager: %v", err)
	}

	// Initialize API Server
	server := api.NewServer(mgr)

	// Load Geographic Data
	api.LoadGeoData(filepath.Join(*dataDir, "indonesia_geo.json"))

	mux := http.NewServeMux()
	server.RegisterRoutes(mux, *webDir)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", *host, *port),
		Handler: mux,
	}

	// Graceful shutdown channel
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[FATAL] HTTP server error: %v", err)
		}
	}()

	log.Printf("[SUCCESS] Control Center online at http://localhost:%d\n", *port)

	<-stopChan
	log.Println("\n[SHUTDOWN] Shutting down Control Center server cleanly...")
	_ = httpServer.Close()
	log.Println("[SHUTDOWN] Server stopped successfully.")
}

func getAbsPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
