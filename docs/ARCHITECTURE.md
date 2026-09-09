# Architecture

This document tracks the technical architecture throughout the project migration from the original Google Maps building-store scraper to the Medical Article Crawler.

---

## 1. Target Conceptual Architecture (CONFIRMED & IMPLEMENTED IN STEP 3)

The generic crawler infrastructure is now implemented in `pkg/crawler/` following the target conceptual pipeline:

```text
+-----------------------------------------------------------------------+
|                              JOB MANAGER                              |
|  - pkg/crawler/job_manager.go: Lifecycle management (Queued, Running,  |
|    Paused, Stopped, Completed, Failed)                                |
+-----------------------------------------------------------------------+
                                  |
                                  v
+-----------------------------------------------------------------------+
|                            CRAWLER ENGINE                             |
|  - pkg/crawler/engine.go: Worker coordination, rate limiting/delays,   |
|    target articles threshold monitoring, real-time telemetry          |
+-----------------------------------------------------------------------+
                                  |
                                  v
+-----------------------------------------------------------------------+
|                              TASK QUEUE                               |
|  - pkg/crawler/task.go: Thread-safe FIFO task distribution channel     |
|    (Task: ID, URL, Type, Source, Metadata, Attempts, MaxRetries)     |
+-----------------------------------------------------------------------+
                                  |
                                  v
+-----------------------------------------------------------------------+
|                                WORKERS                                |
|  - pkg/crawler/worker.go: N concurrent worker goroutines consuming    |
|    tasks, executing HTTP fetch, parsing content, telemetry updates    |
+-----------------------------------------------------------------------+
                                  |
                                  v
+-----------------------------------------------------------------------+
|                            ARTICLE SOURCE                             |
|  - pkg/crawler/source.go: Generic Source, TaskDiscoverer,             |
|    HTTPFetcher, and ArticleExtractor interface abstractions           |
+-----------------------------------------------------------------------+
                                  |
                                  v
+-----------------------------------------------------------------------+
|                          ARTICLE PROCESSING                           |
|  - pkg/crawler/processor.go: Mandatory field validation, text         |
|    sanitization (HTML stripping), and Levenshtein title deduplication |
+-----------------------------------------------------------------------+
                                  |
                                  v
+-----------------------------------------------------------------------+
|                              JSON OUTPUT                              |
|  - pkg/crawler/storage.go: Asynchronous channel-based disk writer     |
|    producing clean JSON files with exactly 4 fields:                  |
|    { "title": "...", "category": "...", "image": "...", "description": "..." }
+-----------------------------------------------------------------------+
```

---

## 2. Infrastructure Refactoring Status

### REUSED (Preserved Stable Infrastructure)
- **Concurrency & Goroutine Pool Pattern**: Worker distribution model, rate-limiting delays (`MinDelay`, `MaxDelay`), and graceful context cancellation (`ctx.Done()`) preserved and adapted from original engine design.
- **Fuzzy String Deduplication**: Reused `pkg/preprocess/similarity.go` (`LevenshteinSimilarity`) within `ArticleProcessor` to identify and discard duplicate or republished article titles across crawled sources.
- **Atomic State Transitions**: Reused job state machine pattern (`QUEUED`, `RUNNING`, `PAUSED`, `STOPPED`, `COMPLETED`, `FAILED`) in `pkg/crawler/job_manager.go`.
- **Channel-based Async I/O**: Reused producer-consumer buffered write channel pattern (`writeChan`) to decouple worker crawling from disk operations.

### REFACTORED (Generalized & Decoupled)
- **`pkg/checkpoint/checkpoint.go`**:
  - Refactored to support generic item counts (`ItemsFound`, `ItemsDiscovered`) alongside legacy store fields.
  - Added generic method `MarkTaskSuccess(id, itemsFound)` so crawling tasks can track progress without store semantics.
- **Data Models (`pkg/model/`)**:
  - `model.Article` (`pkg/model/article.go`) confirmed as the primary data model.

