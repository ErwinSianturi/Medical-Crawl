const puppeteer = require('puppeteer');

(async () => {
    console.log("Starting UI tests...");
    const browser = await puppeteer.launch({ headless: true });
    const page = await browser.newPage();
    
    // Set viewport
    await page.setViewport({ width: 1280, height: 800 });
    
    let logs = [];
    page.on('console', msg => logs.push(`[Browser] ${msg.text()}`));
    page.on('pageerror', err => logs.push(`[Browser Error] ${err.toString()}`));
    
    try {
        console.log("1. Navigating to Dashboard...");
        await page.goto('http://localhost:8080');
        await page.waitForSelector('#nav-dashboard');
        
        console.log("2. Checking Dashboard elements...");
        const title = await page.$eval('#page-title', el => el.innerText);
        if(title !== 'Scraping Dashboard') throw new Error("Dashboard title mismatch: " + title);
        
        console.log("3. Testing Navigation to New Job Wizard...");
        await page.click('#nav-new-job');
        await page.waitForFunction(() => document.getElementById('page-title').innerText === 'New Job Setup Wizard');
        
        console.log("4. Filling out New Job Form...");
        await page.type('#input-target', 'toko kopi');
        
        // Wait for province to load
        await page.waitForTimeout(500);
        await page.type('#select-province', 'DKI Jakarta');
        await page.waitForSelector('#dropdown-province .combobox-item');
        await page.click('#dropdown-province .combobox-item');
        
        // Wait for city to load
        await page.waitForTimeout(500);
        await page.type('#select-city', 'Kota Jakarta Pusat');
        await page.waitForSelector('#dropdown-city .combobox-item');
        await page.click('#dropdown-city .combobox-item');
        
        console.log("5. Generating queries...");
        await page.click('button[onclick="generateQueries()"]');
        await page.waitForTimeout(500);
        const queries = await page.$eval('#input-queries', el => el.value);
        if(!queries) throw new Error("Queries were not generated!");
        console.log("Generated queries:", queries);
        
        console.log("6. Submitting the job...");
        // Set headless to ON
        await page.select('#select-headless', 'true');
        await page.click('#btn-submit-job');
        
        console.log("7. Waiting for redirect to Dashboard and job to start...");
        await page.waitForFunction(() => document.getElementById('page-title').innerText === 'Scraping Dashboard', {timeout: 10000});
        
        // Wait for progress to show
        console.log("8. Verifying job status updates...");
        await page.waitForSelector('#job-status-badge');
        const badge = await page.$eval('#job-status-badge', el => el.innerText);
        console.log("Job status badge:", badge);
        
        console.log("9. Testing Logs Tab...");
        await page.click('#nav-logs');
        await page.waitForFunction(() => document.getElementById('page-title').innerText === 'Logs Viewer');
        await page.waitForTimeout(2000); // let logs stream
        
        console.log("10. Testing Records Tab...");
        await page.click('#nav-records');
        await page.waitForFunction(() => document.getElementById('page-title').innerText === 'Scraper Records Explorer');
        
        console.log("11. Testing Map Tab...");
        await page.click('#nav-map');
        await page.waitForFunction(() => document.getElementById('page-title').innerText === 'Scraped Locations Map');
        
        console.log("All UI tests passed!");
    } catch(err) {
        console.error("UI Test Failed:", err);
        console.log("Browser Logs:", logs);
    } finally {
        await browser.close();
    }
})();
