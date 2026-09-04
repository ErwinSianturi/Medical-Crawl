# SCRAPER PHASE ROLLOUT MASTER GUIDE

## 1. Project Overview

This document is the **official execution guide** for fixing, optimizing, and productionizing the Go-based Scraper Project. It defines a strict, phased approach to address known critical data integrity issues, concurrency bottlenecks, and state management flaws without causing further regressions. 

All future AI agents or developers working on this project **MUST** follow this guide phase by phase, ensuring that all acceptance criteria are met and checkpoints are created before proceeding.

## 2. Current Architecture

Based on the project inspection, the architecture consists of:
- **Frontend**: Served from the `web/` directory (`index.html`).
- **Backend/API**: Driven by Go server (`cmd/server/main.go`, `pkg/api/handler.go`). Server Sent Events (SSE) and HTTP REST APIs are used for UI communication.
- **Job Manager**: Manages concurrent scraping jobs (`pkg/job/manager.go`).
- **Parallel Scraper Engine**: A multi-worker architecture (`pkg/parallel/engine.go`, `pkg/parallel/storage.go`, `pkg/parallel/worker.go`). Defaults to 5 workers.
- **Normalizer & Validator**: Validates addresses and determines location relevance (`pkg/normalizer/clean_address.go`, `pkg/normalizer/address.go`, `pkg/validator/validator.go`).
- **Storage/CSV**: Records are flushed to worker-specific CSVs and a global combined CSV, and state history is saved to JSON.

## 3. Audit Findings

Inspection of the source code confirms the following critical issues:

**Data Integrity & Location (CONFIRMED)**
- `ExtractCity` in `pkg/normalizer/clean_address.go` falls back to `targetCity` if no city is found in the address. This injects "fake cities" and causes data contamination (e.g., Gresik entering Surabaya datasets).
- `IsInTargetLocation` in `pkg/normalizer/address.go` blindly returns `true` if the address string is empty.
- `IsInTargetLocation` passes if city/province/country configurations are missing, allowing overly permissive bounds.

**Job & State Isolation (CONFIRMED)**
- `GetPaginatedRecords` in `pkg/job/manager.go` checks fallback paths like `Hasil/Scrape_Iterative.csv` or `parallel_results/parallel_stores_combined.csv` which are global output files, allowing cross-job CSV contamination.
- `runJobExecution` ticker repeatedly reads the entire CSV file (`loadRecordsFromCSV`) every 2 seconds to update `state.Records`.
- Potential Data Race: `state.Records` is updated via `append` in `OnItemFound` and also completely overwritten in the background polling ticker.

**Concurrency & CSV Performance (CONFIRMED)**
- Global mutex bottleneck: `CheckAndSaveRecord` in `pkg/parallel/storage.go` locks the entire state using `sm.mu.Lock()` per record across all workers.
- CSV I/O bottleneck: `appendCombined` opens, writes, and closes the `mainOutputFile` for every single record, severely hurting performance and disk I/O.

**Scraping Quality (POTENTIAL)**
- Weak Place ID extraction logic and limitations on scrolling behaviour inside worker implementation.
- Browser/context cancellation might leave orphan instances if workers don't forcefully close contexts upon manager stop/cancellation.

## 4. Severity Classification

- **P0 — Critical**: Data Integrity & Contamination (Fake City Injection, Permissive Validation, Cross-Job State Mixing). Must be fixed immediately.
- **P1 — High**: Race conditions, CSV I/O thrashing, Global Mutex bottlenecks.
- **P2 — Medium**: Sub-optimal Scraping Quality (Place ID extraction, scroll limits, context cancellation).
- **P3 — Low**: General code cleanup, UI enhancements.

## 5. Phase Roadmap

```text
Phase 0 -> Baseline & Safety
   ↓
Phase 1 -> Critical Data Integrity & Location Validation
   ↓
Phase 2 -> Job Isolation, State & CSV Stability
   ↓
Phase 3 -> 5-Worker Concurrency & Performance
   ↓
Phase 4 -> Scraping Quality & Reliability
   ↓
Phase 5 -> Final Regression & Production Readiness
```

*Dependency Logic*: You cannot optimize concurrency (Phase 3) if the scraper is writing invalid mixed data (Phase 1) or saving to contaminated global state (Phase 2). Each phase provides a stable foundation for the next.

---

## 6. Phase 0: Baseline & Safety

### Objective
Ensure the project is safely version-controlled and that baseline behaviour is documented before making ANY code changes.

### Tasks
- Verify Git repository initialization. If not using Git, initialize it.
- Run baseline build (`go build ./...`).
- Verify existing tests (`go test ./...`).
- Document baseline performance and functional status.

### Acceptance Criteria
- [ ] Git repository exists, and the working directory is clean.
- [ ] Initial "before phase 1" checkpoint is created.
- [ ] Build passes without syntax errors.

