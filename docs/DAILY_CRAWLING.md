# Daily Crawling, Multi-Source & Export Architecture (Phase 1 & Phase 2)

This document provides complete technical, operational, and architectural documentation for the Crawling Run Session, Daily Scheduler, Multi-Source Crawling, and Granular Export system.

---

## 1. Architecture Overview (Before vs. After)

### Before Phase 1 & 2
- **Crawl Execution**: Crawls ran ephemerally in memory or appended loosely to output/articles.json. Single hardcoded source runner.
- **Session Tracking**: No persistent concept of a Run or Session. Crawl history was lost on server restart.
- **Article Relationships**: Articles in output/articles.json had no correlation to which crawling execution collected them.
- **Source Flexibility**: No multi-source abstraction. Attempting multi-source risked modifying or breaking the rock-solid Halodoc scraper.
- **Failure Handling**: An error or network timeout in one source could crash or halt the entire scrape job.
- **Exporting Capabilities**: All-or-nothing export. No ability to export articles by specific Crawling Run or by Day.

### After Phase 1 & 2
- **Persistent Crawl Runs**: Every crawl execution is assigned a unique RunID (format un_YYYYMMDD_HHMMSS_<8hex>) and an incremental human-readable RunNumber (Run #001, Run #002, etc.) stored permanently in data/runs.json.
- **Article-to-Run Correlation**: Every article scraped is explicitly mapped to its originating RunID in data/run_articles.json.
- **Strict Storage Schema Preservation**: The physical database file on disk (output/articles.json) strictly preserves the canonical 4-7 field contract (id, 	itle, image, category, description, source_url, scraped_at). No unauthorized fields leak to disk.
- **Multi-Source Isolation**:
  - pkg/source/halodoc/adapter.go: **100% untouched and protected**. Baseline Halodoc behavior is completely preserved.
  - pkg/source/detik/adapter.go: Independent adapter dedicated to Detik Health (https://health.detik.com), handling its unique pagination, layout, and image selectors.
- **Fault-Tolerant Crawling**: If one source encounters network or parsing failures, other sources continue uninterrupted. Failure telemetry is isolated to that source's runtime statistics without crashing the entire run.
- **Per-Source Runtime Statistics**: Each run tracks overall metrics (TotalCrawled, TotalSuccess, TotalFailed, TotalNew, TotalDuplicate) as well as breakdown per source in SourceStats (crawled, success, ailed, 
ew, duplicate, status, error).
- **Granular JSON Export**:
  - **Export All**: /api/download (downloads all canonical articles in database).
  - **Export Per Run**: /api/download?run_id={run_id_or_display_name} (exports only articles gathered during that specific run).
  - **Export Per Day**: /api/download?day={YYYY-MM-DD} (exports all articles collected across all runs executed on that date).
- **Daily Automated Scheduler**: Built-in CRON engine scheduled for **01:00 WIB daily** (DailyAtHour: 1, DailyAtMinute: 0), protected by mutex concurrency locking and idempotency guards.
- **Control Center UI**:
  - **Today's Crawling / Latest Run Card**: Displays the current day's crawl count, run status, new vs duplicate metrics, and duration.
  - **Scheduler Status & Manual Trigger**: Displays next scheduled execution and provides a " Run Now\ button to execute immediate on-demand runs tagged with trigger type cron.
 - **Timeline History with Per-Source Breakdown**: Displays visual badges for each source crawled (e.g. Halodoc: 10 ok, 0 fail, Detik Health: 5 ok, 0 fail).
 - **Per-Run and Per-Day Export UI**: Direct \Export JSON\ button on every timeline card, and an \Export by Day\ dropdown selector in the timeline header.

---

## 2. Multi-Source Architecture & Isolation

### Halodoc Protection
The Halodoc crawler (pkg/source/halodoc/adapter.go) was the proven baseline. To ensure zero regressions:
1. **Zero modifications** were made to pkg/source/halodoc/adapter.go.
2. Halodoc implements the standard source.Adapter interface (Name(), Search(), etc.).
3. All Halodoc unit tests (TestHalodoc_Search, TestHalodoc_MultiPageDiscovery, TestHalodoc_LoadMoreIncremental) run and pass regression-free.

### Detik Health Adapter (pkg/source/detik/adapter.go)
- **Target URL**: https://health.detik.com (and search endpoint https://health.detik.com/search/searchall?query=...).
- **Selectors**:
 - Articles: rticle.list-content__item, rticle, .media__text.
 - Titles & URLs: h3.media__title a, .media__link.
 - Images: img.media__image, img.
 - Descriptions: .media__desc, p.
- **Fault-tolerant parsing**: Missing categories default to \Health\; missing descriptions fallback gracefully; relative URLs are converted to absolute URLs.

### Independent Execution & Fault Tolerance
In pkg/api/crawler_handler.go, source adapters are executed sequentially with isolated ecover() and error tracking:
- Any unhandled panic or network timeout in Source A is trapped and set to ailed.
- Source B executes independently.
- Global Crawl Run finishes with status Completed (or Partial Failure if any source failed), accurately reflecting source telemetry.

---

## 3. Run ID & Data Relationship Specification

### Run ID Format
`
run_YYYYMMDD_HHMMSS_<8 random hex characters>
`
*Example:* un_20260907_153703_9bdabc25

### Display Identifier
User-friendly DisplayName (Run #001, Run #002, Run #004) calculated monotonically from total run count.

### Storage Separation
- **output/articles.json**: Strictly 4-7 canonical fields (id, itle, image, category, description, source_url, scraped_at).
- **data/run_articles.json**: Maps rticle_id -> run_id.
- **data/runs.json**: Stores persistent run entities, execution metrics, and source_stats.

---

## 4. REST API Reference

| Method | Endpoint | Description |
|---|---|---|
| GET | /api/runs | List all historical crawling runs ordered newest first |
| GET | /api/runs/today | List runs executed today along with latest_run summary |
| GET | /api/runs/days | List distinct calendar dates (YYYY-MM-DD) with run counts & article counts |
| GET | /api/runs/{id} | Get run details by unique Run ID or Display Name |
| GET | /api/download | Export all articles in database (canonical format) |
| GET | /api/download?run_id={id} | Export articles collected by a specific Run ID or Display Name (Run #001) |
| GET | /api/download?day={YYYY-MM-DD} | Export articles collected across all runs on that date |
| GET | /api/scheduler/status | Current scheduler status (enabled, is_running, 
ext_run_time) |
| POST | /api/scheduler/trigger | Manually trigger a scheduled crawl session |
| GET | /api/articles?session={name} | Filter articles by run display name (e.g. Run #001) |
| GET | /api/articles?run_id={id} | Filter articles by Run ID (e.g. un_20260907_152057_d0f08c8c) |

---

## 5. Granular JSON Export Formats

All download endpoints stream JSON conforming to the canonical 4-7 fields schema without leaking internal RunID or session fields.

### Sample Canonical JSON Article Structure
`json
[
 {
 id: e44d3202-e2d4-4a46-88c9-2f5a0e052ebf,
 title: Mengenal Gejala dan Pengobatan Penyakit Jantung,
 image: https://cdn.example.com/images/cardio.jpg,
 category: Jantung,
 description: Penyakit jantung adalah kondisi ketika jantung mengalami gangguan fungsi...,
 source_url: https://health.detik.com/berita-detikhealth/d-12345/gejala-jantung,
 scraped_at: 2026-09-07T16:20:00Z
 }
]
`

### Export Filename Conventions
- **All Articles**: rticles_all.json
- **Per Run**: rticles_run_{sanitized_run_id}.json (e.g., rticles_run_Run_004.json)
- **Per Day**: rticles_day_{YYYY-MM-DD}.json (e.g., rticles_day_2026-09-07.json)

---

## 6. Concurrency Locking & Idempotency

1. **Crawler Mutex**: MedicalCrawlerController.StartCrawl acquires ctrl.mu.Lock(). If an existing session is in status Running, new requests return HTTP 409 Conflict.
2. **Scheduler Execution Guard**: DailyScheduler checks atomic isJobActive flag. If a previous run is still active when the CRON trigger fires, the new execution is safely skipped and logged.
3. **Preload Seeding**: If output/articles.json exists upon system startup without existing run history, a baseline Run #001 is automatically seeded so existing data is immediately viewable and filterable in the UI.

---

## 7. Verification Matrix & Test Status

All automated tests across all packages pass 100%:
- pkg/api: TestAPI_RunsAndScheduler, TestAPI_RunDaysAndMultiGranularExport, TestUI_EndToEndLifecycle, TestAPI_Articles_DeleteAndSort
- pkg/source/detik: TestDetik_SearchIsolated, TestDetik_ParseHTML, TestDetik_HTTPErrorHandling
- pkg/source/halodoc: TestHalodoc_Search, TestHalodoc_MultiPageDiscovery, TestHalodoc_LoadMoreIncremental (100% untouched)
- pkg/source: TestRegistry_DetikRegistered, TestRegistry_HalodocProtected
- pkg/crawler: TestRunStorage_LifecycleAndOrdering, TestRunStorage_DateGrouping, TestStorage_PersistenceAcrossMultipleCrawls, TestCrawler_VerifyRealOutputFile
- pkg/scheduler: TestDailyScheduler_IdempotencyAndConcurrencyLock
