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
        await page.goto("http://localhost:8080/?v=2.2")
        await page.wait_for_selector("#nav-dashboard")
        
        print("2. Checking Dashboard elements...")
        title = await page.locator("#page-title").inner_text()
        
        print("3. Testing Iteration UI (Run Iteration)")
        await page.click("button[onclick='showStartIterationModal()']")
        await page.wait_for_selector("#modal-start-iteration:not(.hidden)")
        await page.click("button[onclick='submitStartIteration()']")
        await page.wait_for_timeout(1000)
        
        print("4. All UI tests passed successfully!")
    except Exception as e:
        print(f"\nUI Test Failed: {e}")
    finally:
        await browser.close()

async def main():
    async with async_playwright() as playwright:
        await run(playwright)

if __name__ == "__main__":
    asyncio.run(main())
