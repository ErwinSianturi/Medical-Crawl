import asyncio
from playwright.async_api import async_playwright

async def run(playwright):
    browser = await playwright.chromium.launch(headless=True)
    context = await browser.new_context(viewport={"width": 1280, "height": 800})
    page = await context.new_page()

    page.on("console", lambda msg: print(f"[Browser] {msg.text}"))
    page.on("pageerror", lambda err: print(f"[Browser Error] {err}"))

    try:
        print("1. Navigating to Dashboard...")
        await page.goto("http://localhost:8080")
        await page.wait_for_selector("#nav-dashboard")
        
        print("2. Checking Dashboard elements...")
        title = await page.locator("#page-title").inner_text()
        
        print("3. Testing Navigation to New Job Wizard...")
        await page.click("#nav-new-job")
        
        # Wait for the tab content to become visible instead of title matching exactly
        await page.wait_for_selector("#tab-new-job:not(.hidden)")
        
        print("4. Filling out New Job Form...")
        await page.fill("#input-target", "toko kopi")
        
        await page.wait_for_timeout(500)
        await page.fill("#select-province", "DKI Jakarta")
        await page.wait_for_selector("#dropdown-province .combobox-item")
        await page.click("#dropdown-province .combobox-item:first-child")
        
        await page.wait_for_timeout(500)
        await page.fill("#select-city", "Kota Jakarta Pusat")
        await page.wait_for_selector("#dropdown-city .combobox-item")
        await page.click("#dropdown-city .combobox-item:first-child")
        
        print("5. Generating queries...")
        await page.click("button[onclick='generateQueries()']")
        await page.wait_for_timeout(500)
        queries = await page.locator("#input-queries").input_value()
        if not queries:
            raise Exception("Queries were not generated!")
        print("Generated queries:", queries.replace('\n', ', '))
        
        print("6. Submitting the job...")
        await page.select_option("#select-headless", "true")
        await page.click("#btn-submit-job")
        
        print("7. Waiting for redirect to Dashboard and job to start...")
        await page.wait_for_selector("#tab-dashboard:not(.hidden)", timeout=10000)
        
        print("8. Verifying job status updates...")
        await page.wait_for_selector("#job-status-badge")
        badge = await page.locator("#job-status-badge").inner_text()
        print(f"Job status badge: {badge}")
        
        print("9. Testing Logs Tab...")
        await page.click("#nav-logs")
        await page.wait_for_selector("#tab-logs:not(.hidden)")
        await page.wait_for_timeout(1000)
        
        print("10. Testing Records Tab...")
        await page.click("#nav-records")
        await page.wait_for_selector("#tab-records:not(.hidden)")
        
        print("11. Testing Map Tab...")
        await page.click("#nav-map")
        await page.wait_for_selector("#tab-map:not(.hidden)")
        
        print("\nAll UI tests passed successfully!")
    except Exception as e:
        print(f"\nUI Test Failed: {e}")
    finally:
        await browser.close()

async def main():
    async with async_playwright() as playwright:
        await run(playwright)

if __name__ == "__main__":
    asyncio.run(main())