### Rollback Point
Create a commit: `git commit -m "checkpoint: before phase 1"`

### Handoff Requirements
Report the baseline build and test status.

---

## 7. Phase 1: Critical Data Integrity & Location Validation

### Objective
Ensure the target location from UI strictly propagates to the scraper and no outside territories or "fake cities" enter the dataset. 

### Problems Addressed
- `ExtractCity` fake city injection fallback.
- `IsInTargetLocation` returning `true` on empty fields or missing configurations.
- Sidoarjo and Gresik leaking into Surabaya datasets.

### Required Changes
- Remove the `targetCity` fallback block in `ExtractCity` (`pkg/normalizer/clean_address.go`). Unknown cities must remain empty or explicitly marked "UNKNOWN".
- Patch `IsInTargetLocation` (`pkg/normalizer/address.go`) to fail securely on empty location fields instead of returning `true`.
- Enforce strict negative geographic filtering across all workers.

### Files Likely Affected
- `pkg/normalizer/clean_address.go`
- `pkg/normalizer/address.go`

### Tests
- **Geographic Test**: Feed 5 Surabaya, 2 Gresik, 2 Sidoarjo, 1 Jakarta addresses to the validator targeting "Surabaya".
- **Empty Field Test**: Feed an empty address to `IsInTargetLocation` and verify it rejects.

### Acceptance Criteria
- [ ] No fake City injection exists in normalizer.
- [ ] Empty location strings are rejected (INVALID).
- [ ] 100% rejection of Gresik, Sidoarjo, and Jakarta records when target is Surabaya.
- [ ] Tests pass.

### Rollback Point
Create a commit: `git commit -m "checkpoint: after phase 1"`

---

## 8. Phase 2: Job Isolation, State & CSV Stability

### Objective
Ensure scraping jobs do not mix data, UI records requests return isolated data, and eliminate data races regarding `state.Records`.

### Problems Addressed
- Cross-job contamination reading global `Hasil/Scrape_Iterative.csv` or `parallel_results/parallel_stores_combined.csv` in `GetPaginatedRecords`.
- Massive background CSV parsing every 2 seconds in `runJobExecution` (`manager.go`).
- Race condition writing/reading `state.Records`.

### Required Changes
- Modify `GetPaginatedRecords` (`pkg/job/manager.go`) to ONLY read from the job-specific export CSV: `data/exports/<jobID>.csv`. Remove fallback candidate paths.
- Remove the `loadRecordsFromCSV` polling ticker from `runJobExecution`. Instead, maintain `state.Records` entirely in memory during run, using the thread-safe `OnItemFound` callback properly.
- Protect all `state.Records` access with `m.mu.Lock()`/`Unlock()` properly to pass `go test -race`.

### Files Likely Affected
- `pkg/job/manager.go`

### Tests
- **Job Isolation Test**: Run Job A (Target: Surabaya) and Job B (Target: Jakarta). Fetch `/api/jobs/JobA/records` and verify no Jakarta records exist.
- **Race Detector Test**: `go test -race ./...`
- **UI Refresh Test**: Spam progress refresh, ensure no UI crash or duplicate appending.

### Acceptance Criteria
- [ ] `GetPaginatedRecords` strictly isolations by `jobID`.
- [ ] Periodic whole-CSV parsing is removed.
- [ ] Go race detector passes cleanly.
- [ ] No cross-job contamination.

### Rollback Point
Create a commit: `git commit -m "checkpoint: after phase 2"`

---

## 9. Phase 3: 5-Worker Concurrency & Performance

### Objective
Make the multi-worker architecture efficient by removing global I/O and mutex bottlenecks.

### Problems Addressed
- `appendCombined` (`pkg/parallel/storage.go`) opens and closes the CSV file for every single scraped row.
- `CheckAndSaveRecord` holds a global lock during file I/O operations.

### Required Changes
- Refactor `StorageManager` to keep the `mainOutputFile` handle open during the job execution and close it gracefully in `Close()`.
- Use buffered I/O or a channel-based bounded worker queue to stream CSV writes rather than locking the main scraper threads during disk I/O.
- Ensure the deduplication maps (`seenPlaceIDs`, `seenNameAddrs`) are safely accessed using `RWMutex` without wrapping the slow I/O calls.

### Files Likely Affected
- `pkg/parallel/storage.go`
- `pkg/parallel/engine.go`

### Tests
- **Throughput Test**: Run 5 workers on a high-density target. Measure Records/Min before and after the change.
- **Data Integrity**: Verify no duplicate place IDs are inserted despite the lock refactor.

### Acceptance Criteria
- [ ] `appendCombined` no longer uses `os.OpenFile` per record.
- [ ] CSV writing operates on an open handle or buffered channel.
- [ ] Throughput (Records/Min) improves or remains stable.
- [ ] No duplicates found in final CSV.

