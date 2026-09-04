import asyncio
import json
import time
from playwright.async_api import async_playwright

async def run():
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=True)
        context = await browser.new_context(viewport={"width": 1400, "height": 900})
        page = await context.new_page()

        page.on("console", lambda msg: print(f"[Browser Console] {msg.text}"))
        page.on("pageerror", lambda err: print(f"[Browser Error] {err}"))

        print("\n========================================================")
        print("STEP 1: Navigate to Scraping Control Center UI")
        print("========================================================")
        await page.goto("http://localhost:8080")
        await page.wait_for_selector("#nav-dashboard")
        await page.wait_for_timeout(1000)

        title = await page.locator("#page-title").inner_text()
        print(f"Page title: {title}")

        print("\n========================================================")
        print("STEP 2: Wipe all previous test data for clean slate")
        print("========================================================")
        # Call wipe API directly
        await page.evaluate("() => fetch('http://localhost:8080/api/system/wipe', {method: 'POST'})")
        await page.wait_for_timeout(1500)
        await page.click("#btn-global-refresh")
        await page.wait_for_timeout(1500)

        print("\n========================================================")
        print("STEP 3: Navigate to Presets and Launch Surabaya Preset")
        print("========================================================")
        await page.click("#nav-presets")
        await page.wait_for_selector("#presets-grid")
        await page.wait_for_timeout(1000)

        # Click launch on Surabaya preset
        await page.click("button[onclick*='preset-building-surabaya']")
        print("Clicked Launch on Surabaya Preset.")

        # Wait for dashboard transition
        await page.wait_for_selector("#tab-dashboard:not(.hidden)", timeout=10000)
        await page.wait_for_timeout(3000)

        # Stop initial job execution so we can test discrete UI iterations
        jobs_res = await page.evaluate("() => fetch('http://localhost:8080/api/jobs').then(r => r.json())")
        job_id = jobs_res[0]["config"]["id"]
        total_queries = len(jobs_res[0]["config"]["queries"])
        print(f"Created Job ID: {job_id} | Total Queries: {total_queries}")
        assert total_queries == 2865, f"Expected 2865 queries, got {total_queries}"

        await page.evaluate(f"() => fetch('http://localhost:8080/api/jobs/{job_id}/stop', {{method: 'POST'}})")
        await page.wait_for_timeout(2000)
        await page.click("#btn-global-refresh")
        await page.wait_for_timeout(1500)

        print("\n========================================================")
        print("STEP 4: Trigger Iteration 1 from UI (Target: 3 stores)")
        print("========================================================")
        await page.click("button:has-text('Run Iteration')")
        await page.wait_for_selector("#modal-start-iteration:not(.hidden)")

        # Select Strategy stores
        await page.select_option("#iter-strategy", "stores")
        await page.fill("#iter-target-stores", "3")
        print("Configured Iteration 1: Strategy Stores, Target = 3")

        start_time = time.time()
        await page.click("button[onclick='submitStartIteration()']")
        print("Clicked Start Iteration 1.")

        # Wait for iteration to be active in UI
        await page.wait_for_timeout(2000)

        # Monitor iteration running in UI
        iter_completed = False
        for tick in range(60): # wait up to 120 seconds
            await page.wait_for_timeout(2000)
            
            status_text = await page.locator("#active-job-indicator").inner_text()
            monitor_text = await page.locator("#active-iteration-monitor").inner_text()
            elapsed = int(time.time() - start_time)
            
            print(f"[T+{elapsed}s] Job Indicator: {status_text}")
            print(f"   Active Monitor: {monitor_text.replace(chr(10), ' | ')}")

            if "COMPLETED" in status_text or "No active iteration" in monitor_text:
                if elapsed >= 3: # Must have actually run, not completed in 0s
                    print(f"Iteration 1 finished in {elapsed}s!")
                    iter_completed = True
                    break

        assert iter_completed, "Iteration 1 did not complete in time"

        # Verify Iteration 1 results
        jobs_res = await page.evaluate("() => fetch('http://localhost:8080/api/jobs').then(r => r.json())")
        job = jobs_res[0]
        iters = job.get("iterations", [])
        print(f"\nJob Iterations count: {len(iters)}")
        assert len(iters) >= 1, "Expected at least 1 iteration in history"
        iter1 = iters[0]
        print(f"Iteration 1 State: {json.dumps(iter1, indent=2)}")
        assert iter1["status"] == "COMPLETED", f"Expected COMPLETED, got {iter1['status']}"
        assert iter1["stores_found"] > 0, f"Expected stores_found > 0, got {iter1['stores_found']}"
        assert iter1["queries_processed"] > 0, f"Expected queries_processed > 0, got {iter1['queries_processed']}"

        print("\n========================================================")
        print("STEP 5: Trigger Iteration 2 from UI (Target: 3 stores)")
        print("========================================================")
        await page.click("button:has-text('Run Iteration')")
        await page.wait_for_selector("#modal-start-iteration:not(.hidden)")

        await page.select_option("#iter-strategy", "stores")
        await page.fill("#iter-target-stores", "3")
        print("Configured Iteration 2: Strategy Stores, Target = 3")

        start_time2 = time.time()
        await page.click("button[onclick='submitStartIteration()']")
        print("Clicked Start Iteration 2.")

        await page.wait_for_timeout(2000)

        iter2_completed = False
        for tick in range(60):
            await page.wait_for_timeout(2000)
            
            status_text = await page.locator("#active-job-indicator").inner_text()
            monitor_text = await page.locator("#active-iteration-monitor").inner_text()
            elapsed = int(time.time() - start_time2)
            
            print(f"[T+{elapsed}s] Job Indicator: {status_text}")
            print(f"   Active Monitor: {monitor_text.replace(chr(10), ' | ')}")

            if "COMPLETED" in status_text or "No active iteration" in monitor_text:
                if elapsed >= 3:
                    print(f"Iteration 2 finished in {elapsed}s!")
                    iter2_completed = True
                    break

        assert iter2_completed, "Iteration 2 did not complete in time"

        # Verify Iteration 2 results
        jobs_res = await page.evaluate("() => fetch('http://localhost:8080/api/jobs').then(r => r.json())")
        job = jobs_res[0]
        iters = job.get("iterations", [])
        print(f"\nJob Iterations count: {len(iters)}")
        assert len(iters) >= 2, "Expected at least 2 iterations in history"
        iter2 = iters[1]
        print(f"Iteration 2 State: {json.dumps(iter2, indent=2)}")
        assert iter2["status"] == "COMPLETED", f"Expected COMPLETED, got {iter2['status']}"
        assert iter2["stores_found"] > 0, f"Expected stores_found > 0, got {iter2['stores_found']}"

        print("\n========================================================")
        print("STEP 6: Verify Records & Output CSV from UI Explorer")
        print("========================================================")
        await page.click("#nav-records")
        await page.wait_for_selector("#records-table-body")
        await page.wait_for_timeout(1000)
        records_label = await page.locator("#records-counter-label").inner_text()
        print(f"Records table: {records_label}")
        
        file_meta = await page.locator("#records-file-metadata").inner_text()
        print(f"CSV Metadata: {file_meta}")

        print("\n========================================================")
        print("ALL END-TO-END UI ITERATIVE SCRAPING TESTS PASSED!")
        print("========================================================")

        await browser.close()

if __name__ == "__main__":
    asyncio.run(run())
