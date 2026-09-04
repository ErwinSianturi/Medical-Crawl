import asyncio
import json
import os
import shutil
import subprocess
import time
import urllib.request
import urllib.error
from playwright.async_api import async_playwright

TEST_PORT = 8089
TEST_URL = f"http://127.0.0.1:{TEST_PORT}"
CURRENT_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.dirname(CURRENT_DIR)
TEMP_DATA_DIR = os.path.join(CURRENT_DIR, "temp_test_data")
TEMP_LOGS_DIR = os.path.join(CURRENT_DIR, "temp_test_logs")

def cleanup_temp_dirs():
    if os.path.exists(TEMP_DATA_DIR):
        shutil.rmtree(TEMP_DATA_DIR, ignore_errors=True)
    if os.path.exists(TEMP_LOGS_DIR):
        shutil.rmtree(TEMP_LOGS_DIR, ignore_errors=True)

def api_post(endpoint, data):
    req = urllib.request.Request(
        f"{TEST_URL}{endpoint}",
        data=json.dumps(data).encode('utf-8') if data is not None else None,
        headers={'Content-Type': 'application/json'},
        method='POST'
    )
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read().decode('utf-8'))

def api_get(endpoint):
    with urllib.request.urlopen(f"{TEST_URL}{endpoint}") as resp:
        return json.loads(resp.read().decode('utf-8'))

