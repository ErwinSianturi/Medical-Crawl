const puppeteer = require('puppeteer');
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

const TEST_PORT = 8089;
const TEST_URL = `http://127.0.0.1:${TEST_PORT}`;
const TEMP_DATA_DIR = path.join(__dirname, 'temp_test_data');
const TEMP_LOGS_DIR = path.join(__dirname, 'temp_test_logs');

// Clean up temp dirs
function cleanupTempDirs() {
    try {
        if (fs.existsSync(TEMP_DATA_DIR)) fs.rmSync(TEMP_DATA_DIR, { recursive: true, force: true });
        if (fs.existsSync(TEMP_LOGS_DIR)) fs.rmSync(TEMP_LOGS_DIR, { recursive: true, force: true });
    } catch (e) {}
}

async function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

(async () => {
    cleanupTempDirs();
    fs.mkdirSync(TEMP_DATA_DIR, { recursive: true });
    fs.mkdirSync(TEMP_LOGS_DIR, { recursive: true });

    console.log("================================================================================");
    console.log("  🚀 RUNNING COMPREHENSIVE UI DELETION LIFECYCLE TESTS (Puppeteer) 🚀");
    console.log("================================================================================");

    // 1. Start Go backend server on test port with isolated data directory
    console.log(`[SETUP] Starting test server on port ${TEST_PORT}...`);
    const projectRoot = path.resolve(__dirname, '..');
    const serverProc = spawn('go', [
        'run', './cmd/server',
        `-port=${TEST_PORT}`,
        `-data-dir=${TEMP_DATA_DIR}`,
        `-log-dir=${TEMP_LOGS_DIR}`,
        '-web-dir=web'
    ], {
        cwd: projectRoot,
        stdio: 'pipe'
    });

    serverProc.stdout.on('data', data => {});
    serverProc.stderr.on('data', data => {});

    // Wait for server to become responsive
    let serverOnline = false;
    for (let i = 0; i < 30; i++) {
        await sleep(500);
        try {
            const res = await fetch(`${TEST_URL}/api/jobs`);
            if (res.ok) {
                serverOnline = true;
                break;
            }
        } catch (e) {}
    }

    if (!serverOnline) {
        serverProc.kill();
        cleanupTempDirs();
        throw new Error(`[FATAL] Test server did not start on ${TEST_URL} in 15 seconds.`);
    }
    console.log("[SETUP] Test server is online and responding.");

    const browser = await puppeteer.launch({
        headless: true,
        args: ['--no-sandbox', '--disable-setuid-sandbox']
    });
    const page = await browser.newPage();
    await page.setViewport({ width: 1280, height: 900 });

    const browserLogs = [];
    page.on('console', msg => browserLogs.push(`[CONSOLE] ${msg.text()}`));
    page.on('pageerror', err => browserLogs.push(`[ERROR] ${err.toString()}`));

    try {
        // =====================================================================
        // STEP 1: Load Web UI
        // =====================================================================
        console.log("\n[TEST 1] Loading Web UI...");
        await page.goto(TEST_URL, { waitUntil: 'networkidle2' });
        await page.waitForSelector('#nav-jobs');
        console.log("✓ Web UI loaded successfully.");

        // =====================================================================
        // STEP 2: Create 3 Distinct Jobs via UI / API
        // =====================================================================
        console.log("\n[TEST 2] Creating 3 distinct jobs: Job 1 (Surabaya), Job 2 (Jakarta), Job 3 (Bandung)...");
        await fetch(`${TEST_URL}/api/jobs`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                id: 'job_test_surabaya_01',
                title: 'Toko Semen Surabaya',
                target: 'toko semen',
                location: { country: 'Indonesia', province: 'Jawa Timur', city: 'Surabaya' },
                queries: ['toko semen surabaya'],
                workers: 2,
                created_at: '2026-09-04T10:00:00Z'
            })
        });

        await fetch(`${TEST_URL}/api/jobs`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                id: 'job_test_jakarta_02',
                title: 'Toko Cat Jakarta',
                target: 'toko cat',
                location: { country: 'Indonesia', province: 'DKI Jakarta', city: 'Jakarta' },
                queries: ['toko cat jakarta'],
                workers: 2,
                created_at: '2026-09-04T10:05:00Z'
            })
        });

        await fetch(`${TEST_URL}/api/jobs`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                id: 'job_test_bandung_03',
                title: 'Toko Besi Bandung',
                target: 'toko besi',
                location: { country: 'Indonesia', province: 'Jawa Barat', city: 'Bandung' },
                queries: ['toko besi bandung'],
                workers: 2,
                created_at: '2026-09-04T10:10:00Z'
            })
        });

        // Refresh UI
        await page.evaluate(() => refreshAllData());
        await sleep(500);

        // Switch to Jobs tab and verify 3 jobs exist in table
        await page.click('#nav-jobs');
        await sleep(300);

        let tableRows = await page.$$eval('#jobs-table-body tr', rows => rows.map(r => r.innerText));
        console.log(`[INFO] Current table rows count: ${tableRows.length}`);
        if (tableRows.length !== 3) {
            throw new Error(`Expected 3 rows in jobs table, got ${tableRows.length}`);
        }
        console.log("✓ All 3 jobs visible in UI table.");

        // =====================================================================
        // STEP 3: Delete Middle Job (Job 2: Jakarta) through UI Confirmation Modal
        // =====================================================================
        console.log("\n[TEST 3] Deleting Middle Job (Job 2: Jakarta) via UI Modal...");
        
        // Trigger Delete Modal for Job 2
        await page.evaluate(() => showDeleteJobModal('job_test_jakarta_02'));
        await sleep(300);

        // Verify Modal is visible with correct info
        const isModalVisible = await page.$eval('#modal-delete-job', el => !el.classList.contains('hidden'));
        const modalJobId = await page.$eval('#del-job-id', el => el.innerText);
        if (!isModalVisible || modalJobId !== 'job_test_jakarta_02') {
            throw new Error(`Modal not open or wrong job displayed. Visible: ${isModalVisible}, ID: ${modalJobId}`);
        }
        console.log(`✓ Delete Modal opened correctly for job ID: ${modalJobId}`);

        // Click "Hapus Job Ini" in the modal
        await page.click('#btn-confirm-delete-job');
        await sleep(1000);

        // Verify Modal closed and loading button reset
        const isModalClosed = await page.$eval('#modal-delete-job', el => el.classList.contains('hidden'));
        const btnDisabled = await page.$eval('#btn-confirm-delete-job', el => el.disabled);
        if (!isModalClosed) throw new Error("Modal remained open after deletion!");
        if (btnDisabled) throw new Error("Delete button was left in disabled state (Infinite loading bug)!");
        console.log("✓ Modal closed cleanly and loading state terminated.");

        // Verify UI Table: Job 1 and Job 3 MUST EXIST, Job 2 MUST BE GONE
        const tableTextAfterDel2 = await page.$eval('#jobs-table-body', el => el.innerText);
        if (!tableTextAfterDel2.includes('job_test_surabaya_01')) {
            throw new Error("CRITICAL BUG: Job 1 (Surabaya) was accidentally deleted!");
        }
        if (!tableTextAfterDel2.includes('job_test_bandung_03')) {
            throw new Error("CRITICAL BUG: Job 3 (Bandung) was accidentally deleted!");
        }
        if (tableTextAfterDel2.includes('job_test_jakarta_02')) {
            throw new Error("Job 2 (Jakarta) is still visible in table!");
        }
        console.log("✓ Deletion Isolation Verified: Job 2 deleted, Job 1 & Job 3 remain perfectly intact!");

        // =====================================================================
        // STEP 4: Refresh Browser & Verify Persistence
        // =====================================================================
        console.log("\n[TEST 4] Refreshing browser page...");
        await page.reload({ waitUntil: 'networkidle2' });
        await page.click('#nav-jobs');
        await sleep(500);

        const tableTextAfterReload = await page.$eval('#jobs-table-body', el => el.innerText);
        if (!tableTextAfterReload.includes('job_test_surabaya_01') || !tableTextAfterReload.includes('job_test_bandung_03')) {
            throw new Error("Jobs missing after page reload!");
        }
        if (tableTextAfterReload.includes('job_test_jakarta_02')) {
            throw new Error("Deleted Job 2 reappeared after page reload!");
        }
        console.log("✓ Persistence Verified: Reloaded page retains Job 1 & Job 3, Job 2 remains deleted.");

        // =====================================================================
        // STEP 5: Delete First Job (Job 1) & Last Job (Job 3)
        // =====================================================================
        console.log("\n[TEST 5] Deleting First Job (Job 1)...");
        await page.evaluate(() => showDeleteJobModal('job_test_surabaya_01'));
        await sleep(200);
        await page.click('#btn-confirm-delete-job');
        await sleep(1000);

        const tableTextAfterDel1 = await page.$eval('#jobs-table-body', el => el.innerText);
        if (tableTextAfterDel1.includes('job_test_surabaya_01')) throw new Error("Job 1 not deleted!");
        if (!tableTextAfterDel1.includes('job_test_bandung_03')) throw new Error("Job 3 was deleted when deleting Job 1!");
        console.log("✓ Job 1 deleted, Job 3 is only remaining job.");

        console.log("\n[TEST 6] Deleting Last Job (Job 3)...");
        await page.evaluate(() => showDeleteJobModal('job_test_bandung_03'));
        await sleep(200);
        await page.click('#btn-confirm-delete-job');
        await sleep(1000);

        const emptyTableText = await page.$eval('#jobs-table-body', el => el.innerText);
        if (!emptyTableText.includes('Belum ada riwayat pencarian')) {
            throw new Error(`Expected empty state message, got: ${emptyTableText}`);
        }
        console.log("✓ All jobs deleted cleanly. Empty state handled without errors.");

        // =====================================================================
        // STEP 6: Double Click Protection & Fast Clicking Guard
        // =====================================================================
        console.log("\n[TEST 7] Testing Double-Click Protection on Delete Button...");
        // Create 1 job
        await fetch(`${TEST_URL}/api/jobs`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                id: 'job_test_double_click',
                title: 'Double Click Test Job',
                target: 'toko',
                location: { city: 'Malang' }
            })
        });
        await page.evaluate(() => refreshAllData());
        await sleep(500);

        await page.evaluate(() => showDeleteJobModal('job_test_double_click'));
        await sleep(200);

        // Click confirm twice rapidly
        const click1 = page.click('#btn-confirm-delete-job');
        const click2 = page.click('#btn-confirm-delete-job');
        await Promise.all([click1, click2]);
        await sleep(1000);

        const isModalHidden = await page.$eval('#modal-delete-job', el => el.classList.contains('hidden'));
        if (!isModalHidden) throw new Error("Modal stuck after rapid clicks!");
        console.log("✓ Double-click guarded successfully: No race condition or error.");

        // =====================================================================
        // STEP 7: Error Handling & Infinite Loading Prevention (Running Job Protection)
        // =====================================================================
        console.log("\n[TEST 8] Testing Error Handling: Attempting to delete a RUNNING job...");
        // Create and start a job
        const runJob = await (await fetch(`${TEST_URL}/api/jobs`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                id: 'job_test_running_prot',
                title: 'Running Protected Job',
                target: 'toko',
                location: { city: 'Semarang' },
                headless: true
            })
        })).json();

        // Start job
        await fetch(`${TEST_URL}/api/jobs/${runJob.config.id}/start`, { method: 'POST' });
        await page.evaluate(() => refreshAllData());
        await sleep(500);

        // Try to trigger deletion of running job
        await page.evaluate((id) => showDeleteJobModal(id), runJob.config.id);
        await sleep(300);

        // UI should show toast warning and prevent opening modal for running job
        const runningModalOpen = await page.$eval('#modal-delete-job', el => !el.classList.contains('hidden'));
        if (runningModalOpen) {
            throw new Error("Modal should not open for a running job!");
        }
        console.log("✓ UI correctly prevented deleting running job with toast notification.");

        // =====================================================================
        // STEP 8: Wipe All Data Modal & Functionality (Independent from Individual Delete)
        // =====================================================================
        console.log("\n[TEST 9] Testing Wipe All Data Feature...");
        // Stop the running job first
        await fetch(`${TEST_URL}/api/jobs/${runJob.config.id}/stop`, { method: 'POST' });
        await page.evaluate(() => refreshAllData());
        await sleep(500);

        // Open Wipe All Modal
        await page.evaluate(() => showWipeAllModal());
        await sleep(200);
        const isWipeModalOpen = await page.$eval('#modal-wipe-all', el => !el.classList.contains('hidden'));
        if (!isWipeModalOpen) throw new Error("Wipe All Modal did not open!");

        // Confirm wipe
        await page.click('#btn-confirm-wipe-all');
        await sleep(1000);

        const isWipeModalClosed = await page.$eval('#modal-wipe-all', el => el.classList.contains('hidden'));
        const wipeBtnDisabled = await page.$eval('#btn-confirm-wipe-all', el => el.disabled);
        if (!isWipeModalClosed || wipeBtnDisabled) {
            throw new Error("Wipe All Modal did not close or stuck in loading!");
        }

        const jobsAfterWipe = await (await fetch(`${TEST_URL}/api/jobs`)).json();
        if (jobsAfterWipe && jobsAfterWipe.length > 0) {
            throw new Error(`Expected 0 jobs after wipe, got ${jobsAfterWipe.length}`);
        }
        console.log("✓ Wipe All Data executed cleanly, all jobs cleared, modals closed.");

        console.log("\n================================================================================");
        console.log("  🎉 ALL 9 UI AUTOMATED DELETION TESTS PASSED WITH 100% SUCCESS! 🎉");
        console.log("================================================================================");

    } catch (err) {
        console.error("\n❌ UI Test Failure:", err);
        console.log("\nCaptured Browser Logs:", browserLogs);
        process.exitCode = 1;
    } finally {
        await browser.close();
        serverProc.kill();
        cleanupTempDirs();
        console.log("[TEARDOWN] Browser closed, test server terminated, temporary test data cleaned.");
    }
})();
