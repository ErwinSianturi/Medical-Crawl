package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"maps-scraper/pkg/api"
)

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func main() {
	defaultPort := getEnvInt("PORT", 8080)
	defaultHost := getEnv("HOST", "0.0.0.0")
	defaultWebDir := getEnv("WEB_DIR", "web")
	defaultOutputDir := getEnv("OUTPUT_DIR", "output")

	port := flag.Int("port", defaultPort, "HTTP Server Port")
	host := flag.String("host", defaultHost, "HTTP Server Host Interface")
	webDir := flag.String("web-dir", defaultWebDir, "Directory containing built Web UI frontend assets")
	outputDir := flag.String("output-dir", defaultOutputDir, "Directory for crawled articles JSON output")
	flag.Parse()

	log.Println("================================================================================")
	log.Println("           🏥 MEDICAL ARTICLE CRAWLER — CONTROL CENTER SERVER 🏥                ")
	log.Println("================================================================================")
	log.Printf("[SERVER] Listening on http://%s:%d\n", *host, *port)
	log.Printf("[FRONTEND] Web Directory: %s\n", *webDir)
	log.Printf("[STORAGE] Output Directory: %s\n", *outputDir)
	log.Println("================================================================================")

	// Ensure runtime directories exist
	_ = os.MkdirAll(*webDir, 0755)
	_ = os.MkdirAll(*outputDir, 0755)

	// Initialize API Server
	server := api.NewServer()

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

	log.Printf("[SUCCESS] Medical Article Crawler online at http://localhost:%d\n", *port)

	<-stopChan
	log.Println("\n[SHUTDOWN] Shutting down Control Center server cleanly...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("[SHUTDOWN] HTTP server shutdown error: %v", err)
	} else {
		log.Println("[SHUTDOWN] HTTP server stopped gracefully.")
	}
	log.Println("[SHUTDOWN] Server stopped successfully.")
}
