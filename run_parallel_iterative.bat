@echo off
REM ============================================================================
REM   BACKGROUND RUNNER FOR ANTIGRAVITY PARALLEL ITERATIVE SCRAPER
REM   Mulai dari query 2865 ke belakang (reverse order) dengan Multi-Workers
REM   Otomatis berhenti & simpan hasil setelah menemukan 200 toko baru
REM ============================================================================

title Parallel Iterative Scraper Engine (Background)

if not exist parallel_iterative_scraper.exe (
    echo [BUILD] Compiling parallel_iterative_scraper.exe...
    go build -o parallel_iterative_scraper.exe ./cmd/parallel_iterative_scraper
    if errorlevel 1 (
        echo [ERROR] Build failed. Please check Go compiler.
        pause
        exit /b 1
    )
)

echo [START] Launching Parallel Iterative Scraper (Workers: 5 | Target: 200 Stores | Start: 2865 | Direction: reverse)...
start "" parallel_iterative_scraper.exe --input "Scrape_Iterative.csv" --start 2865 --end 1 --direction reverse --workers 5 --limit-stores 200 --headless=true --min-delay 1000 --max-delay 2500 --output-dir "parallel_results" --checkpoint "parallel_progress.json" --log-dir "logs"

echo [SUCCESS] Parallel Scraper is now running in the background!
echo - Target: Stop & Save automatically after 200 new verified stores
echo - Progress Checkpoint: parallel_progress.json
echo - Combined Output: parallel_results/parallel_stores_combined.csv
echo - Combined Log: logs/parallel_iterative.log
echo.
pause
