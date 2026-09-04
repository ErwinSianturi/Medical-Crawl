@echo off
REM ============================================================================
REM   SCRAPING CONTROL CENTER — AUTOMATED LAUNCHER
REM   Starts Go Backend Server and Opens Control Center Web UI in Browser
REM ============================================================================

title Scraping Control Center — Web UI and API Server

echo ================================================================================
echo         🚀 LAUNCHING SCRAPING CONTROL CENTER (MULTI-REGION ENGINE) 🚀       
echo ================================================================================

echo [1/2] Starting Go Server Backend on http://localhost:8080...
IF EXIST server.exe (
    start "Scraping Control Center Server" server.exe --port 8080 --data-dir data --web-dir web
) ELSE (
    start "Scraping Control Center Server" go run ./cmd/server --port 8080 --data-dir data --web-dir web
)

echo [2/2] Waiting for server initialization...
ping 127.0.0.1 -n 3 >nul

echo [LAUNCH] Opening Control Center UI in browser...
start http://localhost:8080/?v=2.1

echo.
echo ================================================================================
echo [SUCCESS] Control Center is now running!
echo - Web UI URL: http://localhost:8080
echo - Server Window: Keep the server console window open while scraping.
echo ================================================================================
echo.
pause