### REPLACED (New Clean Implementation in Place)
- **Scraper Engine -> Crawler Engine**: Replaced Google Maps Playwright-bound engine with `pkg/crawler/engine.go` and `pkg/crawler/worker.go`, which operate on generic `Task` URLs via `HTTPFetcher`.
- **CSV Output -> JSON Storage**: Replaced 17-column semicolon CSV writer with `pkg/crawler/storage.go`, writing formatted `[]model.Article` JSON files.
- **Store Validation -> Article Processing**: Replaced cement store / subdistrict validators with `pkg/crawler/processor.go`, which enforces strict 4-field presence and sanitization.

### IMPLEMENTED (Medical Source Adapters)
- **`pkg/source/medlineplus/adapter.go`**:
  - Concrete implementation of Source #1 (NIH MedlinePlus).
  - Implements `crawler.Source`, `crawler.TaskDiscoverer`, `crawler.HTTPFetcher`, and `crawler.ArticleExtractor`.
  - Converts NLM Web Service XML search responses directly into canonical `model.Article` instances.
  - Sanitizes and unescapes text, extracts topic image / fallbacks, and assigns medical categorization.
  - Safely handles malformed XML, missing summaries/images, and HTTP errors.

### IMPLEMENTED (Medical Category Detection)
- **`pkg/classifier/classifier.go`**:
  - Source-independent, deterministic rule/keyword-based medical category detector.
  - Initial 10 standard categories: `Cancer`, `Diabetes`, `Heart Disease`, `Infectious Disease`, `Mental Health`, `Nutrition`, `Vaccination`, `Neurology`, `Pharmacy`, `General Health`.
  - Advanced text normalization and word-boundary regex matching (`\b...`) preventing false positive substrings (e.g. `flu` vs `fluid`, `hiv` vs `chive`, `pill` vs `spill`).
  - Proportional multi-word phrase bonuses (e.g. `flu vaccine` prioritizes `Vaccination` over generic `flu` `Infectious Disease`).
  - Weighted scoring: title matches scored at 3x, description matches scored at 1x; deterministic tie-breaking.
  - Sensible fallback to `General Health` when scores are zero or fields are missing.
  - Dynamically extensible via `AddRule(CategoryRule)`.
  - Integrated with `pkg/crawler/processor.go` (`ArticleProcessor.AutoClassify`) and `pkg/source/medlineplus/adapter.go`.

### IMPLEMENTED (Cleaning & Deduplication Engine)
- **`pkg/cleaner/cleaner.go`**:
  - Source-independent text sanitization and validation engine.
  - HTML stripping: scripts, styles, comments, tags, and multi-pass HTML entity decoding.
  - Encoding correction: unicode replacement character cleanup, zero-width space removal, smart quote/dash normalization, and punctuation spacing normalization.
  - Boilerplate removal: breadcrumbs (`Home > Topics > ...`), navigation directives (`Skip to content`, `Back to top`), advertisements (`Advertisement: ...`), and cookie disclaimers (`This website uses cookies...`, `Accept all cookies`).
  - Redundant content handling: collapses whole-block repeated halves and consecutive sentence repetitions.
  - URL validation: ensures image URLs are valid HTTP/HTTPS endpoints with fully qualified domains.
  - Strict validation: enforces non-empty titles, descriptions, categories, and images.
- **`pkg/cleaner/dedup.go`**:
  - Multi-tier in-memory deduplication:
    1. Normalized URL cache
    2. SHA-256 exact title hash
    3. SHA-256 exact content hash (Title + Description)
    4. Normalized alphanumeric title string comparison
    5. Fuzzy title similarity (Levenshtein distance $\ge$ threshold, default 0.85)
  - Critical invariant: Internal identifiers and hashes exist solely in memory and never appear in the final JSON schema.
- **`pkg/crawler/processor.go`**:
  - Delegates all cleaning and deduplication to `pkg/cleaner`, maintaining thread-safe processing and decoupling from crawler workers.

### IMPLEMENTED (JSON Output & Persistence Engine)
- **`pkg/crawler/storage.go`**:
  - Primary persistent destination: `output/articles.json`.
  - Format: human-readable UTF-8 JSON array with 2-space indentation and trailing newline.
  - Safe file writes (`SafeWriteFile`): writes to temp file with `.tmp` suffix, flushes/syncs, removes destination on Windows to avoid access collisions, and renames with fallback to direct write.
  - Empty result handling: empty array produces valid `[]\n`.
  - Deterministic ordering: titles sorted alphabetically to produce clean, reproducible diffs.
  - Deduplication safety: `appendUniqueLocked` prevents duplicate titles from being persisted across runs.
  - Programmatic verification helper: `VerifyArticlesJSON(path string)` validates strict 4-field compliance and rejects any unauthorized keys.