### Rollback Point
Create a commit: `git commit -m "checkpoint: after phase 3"`

---

## 10. Phase 4: Scraping Quality & Reliability

### Objective
Improve robust extraction, infinite scroll behaviour, and graceful degradation/cancellation handling.

### Problems Addressed
- Weak Place ID extraction.
- Fixed limit scrolling.
- Context cancellation not properly closing browser pages or leaving orphan goroutines.

### Required Changes
- Audit worker loop (`pkg/parallel/worker.go` or equivalent scraper logic) to ensure `ctx.Done()` is checked in all blocking network/DOM operations.
- Ensure all resources (browsers, pages) defer `Close()` correctly upon cancellation or error.
- Implement dynamic wait/scroll logic for pagination if currently hardcoded.

### Files Likely Affected
- `pkg/parallel/worker.go`
- `pkg/scraper/scraper.go`

### Tests
- **Cancellation Test**: Start a job, let it run for 10 seconds, then cancel it. Ensure all workers immediately stop and no orphan browsers exist.
- **Deep Scroll Test**: Run a broad query and ensure pagination fetches >100 records consistently.

### Acceptance Criteria
- [ ] Immediate worker halt upon Job Cancel.
- [ ] No memory/goroutine leaks after multiple Job Start/Stop cycles.
- [ ] Dynamic scroll logic is confirmed functional.

### Rollback Point
Create a commit: `git commit -m "checkpoint: after phase 4"`

---

## 11. Phase 5: Final Regression & Production Readiness

### Objective
Full system verification ensuring all phases work cohesively without regressions.

### Tasks
- **Location Matrix**: Scraping Surabaya yields only Surabaya. Scraping Jakarta yields only Jakarta.
- **Concurrency**: 5 Workers running concurrently.
- **Load Test**: Generate at least 500 records.
- **UI Walkthrough**: Create Job -> Start -> Monitor Progress -> Cancel -> Export CSV -> Wipe Data.

### Acceptance Criteria
- [ ] All P0, P1, and P2 issues are resolved.
- [ ] `go test -race ./...` passes.
- [ ] UI correctly maps to isolated Job State.
- [ ] Validated CSV output strictly conforms to location constraints.

---

## 12. Rollback Procedure

Before every phase, run:
```bash
git status
git add .
git commit -m "checkpoint: before phase X"
```

If a phase FAILS an acceptance criteria or causes a regression:
1. **STOP** immediately. Do NOT push to fix it iteratively within the same messy state.
2. Record the failure log, error message, and the specific test that failed.
3. Identify the Root Cause.
4. **ROLLBACK**:
   ```bash
   git reset --hard HEAD
   git clean -fd
   ```
   *(Be cautious with `git clean -fd` if there are intentional untracked files you need to keep)*
5. Document the failure in the Phase Handoff format.
6. Propose a new strategy.
7. Retry the phase.

---

## 13. Testing Strategy
- Unit tests for all business logic (`normalizer`, `validator`).
- Integration testing using the UI/API layer with real local HTTP requests.
- Race testing strictly enforced via `go test -race`.

## 14. Regression Matrix
| Feature | Risk Area | Verification |
|---------|-----------|--------------|
| Location Filter | Gresik/Sidoarjo bleeding | CSV validation on target Surabaya |
| Concurrency | Job A data in Job B | Parallel job fetch API check |
| Performance | High CPU / I/O locks | Check records/min and task manager |

## 15. Data Safety Rules
- **DO NOT** delete any production or `archive/` data.
- **DO NOT** overwrite existing critical CSVs during tests. Use temporary job IDs (e.g., `job_test_123`).
- **DO NOT** artificially fabricate missing data to pass validation. If data is missing, flag it as `UNKNOWN` or drop it cleanly.

## 16. Production Readiness Checklist
- [ ] No hardcoded global CSV paths.
- [ ] Context cancellation propagates cleanly.
- [ ] Geobounding validation is strict and tested.
- [ ] No `OpenFile` inside tight loops.
- [ ] No un-mutexed map writes.

---

## 17. Phase Handoff Template

When completing a phase or reporting a failure, use this format:

```markdown
# PHASE X REPORT

## Status
[PASS / FAIL]

## Changes Made
- ...

## Files Changed
- ...

## Test Results
- Geographic Test: [PASS/FAIL]
- Race Test: [PASS/FAIL]
- Isolation Test: [PASS/FAIL]

## Regression Status
[PASS / FAIL]

## Rollback Checkpoint Created
Commit Hash / Backup location: ...

## Ready For Next Phase
[YES / NO]
```

## 18. Final Definition of Done
The system correctly isolates concurrent scraping jobs, enforces strict geographical boundaries avoiding adjacent-city bleed, writes to disk efficiently without blocking workers, avoids race conditions, and correctly halts and cleans up on user cancellation.
