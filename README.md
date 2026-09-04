# Medical Crawl

## Project Overview

**Medical Crawl** is a large-scale, iterative Google Maps scraping system (Control Center) designed to search and extract data on construction material stores across Indonesia. The system automatically crawls based on regional parameters (Province, City, District) and user-defined keywords. Collected data includes store name, address, city, district, coordinates, phone number, rating, opening hours, and business category.

## Main Technologies

| Component         | Technology                                    |
|--------------------|----------------------------------------------|
| **Frontend**       | Vanilla HTML, CSS (Tailwind CSS via CDN), JS |
| **Backend**        | Go (Golang) REST API                         |
| **Scraping Engine**| Playwright (Go) for browser automation       |
| **Database**       | MySQL                                        |
| **Export Format**   | CSV                                         |

## Architecture

```text
Frontend (Web UI)
   ↓
Backend API (Go)
   ↓
Job Manager
   ↓
Iteration Engine
   ↓
Query Pool (Generated Queries)
   ↓
5 Parallel Workers (Goroutines)
   ↓
Playwright Scraper
   ↓
Storage Manager (In-memory Deduplication)
   ↓
MySQL Database (Persistence)
   ↓
CSV Export
```

## Project Structure

```text
Medical Crawl/
├── cmd/                          # Application entry points
│   ├── server/                   #   Web server (API + frontend host)
│   └── parallel_iterative_scraper/ # CLI parallel scraper
├── pkg/                          # Core application logic
│   ├── api/                      #   REST API handlers & SSE
│   ├── checkpoint/               #   Job state checkpoint management
│   ├── database/                 #   MySQL database layer
│   ├── job/                      #   Job lifecycle manager
│   ├── model/                    #   Data models & structs
│   ├── normalizer/               #   Address normalization & validation
│   ├── parallel/                 #   Multi-worker parallel engine
│   ├── preprocess/               #   Data preprocessing utilities
│   ├── query/                    #   Query generation & management
│   ├── scraper/                  #   Playwright-based scraper core
│   ├── validator/                #   Location & data validator
│   └── writer/                   #   CSV/file output writer
├── web/                          # Frontend UI (HTML, CSS, JS)
├── ui_tests/                     # End-to-end automated tests
├── data/                         # Runtime: state, checkpoints, exports
├── logs/                         # Runtime: worker & scraper logs
├── parallel_results/             # Runtime: CLI scraper output
├── provinces.json                # Indonesian province reference data
├── regencies.json                # Indonesian regency reference data
├── districts.json                # Indonesian district reference data
├── .env.example                  # Environment variable template
├── go.mod                        # Go module definition
├── go.sum                        # Go dependency checksums
├── run_control_center.bat        # Windows launcher for web server
└── run_parallel_iterative.bat    # Windows launcher for CLI scraper
```

## Prerequisites

- **Go** 1.26+ installed
- **MySQL** server running (default port 3306)
- **Playwright** browser dependencies installed

## Installation

1. **Clone the repository:**
   ```bash
   git clone https://github.com/ErwinSianturi/Medical-Crawl.git
   cd Medical-Crawl
   ```

2. **Install Go dependencies:**
   ```bash
   go mod download
   ```

3. **Install Playwright browsers** (required for scraping):
   ```bash
   npx playwright install chromium
   ```

4. **Setup MySQL database:**
   The application will automatically create the database and required tables on first run if the configured MySQL user has `CREATE DATABASE` privileges.

## Configuration

Copy the example environment file and configure it:

```bash
cp .env.example .env
```

Edit `.env` with your database credentials:

```env
DB_HOST=127.0.0.1
DB_PORT=3306
DB_USER=root
DB_PASSWORD=your_password_here
DB_NAME=db_3rdtryscraping
```

> **⚠️ Important:** Never commit the `.env` file. It is already listed in `.gitignore`.

## Running the Project

### Option 1: Web Server (Control Center)

Run the backend server which hosts both the API and frontend UI:

```bash
go run ./cmd/server --port 8080 --data-dir data --web-dir web
```

Or on Windows, use the provided batch file:

```bash
run_control_center.bat
```

Then open `http://localhost:8080` in your browser.

### Option 2: CLI Parallel Scraper

For headless/CLI-based parallel scraping:

```bash
go run ./cmd/parallel_iterative_scraper
```

Or on Windows:

```bash
run_parallel_iterative.bat
```

## Building

Compile the backend server binary:

```bash
go build -o server.exe ./cmd/server
```

Compile the parallel scraper binary:

```bash
go build -o parallel_scraper.exe ./cmd/parallel_iterative_scraper
```

## How It Works

1. User opens the Dashboard UI and selects target regions (Province → City → District).
2. The system generates hundreds of specific search queries from keywords × locations.
3. 5 parallel workers (goroutines) execute queries using Playwright headless browser.
4. Scraped data goes through normalization and deduplication.
5. Valid records are stored in MySQL and exported to CSV.
6. Real-time progress is streamed to the UI via Server-Sent Events (SSE).
7. Jobs can be paused, resumed, and downloaded at any time.

## Development Notes

- **Runtime directories** (`data/`, `logs/`, `parallel_results/`, `Hasil/`) are created automatically and excluded from version control.
- **Workers** default to 5 parallel goroutines. Each worker writes to its own CSV file to avoid race conditions.
- **Database credentials** must be configured via environment variables (see `.env.example`).
- **Tests** are located in `*_test.go` files within each package and in the `ui_tests/` directory.
- **No secrets or credentials** should ever be hardcoded in source code.

## Running Tests

```bash
# Run all Go unit tests
go test ./...

# Run UI end-to-end tests (requires Node.js)
cd ui_tests
npm install
npm test
```

## License

This project is proprietary. All rights reserved.