- **`cmd/crawler/main.go`**:
  - Dedicated CLI runner for crawling trusted medical sources (`-topics`, `-output`, `-limit`, `-workers`).
  - Automatically executes programmatic schema verification upon crawl completion.

### DEPRECATED / MARKED FOR REMOVAL (Old Scraper Output)
- **`pkg/writer/` (17-Column Store CSV Writer)**: Marked for removal. Completely replaced by `output/articles.json`.
- **`pkg/database/` (MySQL Store Database)**: Marked for removal. Relational store tables and queries are obsolete in the new JSON-centric architecture.
- **`cmd/parallel_iterative_scraper/`**: Marked for replacement by `cmd/crawler/`.
- *Note*: Per migration guidelines, legacy code is preserved until explicit cleanup instruction is given.

### STILL PENDING (To Be Addressed in Subsequent Steps)
- **Decommissioning Legacy Code**: Deleting obsolete store scraping code (`pkg/scraper/`, `pkg/database/`, `pkg/model/geo.go`, `provinces.json`, etc.) once the new crawler is fully operational.

---

## 3. Decoupling Verification Matrix

| Concept | Status in `pkg/crawler/` | Explanation |
|---|---|---|
| **Store / StoreRecord** | **REMOVED** | Replaced entirely by `model.Article`. No store struct imports exist in `pkg/crawler/`. |
| **Business / Cement** | **REMOVED** | Zero keywords or category checks for cement or hardware products. |
| **Google Maps / Playwright** | **REMOVED** | Uses generic `HTTPFetcher` and `ArticleExtractor` interfaces; no Playwright or Chromium dependencies. |
| **Province / City / Kecamatan** | **REMOVED** | Geographic validation logic eliminated from crawler components. |
| **Place ID** | **REMOVED** | Tasks use generic string `Task.ID` and `Task.URL`; no Google Place ID requirements. |
| **MySQL Database** | **REMOVED** | Outputs clean JSON files directly via `pkg/crawler/storage.go`. |

---

## 4. Canonical Article Schema & JSON Output

Target schema confirmed and verified in `pkg/model/article.go` and `pkg/crawler/crawler_test.go`:

```json
{
  "title": "...",
  "category": "...",
  "image": "...",
  "description": "..."
}
```

---

## 5. Known Limitations

1. **Boilerplate Lexicon**: Pattern-based boilerplate removal targets standard English patterns; heavily obfuscated inline native advertisements or non-English consent banners will require dictionary extension.
2. **Fuzzy Dedup Complexity**: In-memory Levenshtein comparison runs in $O(N)$ against previously indexed normalized titles. Highly scalable for typical crawls ($<50,000$ articles); for ultra-large crawls ($>100,000$), an inverted token index or MinHash/SimHash index should replace linear comparison.
3. **Punctuation Stripping in Titles**: Hyphenated medical terms (e.g. `COVID-19`) are normalized to spaces during duplicate title comparison, which treats `Covid 19` and `Covid-19` as duplicates (intended behavior).

---

## 6. Change Log