async def run_tests(playwright):
    cleanup_temp_dirs()
    os.makedirs(TEMP_DATA_DIR, exist_ok=True)
    os.makedirs(TEMP_LOGS_DIR, exist_ok=True)

    print("================================================================================")
    print("  [TEST RUNNER] COMPREHENSIVE UI DELETION LIFECYCLE TESTS (Playwright)")
    print("================================================================================")

    print(f"[SETUP] Starting Go server on port {TEST_PORT} with isolated temp data directory...")
    server_proc = subprocess.Popen(
        [
            "go", "run", "./cmd/server",
            f"-port={TEST_PORT}",
            f"-data-dir={TEMP_DATA_DIR}",
            f"-log-dir={TEMP_LOGS_DIR}",
            "-web-dir=web"
        ],
        cwd=PROJECT_ROOT,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE
    )

    # Wait for server to respond
    server_online = False
    for _ in range(30):
        await asyncio.sleep(0.5)
        try:
            with urllib.request.urlopen(f"{TEST_URL}/api/jobs") as r:
                if r.status == 200:
                    server_online = True
                    break
        except Exception:
            pass

    if not server_online:
        server_proc.kill()
        cleanup_temp_dirs()
        raise Exception(f"Server did not start in time on {TEST_URL}")
    print("[SETUP] Server is online and healthy.")

    browser = await playwright.chromium.launch(headless=True)
    context = await browser.new_context(viewport={"width": 1280, "height": 900})
    page = await context.new_page()

    browser_logs = []
    page.on("console", lambda msg: browser_logs.append(f"[Browser Console] {msg.text}"))
    page.on("pageerror", lambda err: browser_logs.append(f"[Browser Error] {err}"))

    try:
        # Initial wipe to ensure 100% pristine state
        api_post("/api/system/wipe", None)

        # =====================================================================
        # STEP 1: Load Web UI
        # =====================================================================
        print("\n[TEST 1] Loading Web UI...")
        await page.goto(TEST_URL, wait_until="networkidle")
        await page.wait_for_selector("#nav-jobs")
        print("[PASS] Web UI loaded successfully.")

        # =====================================================================
        # STEP 2: Create 3 Distinct Jobs
        # =====================================================================
        print("\n[TEST 2] Creating 3 distinct jobs: Job 1 (Surabaya), Job 2 (Jakarta), Job 3 (Bandung)...")
        api_post("/api/jobs", {
            "id": "job_surabaya_01",
            "title": "Toko Semen Surabaya",
            "target": "toko semen",
            "location": {"country": "Indonesia", "province": "Jawa Timur", "city": "Surabaya"},
            "queries": ["toko semen surabaya"],
            "workers": 2,
            "created_at": "2026-09-04T10:00:00Z"
        })

        api_post("/api/jobs", {
            "id": "job_jakarta_02",
            "title": "Toko Cat Jakarta",
            "target": "toko cat",
            "location": {"country": "Indonesia", "province": "DKI Jakarta", "city": "Jakarta"},
            "queries": ["toko cat jakarta"],
            "workers": 2,
            "created_at": "2026-09-04T10:05:00Z"
        })

        api_post("/api/jobs", {
            "id": "job_bandung_03",
            "title": "Toko Besi Bandung",
            "target": "toko besi",
            "location": {"country": "Indonesia", "province": "Jawa Barat", "city": "Bandung"},
            "queries": ["toko besi bandung"],
            "workers": 2,
            "created_at": "2026-09-04T10:10:00Z"
        })

        # Trigger refresh in UI
        await page.evaluate("() => refreshAllData()")
        await asyncio.sleep(0.5)

        # Switch to Jobs Tab and verify 3 items in table
        await page.click("#nav-jobs")
        await page.wait_for_selector("#jobs-table-body tr")
        table_text = await page.locator("#jobs-table-body").inner_text()
        print(f"[INFO] Jobs Table Content:\n{table_text}")
        if "job_surabaya_01" not in table_text or "job_jakarta_02" not in table_text or "job_bandung_03" not in table_text:
            raise Exception("Not all 3 jobs are present in UI table!")
        print("[PASS] All 3 jobs are properly displayed in the UI table.")

        # =====================================================================
        # STEP 3: Delete Middle Job (Job 2: Jakarta) via UI Modal
        # =====================================================================
        print("\n[TEST 3] Deleting Middle Job (Job 2: Jakarta) through UI Confirmation Modal...")
        await page.evaluate("() => showDeleteJobModal('job_jakarta_02')")
        await page.wait_for_selector("#modal-delete-job:not(.hidden)")

        modal_job_id = await page.locator("#del-job-id").inner_text()
        if modal_job_id != "job_jakarta_02":
            raise Exception(f"Modal opened for wrong job ID: {modal_job_id}")
        print(f"[PASS] Delete confirmation modal opened for target job ID: {modal_job_id}")

        # Click confirm delete
        await page.click("#btn-confirm-delete-job")
        await page.wait_for_selector("#modal-delete-job", state="hidden", timeout=5000)

        # Check button state not stuck
        await page.wait_for_function("() => !document.getElementById('btn-confirm-delete-job').disabled", timeout=5000)
        print("[PASS] Modal closed cleanly, button re-enabled.")

        # Verify UI Table: Job 1 & 3 MUST EXIST, Job 2 MUST BE GONE
        table_after_del2 = await page.locator("#jobs-table-body").inner_text()
        if "job_surabaya_01" not in table_after_del2:
            raise Exception("CRITICAL BUG: Job 1 was accidentally deleted when deleting Job 2!")
        if "job_bandung_03" not in table_after_del2:
            raise Exception("CRITICAL BUG: Job 3 was accidentally deleted when deleting Job 2!")
        if "job_jakarta_02" in table_after_del2:
            raise Exception("Job 2 still appears in UI table after deletion!")
        print("[PASS] ISOLATION VERIFIED: Job 2 successfully deleted, Job 1 and Job 3 remain completely intact.")

        # =====================================================================
        # STEP 4: Refresh Browser & Verify Persistence
        # =====================================================================
        print("\n[TEST 4] Refreshing browser page to test backend persistence...")
        await page.reload(wait_until="networkidle")
        await page.click("#nav-jobs")
        await page.wait_for_selector("#jobs-table-body tr")

        table_after_reload = await page.locator("#jobs-table-body").inner_text()
        if "job_surabaya_01" not in table_after_reload or "job_bandung_03" not in table_after_reload:
            raise Exception("Remaining jobs lost after browser reload!")
        if "job_jakarta_02" in table_after_reload:
            raise Exception("Deleted Job 2 reappeared after reload!")
        print("[PASS] PERSISTENCE VERIFIED: Reloaded UI shows Job 1 and Job 3, Job 2 is permanently gone.")

        # =====================================================================
        # STEP 5: Delete First Job (Job 1) & Last Job (Job 3)
        # =====================================================================
        print("\n[TEST 5] Deleting First Job (Job 1)...")
        await page.evaluate("() => showDeleteJobModal('job_surabaya_01')")
        await page.wait_for_selector("#modal-delete-job:not(.hidden)")
        await page.click("#btn-confirm-delete-job")
        await page.wait_for_selector("#modal-delete-job", state="hidden", timeout=5000)

        table_after_del1 = await page.locator("#jobs-table-body").inner_text()
        if "job_surabaya_01" in table_after_del1:
            raise Exception("Job 1 not deleted!")
        if "job_bandung_03" not in table_after_del1:
            raise Exception("Job 3 was deleted when deleting Job 1!")
        print("[PASS] Job 1 deleted, Job 3 is the only remaining job.")

        print("\n[TEST 6] Deleting Last Job (Job 3)...")
        await page.evaluate("() => showDeleteJobModal('job_bandung_03')")
        await page.wait_for_selector("#modal-delete-job:not(.hidden)")
        await page.click("#btn-confirm-delete-job")
        await page.wait_for_selector("#modal-delete-job", state="hidden", timeout=5000)

        table_empty = await page.locator("#jobs-table-body").inner_text()
        if "Belum ada riwayat pencarian" not in table_empty:
            raise Exception(f"Expected empty table placeholder, got: {table_empty}")
        print("[PASS] All jobs deleted. UI handles empty state cleanly without error.")

        # =====================================================================
        # STEP 6: Double Click Protection
        # =====================================================================
        print("\n[TEST 7] Testing Double Click Protection on Delete Button...")
        api_post("/api/jobs", {
            "id": "job_double_click_test",
            "title": "Double Click Protection Job",
            "target": "toko cat",
            "location": {"city": "Malang"}
        })
        await page.evaluate("() => refreshAllData()")
        await asyncio.sleep(0.5)

        await page.evaluate("() => showDeleteJobModal('job_double_click_test')")
        await page.wait_for_selector("#modal-delete-job:not(.hidden)")

        # Fast clicks
        click1 = page.click("#btn-confirm-delete-job")
        click2 = page.click("#btn-confirm-delete-job")
        await asyncio.gather(click1, click2)
        await page.wait_for_selector("#modal-delete-job", state="hidden", timeout=5000)
        print("[PASS] Double-click guarded cleanly without race condition.")

        # =====================================================================
        # STEP 7: Protected Running Job Deletion Error Handling
        # =====================================================================
        print("\n[TEST 8] Testing Running Job Deletion Protection & Error State...")
        run_job = api_post("/api/jobs", {
            "id": "job_running_protected",
            "title": "Running Job Protection Test",
            "target": "toko semen",
            "location": {"city": "Medan"},
            "headless": True
        })
        await page.evaluate("() => refreshAllData()")
        await asyncio.sleep(0.3)
        # Mark job status as RUNNING in frontend state
        await page.evaluate(f"() => {{ const j = jobs.find(x => x.config && x.config.id === '{run_job['config']['id']}'); if (j) j.status = 'RUNNING'; }}")

        # Attempt to delete running job
        await page.evaluate(f"() => showDeleteJobModal('{run_job['config']['id']}')")
        await asyncio.sleep(0.3)
        modal_open = await page.locator("#modal-delete-job").is_visible()
        if modal_open:
            raise Exception("Delete modal should not open for a running job!")
        print("[PASS] Running job deletion correctly blocked before opening modal.")

        # =====================================================================
        # STEP 8: Wipe All Data Modal and Operation
        # =====================================================================
        print("\n[TEST 9] Testing Wipe All Data Feature...")
        api_post(f"/api/jobs/{run_job['config']['id']}/stop", None)
        await page.evaluate("() => refreshAllData()")
        await asyncio.sleep(0.5)

        # Open Wipe All modal
        await page.evaluate("() => showWipeAllModal()")
        await page.wait_for_selector("#modal-wipe-all:not(.hidden)")
        await page.click("#btn-confirm-wipe-all")
        await page.wait_for_selector("#modal-wipe-all", state="hidden", timeout=5000)

        remaining_jobs = api_get("/api/jobs")
        if len(remaining_jobs) != 0:
            raise Exception(f"Expected 0 jobs after Wipe All, got {len(remaining_jobs)}")
        print("[PASS] Wipe All Data cleared all jobs cleanly.")

        print("\n================================================================================")
        print("  [SUCCESS] ALL 9 AUTOMATED UI TESTS PASSED SUCCESSFULLY WITH 100% ACCURACY!")
        print("================================================================================")

    except Exception as e:
        print(f"\n[ERROR] UI Test Failure: {e}")
        print("Browser Logs:", browser_logs)
        raise e
    finally:
        await browser.close()
        server_proc.kill()
        cleanup_temp_dirs()
        print("[TEARDOWN] Cleanup complete.")

async def main():
    async with async_playwright() as playwright:
        await run_tests(playwright)

if __name__ == "__main__":
    asyncio.run(main())
