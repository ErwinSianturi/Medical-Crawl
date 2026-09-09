# Project Audit: Medical Article Crawler

Date: 2026-09-08
Version: 2.1.0

## Project Structure

maps-scraper/ (Medical Article Crawler)
├── cmd/
│   ├── crawler/              # Standalone CLI crawler runner
│   │   └── main.go
│   └── server/               # Full Control Center HTTP API & Web dashboard server
│       └── main.go
├── data/                     # Persistent runtime data (registry, runs, scheduler)
│   ├── run_articles.json
│   ├── runs.json
│   ├── scheduler_config.json
│   └── trusted_sources.json
├── docs/                     # Technical documentation & architecture guides
│   ├── ARCHITECTURE.md
│   ├── DAILY_CRAWLING.md
│   ├── DOCKER.md
│   ├── MIGRATION_STATE.md
│   └── TRUSTED_SOURCES.md
├── output/                   # Primary output directory for canonical articles.json
│   └── articles.json
├── pkg/                      # Core backend libraries and domain modules
│   ├── api/                  # HTTP REST API, SSE telemetry, static routing
│   │   ├── crawler_handler.go
│   │   ├── crawler_test.go
│   │   ├── handler.go
│   │   └── ui_e2e_test.go
│   ├── checkpoint/           # Generic task state checkpoints
│   │   ├── checkpoint.go
│   │   └── checkpoint_test.go
│   ├── classifier/           # Medical category classification engine
│   │   ├── classifier.go
│   │   └── classifier_test.go
│   ├── cleaner/              # Content sanitization, boilerplate & TOC strip
│   │   ├── cleaner.go
│   │   ├── cleaner_test.go
│   │   └── dedup.go
│   ├── crawler/              # Task queue, concurrency engine, JSON storage
│   │   ├── crawler_test.go
│   │   ├── engine.go
│   │   ├── job_manager.go
│   │   ├── processor.go
│   │   ├── run_storage.go
│   │   ├── run_storage_test.go
│   │   ├── source.go
│   │   ├── storage.go
│   │   ├── task.go
│   │   └── worker.go
│   ├── model/                # Canonical article & crawl run schemas
│   │   ├── article.go
│   │   ├── article_test.go
│   │   └── run.go
│   ├── scheduler/            # CRON-style daily scheduled crawler
│   │   ├── scheduler.go
│   │   └── scheduler_test.go
│   └── source/               # Trusted medical source adapters
│       ├── registry.go
│       ├── registry_test.go
│       ├── detik/
│       │   ├── adapter.go
│       │   └── adapter_test.go
│       ├── halodoc/
│       │   ├── adapter.go
│       │   └── adapter_test.go
│       ├── kemenkes/
│       │   ├── adapter.go
│       │   └── adapter_test.go
│       └── medlineplus/
│           ├── adapter.go
│           └── adapter_test.go
├── test/                     # End-to-end integration & multi-source tests
│   ├── cron_daily_crawling_e2e_test.go
│   ├── e2e_full_test.go
│   ├── gym_article_extraction_e2e_test.go
│   └── trusted_sources_e2e_test.go
├── web/                      # Production single-page application dashboard
│   └── index.html
├── .dockerignore             # Docker build exclusions
├── .env.example              # Environment configuration template
├── .gitignore                # Git exclusions
├── compose.yaml              # Docker Compose deployment definition
├── Dockerfile                # Multi-stage production container build
├── README.md                 # Primary developer and operational documentation
├── run_medical_crawler.bat   # Windows one-click runner
└── start.bat                 # Direct launch shortcut

## Main Components

1. Crawler Engine (pkg/crawler): Coordinates concurrency, rate limiting, and queueing. Atomic file write via SafeWriteFile with deduplication.
2. Medical Source Adapters (pkg/source): Halodoc, detikHealth, Kemenkes RI, and MedlinePlus. Complete DOM-based TOC removal and promotion stripping.
3. Content Cleaner & Classifier (pkg/cleaner, pkg/classifier): Strips ads, doctor consult widgets, and standardizes Indonesian medical categories.
4. Daily Scheduler (pkg/scheduler): CRON-style background crawler with configurable time and concurrency locks.
5. REST API & Telemetry (pkg/api): Handles HTTP endpoints, article export, and Server-Sent Events (/api/events).
6. Web Dashboard (web/index.html): Responsive single-page application dashboard.

## Entry Points

- Web UI Server: cmd/server/main.go (Runs HTTP server on port 8080 or PORT env var).
- CLI Crawler: cmd/crawler/main.go (Headless batch crawler).
- Launch Scripts: start.bat, run_medical_crawler.bat.

## Scraping System

- Discovery: Angular TransferState, CMS API, and HTML listing parser.
- Extraction: Title, direct image URL, category, clean paragraphs.
- Sanitization: Complete removal of 'DAFTAR ISI' and internal '#h-' links via DOM traversal.
- Deduplication: Unique MD5 IDs ensuring zero duplicate records.

## Database

- Primary JSON Store: output/articles.json (thread-safe, fsync atomic swap).
- Telemetry & Runs: data/runs.json, data/run_articles.json.
- Scheduler Config: data/scheduler_config.json.
- Sources Registry: data/trusted_sources.json.

## Scheduler / CRON

- Background goroutine ticking every 30 seconds.
- Single-flight mutual exclusion mutex preventing concurrent runs.
- Configurable time (e.g. 01:00 WIB, 08:30 WIB) stored persistently.

## Frontend

- web/index.html: Self-contained single-page dashboard with real-time SSE progress, source registry manager, category filtering, search, and JSON export.

## Backend

- cmd/server/main.go + pkg/api/crawler_handler.go + pkg/api/handler.go.
- Pure Go standard library (net/http). Zero external framework dependencies.

## Docker

- Multi-stage build in Dockerfile (golang:alpine -> alpine:3.21).
- Healthcheck targeting /api/system/status.
- compose.yaml mounting output, data, and gambar volumes.

## Tests

- 12 Unit test suites under pkg/
- 4 Integration/E2E test suites under test/
- 100% PASS rate across all suites.

## Scripts

- run_medical_crawler.bat (Windows one-click launcher)
- start.bat (Convenience wrapper)

## Files That Can Be Removed

- Scratch HTML dumps: article_sample.html, cari_kanker.html, faktor_kanker.html, halodoc_artikel.html
- Scratch JSON test request: test_detik_req.json
- Precompiled local binary: server.exe (11 MB)
- Stray empty/temporary artifacts: gambar/Nge-Gym Artinya, pkg/source/halodoc/gambar, test/gambar
- Local test run leftover dirs: pkg/api/data, pkg/api/output, test/data, test/output

## Files That Should Be Kept

- All source code in cmd/, pkg/, web/, docs/
- Database output/articles.json and config in data/
- Test suites in test/
- Dockerfile, compose.yaml, .dockerignore, .env.example, .gitignore
- README.md, start.bat, run_medical_crawler.bat

## Files That Need Refactoring

- cmd/server/main.go: Read PORT from environment variable if set.
- .gitignore: Clean up legacy Google Maps paths, ignore local binaries, debug html, and runtime images.
- .env.example: Fully document available configuration variables.
- docs/scraping.md: Document scraping pipeline, sources, and cleaner.
- docs/deployment.md: Comprehensive deployment guide for Docker & bare metal.

## Deployment Dependencies

- Go 1.22+ or Docker / Docker Compose.
- Outbound HTTPS (port 443) network access.
- Statically linked, pure Go, zero CGO dependencies.