| Phase | Date | Status | Changes |
|---|---|---|---|
| **STEP 0** | 2026-09-04 | Completed | Initial architecture tracking created in `docs/`. |
| **STEP 1** | 2026-09-04 | Completed | Full codebase analysis of 42 Go files; current execution flow mapped; component classification established. |
| **STEP 2** | 2026-09-04 | Completed | Canonical `model.Article` implemented in `pkg/model/article.go` with 4 strict JSON fields (`title`, `category`, `image`, `description`). Unit tests added. |
| **STEP 3** | 2026-09-04 | Completed | Generic crawler infrastructure separated into `pkg/crawler/`: Task Queue, Workers, Engine, Processor, JSON Storage, and Job Manager. Generalized checkpointing in `pkg/checkpoint/checkpoint.go`. Full test suite added in `pkg/crawler/crawler_test.go`. Zero coupling to building-store logic. |
| **STEP 4** | 2026-09-04 | Completed | Registered and analyzed Source #1 NIH MedlinePlus (`https://medlineplus.gov`). Documented NLM XML query endpoint, pagination, metadata, and 4-field extraction mapping in `docs/TRUSTED_SOURCES.md`. |
| **STEP 5** | 2026-09-04 | Completed | Implemented MedlinePlus source adapter in `pkg/source/medlineplus/adapter.go` and unit/integration test suite in `adapter_test.go`. Verified search, XML parsing, HTML unescaping/stripping, 4-field output compliance, and error resiliency on topics: `diabetes`, `cancer`, `vaccination`. |
| **STEP 6** | 2026-09-04 | Completed | Implemented deterministic, rule-based medical category detector in `pkg/classifier/classifier.go` and test suite `classifier_test.go`. Configured 10 standard categories (`Cancer`, `Diabetes`, `Heart Disease`, `Infectious Disease`, `Mental Health`, `Nutrition`, `Vaccination`, `Neurology`, `Pharmacy`, `General Health`). Integrated with `pkg/crawler/processor.go` and `pkg/source/medlineplus/adapter.go`. Verified multi-topic classification, case-insensitivity, word boundaries, and fallback handling. |
| **STEP 7** | 2026-09-04 | Completed | Implemented source-independent article cleaner and deduplicator in `pkg/cleaner/`. Added HTML stripping, entity decoding, unicode/smart quote normalization, boilerplate/ad/cookie stripping, duplicated sentence collapse, image URL validation, and multi-tier SHA-256/Levenshtein deduplication with zero hash leakage into JSON output. All tests passing. |
| **STEP 8** | 2026-09-04 | Completed | Implemented primary output persistence in `output/articles.json` via `pkg/crawler/storage.go` with atomic-safe writes, deterministic title sorting, and empty result support. Created CLI runner `cmd/crawler/main.go`. Ran live crawl and programmatically verified that all items in `output/articles.json` contain strictly 4 canonical fields (`title`, `category`, `image`, `description`) with zero leaked metadata. Marked legacy CSV and MySQL persistence as deprecated for removal. |
| **STEP 9** | 2026-09-04 | Completed | Implemented Trusted Medical Source #2 (Kementerian Kesehatan RI / Ayo Sehat / Sehat Negeriku) in `pkg/source/kemenkes/adapter.go`. Added directory indexing, HTML extraction, soft 404 detection, safe image/description fallbacks, bilingual classification (`pkg/classifier/`), cross-source deduplication tests (`pkg/source/kemenkes/adapter_test.go`), and multi-source concurrent crawling in `cmd/crawler/main.go`. Live crawl generated 20 articles in `output/articles.json` combining both sources with strict 4-field schema validation. |
| **STEP 10** | 2026-09-04 | Completed | Converted UI (`web/index.html`) into dedicated Medical Article Crawler interface. Removed all Google Maps, Leaflet, store categories, cement, and geographic location hierarchy elements. Implemented topic search, approved source selector, article limit, start/stop crawl triggers, accurate state transitions (`Waiting`, `Running`, `Completed`, `Failed`), clean 4-field article cards, and direct `articles.json` download. End-to-end user lifecycle verified via `pkg/api/ui_e2e_test.go`. |
| **STEP 11** | 2026-09-04 | Completed | Executed full End-to-End test suite (`test/e2e_full_test.go`) covering 6 mandated medical topics (`diabetes`, `cancer`, `vaccination`, `heart disease`, `mental health`, `nutrition`), backend pipeline, 10 edge case failure modes, complete UI user flow, and strict 4-field JSON validation. 100% test pass rate achieved across all suites. |
| **STEP 12** | 2026-09-04 | Completed | Decommissioned and purged all legacy building-material scraping components (`pkg/job`, `pkg/database`, `pkg/scraper`, `pkg/writer`, `pkg/validator`, `pkg/query`, `pkg/parallel`, `pkg/normalizer`, `pkg/preprocess`, `pkg/model/store.go`, `pkg/model/geo.go`, `cmd/parallel_iterative_scraper`). Inlined Levenshtein distance into `pkg/cleaner/dedup.go`. Tidied `go.mod` to pure Go standard library with zero third-party dependencies. Re-verified 100% build and test pass rate. |







