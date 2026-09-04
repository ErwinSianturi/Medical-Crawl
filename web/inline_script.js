<script>
    let activeTab = 'dashboard';
    let jobs = [];
    let presets = [];
    let currentJob = null;
    let currentRecordsResp = { total: 0, page: 1, total_pages: 1, records: [] };
    let currentPage = 1;
    let map = null;
    let markersGroup = null;

    document.addEventListener('DOMContentLoaded', () => {
      lucide.createIcons();
      initMap();
      refreshAllData();
      initSSE();
      initLocationDropdowns();
    });

    function initMap() {
      map = L.map('map').setView([-7.2575, 112.7521], 12);
      L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
        maxZoom: 19,
        attribution: '&copy; OpenStreetMap'
      }).addTo(map);
      markersGroup = L.layerGroup().addTo(map);
    }

    // UI State Management
    function switchTab(tabId) {
      document.querySelectorAll('.tab-content').forEach(el => {
        el.classList.add('hidden');
        el.classList.remove('active');
      });
      document.querySelectorAll('.nav-item').forEach(el => {
        el.classList.remove('active', 'bg-slate-100', 'text-slate-900', 'bg-slate-800', 'text-white');
        el.classList.add('text-slate-600');
        const icon = el.querySelector('i, svg');
        if (icon) {
          icon.classList.remove('text-blue-600');
          icon.classList.add('text-slate-600');
        }
      });

      const targetTab = document.getElementById(`tab-${tabId}`);
      if (targetTab) {
        targetTab.classList.remove('hidden');
        targetTab.classList.add('active');
      }

      const navBtn = document.getElementById(`nav-${tabId}`);
      if (navBtn) {
        navBtn.classList.add('active', 'bg-slate-100', 'text-slate-900');
        navBtn.classList.remove('text-slate-600', 'text-white');
        const icon = navBtn.querySelector('i, svg');
        if (icon) {
          icon.classList.add('text-blue-600');
          icon.classList.remove('text-slate-600');
        }
      }

      const titles = {
        'dashboard': 'Scraping Dashboard',
        'new-job': 'New Job Setup Wizard',
        'jobs': 'Scraping Jobs Management',
        'presets': 'Saved Presets Catalog',
        'records': 'Scraper Records Explorer',
        'logs': 'Logs Viewer',
        'system': 'System Resource Monitor',
        'map': 'Scraped Locations Map'
      };
      document.getElementById('page-title').innerText = titles[tabId] || 'Control Center';

      if (tabId === 'dashboard' && map) {
        setTimeout(() => map.invalidateSize(), 200);
      }
      if (tabId === 'jobs') {
        renderJobsTable();
      }
      if (tabId === 'presets') {
        renderPresets();
      }
      if (tabId === 'records') {
        fetchCurrentRecords();
      }
    }

    // MAIN MANUAL REFRESH FUNCTION
    async function refreshAllData() {
      const btnLabel = document.getElementById('label-refresh');
      if (btnLabel) btnLabel.innerText = '⟳ Refreshing...';

      await fetchJobs();
      await fetchPresets();
      await fetchMetrics();
      await fetchCurrentRecords();

      const timeStr = new Date().toLocaleTimeString();
      if (btnLabel) btnLabel.innerText = '↻ Muat Ulang';
      document.getElementById('last-updated-footer').innerText = `Update terakhir: ${timeStr}`;
      document.getElementById('job-last-refreshed').innerText = `Update terakhir: ${timeStr}`;
    }

    function onSelectJob(jobId) {
      if (!jobId || !jobs || jobs.length === 0) return;
      const selected = jobs.find(j => j.config.id === jobId);
      if (selected) {
        currentJob = selected;
        renderGlobalJobCard(selected);
        if (typeof renderIterationUI === 'function') renderIterationUI(selected);
        render5WorkersCards(selected.workers || [], selected.config.workers || 5);
        if (typeof fetchCurrentRecords === 'function') fetchCurrentRecords();
      }
    }

    function updateJobSelectorDropdown() {
      const select = document.getElementById('job-selector');
      if (!select) return;
      if (!jobs || jobs.length === 0) {
        select.innerHTML = '<option value="">Belum ada riwayat pencarian</option>';
        return;
      }
      const currentId = currentJob ? currentJob.config.id : '';
      select.innerHTML = jobs.map(j => {
        const isSel = j.config.id === currentId ? 'selected' : '';
        const loc = j.config.location.city || j.config.location.custom_location || 'Indonesia';
        let displayTitle = j.config.title || 'Scraping Job';
        if (displayTitle.length > 40) displayTitle = displayTitle.substring(0, 37) + '...';
        return `<option value="${j.config.id}" ${isSel}>${displayTitle} • ${loc} [${j.status}]</option>`;
      }).join('');
    }

    async function fetchJobs() {
      try {
        const res = await fetch('http://localhost:8080/api/jobs');
        if (!res.ok) return;
        jobs = await res.json() || [];
        updateJobSelectorDropdown();
        
        let target = null;
        if (currentJob) {
          target = jobs.find(j => j.config.id === currentJob.config.id);
        }
        if (!target) {
          target = jobs.find(j => j.status === 'RUNNING' || j.status === 'PAUSED') || jobs[0];
        }

        if (target) {
          currentJob = target;
          renderGlobalJobCard(target);
          if (typeof renderIterationUI === 'function') renderIterationUI(target);
          render5WorkersCards(target.workers || [], target.config.workers || 5);
          
          // Populate historical logs if terminal is empty/placeholder
          const terminal = document.getElementById("log-terminal");
          if (terminal && terminal.innerHTML.includes("[SYSTEM] Logs panel ready")) {
            if (target.logs && target.logs.length > 0) {
              terminal.innerHTML = "";
              const slice = target.logs.slice(-500);
              slice.forEach(payload => {
                const ts = payload.timestamp || payload.Timestamp || '00:00:00';
                const lvl = (payload.level || payload.Level || 'INFO').toUpperCase();
                const msg = payload.message || payload.Message || '';
                const logLine = document.createElement("div");
                const color = lvl === "ERROR" ? "text-rose-400" : (lvl === "WARN" ? "text-amber-400" : "text-slate-300");
                logLine.className = color;
                logLine.innerText = `[${ts}] [${lvl}] ${msg}`;
                terminal.appendChild(logLine);
              });
              terminal.scrollTop = terminal.scrollHeight;
            }
          }
        } else {
          render5WorkersCards([], 5);
        }

        renderJobsTable();
      } catch (err) {}
    }

    async function fetchPresets() {
      try {
        const res = await fetch('http://localhost:8080/api/presets');
        if (!res.ok) return;
        presets = await res.json() || [];
        renderPresets();
      } catch (err) {}
    }

    async function fetchMetrics() {
      try {
        const res = await fetch('http://localhost:8080/api/system/metrics');
        if (!res.ok) return;
        const metrics = await res.json();
        document.getElementById('sys-mem-alloc').innerText = `${metrics.mem_alloc_mb.toFixed(1)} MB`;
        document.getElementById('sys-goroutines').innerText = metrics.goroutines;
      } catch (err) {}
    }

    async function fetchCurrentRecords() {
      const jobId = currentJob ? currentJob.config.id : (jobs[0] ? jobs[0].config.id : 'default');
      const search = document.getElementById('records-search').value;
      const worker = document.getElementById('filter-worker').value;

      try {
        const res = await fetch(`http://localhost:8080/api/jobs/${jobId}/records?page=${currentPage}&limit=50&search=${encodeURIComponent(search)}&worker=${worker}`);
        if (!res.ok) return;
        currentRecordsResp = await res.json();
        renderRecordsTable();
        renderDashboardLatestRecords();
        updateMapMarkers(currentRecordsResp.records || []);
        updateHeaderStats();
      } catch (err) {}
    }

    function updateHeaderStats() {
      const resp = currentRecordsResp;
      document.getElementById('stat-total-records').innerText = resp.total ? resp.total.toLocaleString() : '0';
      document.getElementById('stat-valid-records').innerText = resp.total ? resp.total.toLocaleString() : '0';
      document.getElementById('stat-duplicates').innerText = currentJob ? currentJob.stats.duplicates : 0;

      const fileStat = document.getElementById('stat-file-status');
      const sizeStat = document.getElementById('stat-file-size');
      if (resp.file_exists) {
        fileStat.innerText = 'CSV File Loaded';
        fileStat.className = 'text-xs font-bold text-emerald-400 truncate mt-1';
        sizeStat.innerText = `${resp.file_size_mb ? resp.file_size_mb.toFixed(2) : 0} MB • ${resp.output_file}`;
      } else {
        fileStat.innerText = 'No File Found';
        fileStat.className = 'text-xs font-bold text-amber-400 truncate mt-1';
        sizeStat.innerText = resp.diagnostic || 'CSV not generated yet';
      }

      // Diagnostic banner
      const banner = document.getElementById('diagnostic-banner');
      const diagText = document.getElementById('diagnostic-text');
      document.getElementById('records-file-metadata').innerText = `Source CSV Path: ${resp.output_file || 'Searching...'} (${resp.file_size_mb ? resp.file_size_mb.toFixed(2) : 0} MB)`;

      if (resp.diagnostic || !resp.file_exists) {
        banner.classList.remove('hidden');
        diagText.innerText = resp.diagnostic || `CSV File Path: ${resp.output_file} (Exists: ${resp.file_exists})`;
      } else {
        banner.classList.add('hidden');
      }
    }

    function renderGlobalJobCard(job) {
      if (!job) return;
      const indicator = document.getElementById('active-job-indicator');
      if (indicator) indicator.innerText = `${job.config.title} (${job.status})`;
      const selector = document.getElementById('job-selector');
      if (selector && selector.value !== job.config.id) {
        selector.value = job.config.id;
      }
      document.getElementById('job-title').innerText = job.config.title;
      document.getElementById('job-subtitle').innerText = `Target: ${job.config.target} • Lokasi: ${job.config.location.city || job.config.location.custom_location || 'Seluruh Wilayah'}`;
      
      const badge = document.getElementById('job-status-badge');
      badge.innerText = job.status;
      badge.className = `px-2.5 py-0.5 rounded text-xs font-bold border ${getStatusClass(job.status)}`;

      document.getElementById('job-progress-pct').innerText = `${job.progress.toFixed(1)}%`;
      document.getElementById('job-progress-bar').style.width = `${job.progress.toFixed(1)}%`;
      document.getElementById('job-records-count').innerText = `${currentRecordsResp.total || job.current_count || 0} / ${job.target_count.toLocaleString()} data`;
    }

    function render5WorkersCards(workersList, count = 5) {
      const container = document.getElementById('workers-grid');
      const cards = [];

      for (let i = 1; i <= count; i++) {
        const w = (workersList && workersList[i-1]) ? workersList[i-1] : {
          id: i,
          status: 'IDLE',
          progress: 0,
          records_found: 0,
          errors: 0,
          current_query: '—',
          elapsed_time: '00:00'
        };

        cards.push(`
          <div class="bg-dark-800 border border-slate-800 p-4 rounded-xl space-y-3 flex flex-col justify-between hover:border-slate-700 transition">
            <div class="flex items-center justify-between">
              <span class="text-xs font-bold text-slate-100 flex items-center">
                <span class="w-2 h-2 rounded-full ${w.status === 'RUNNING' ? 'bg-cyan-400 animate-pulse' : (w.status === 'COMPLETED' ? 'bg-emerald-400' : (w.status === 'FAILED' ? 'bg-rose-500' : 'bg-slate-600'))} mr-2"></span>
                WORKER 0${i}
              </span>
              <span class="px-2 py-0.5 rounded text-[10px] font-bold ${getWorkerStatusClass(w.status)}">${w.status}</span>
            </div>

            <div class="space-y-1">
              <div class="flex justify-between text-[11px] font-mono">
                <span class="text-slate-400">Progress</span>
                <span class="text-cyan-400 font-bold">${(w.progress || 0).toFixed(0)}%</span>
              </div>
              <div class="w-full bg-slate-900 h-2 rounded-full overflow-hidden border border-slate-800">
                <div class="bg-cyan-500 h-full transition-all duration-300" style="width: ${(w.progress || 0).toFixed(0)}%"></div>
              </div>
            </div>

            <div class="grid grid-cols-2 gap-2 bg-slate-900/60 p-2.5 rounded-lg border border-slate-800/80 text-[11px] font-mono">
              <div>
                <span class="text-slate-500 block text-[10px]">Records</span>
                <span class="text-slate-200 font-bold">${(w.records_found || 0).toLocaleString()}</span>
              </div>
              <div>
                <span class="text-slate-500 block text-[10px]">Errors</span>
                <span class="text-rose-400 font-bold">${w.errors || 0}</span>
              </div>
            </div>

            <div class="text-[10px] text-slate-400 space-y-1 font-mono truncate">
              <div class="truncate text-cyan-400"><span class="text-slate-500">Target:</span> ${w.current_location || (currentJob ? (currentJob.config.location.city || currentJob.config.location.custom_location) : 'Surabaya')}</div>
              <div class="truncate"><span class="text-slate-500">Query:</span> ${w.current_query || '—'}</div>
              <div><span class="text-slate-500">Elapsed:</span> ${w.elapsed_time || '00:00'}</div>
            </div>
          </div>
        `);

      }

      container.innerHTML = cards.join('');
    }

    function renderRecordsTable() {
      const tbody = document.getElementById('records-table-body');
      const resp = currentRecordsResp;
      const records = resp.records || [];

      document.getElementById('records-counter-label').innerText = `Menampilkan ${records.length} dari ${resp.total.toLocaleString()} data (Halaman ${resp.page} dari ${resp.total_pages})`;
      document.getElementById('records-page-num').innerText = `Halaman ${resp.page} dari ${resp.total_pages}`;

      if (records.length === 0) {
        tbody.innerHTML = `<tr><td colspan="6" class="p-6 text-center text-slate-500">${resp.diagnostic || 'Belum ada data atau tidak ditemukan pencarian yang cocok.'}</td></tr>`;
        return;
      }

      tbody.innerHTML = records.map((r, i) => `
        <tr class="hover:bg-slate-100/50 border-b border-slate-200/40">
          <td class="p-3 font-bold text-slate-900">${r.Name}</td>
          <td class="p-3 text-slate-700 truncate max-w-[200px]">${r.Address}</td>
          <td class="p-3 text-slate-600">${r.City} / ${r.Kecamatan || '-'}</td>
          <td class="p-3 text-amber-600 font-bold">${r.Rating || '-'}</td>
          <td class="p-3 text-slate-700">${r.Phone || '-'}</td>
          <td class="p-3 text-right">
            <button onclick="showDetailModal(${i})" class="text-blue-600 hover:text-cyan-600">Detail</button>
          </td>
        </tr>
      `).join('');
    }

    function renderDashboardLatestRecords() {
      const tbody = document.getElementById('dashboard-latest-records');
      const latest = (currentRecordsResp.records || []).slice(0, 6);

      if (latest.length === 0) {
        tbody.innerHTML = `<tr><td colspan="3" class="p-3 text-center text-slate-500">Belum ada data terbaru.</td></tr>`;
        return;
      }

      tbody.innerHTML = latest.map(r => `
        <tr class="hover:bg-slate-100/40">
          <td class="p-2.5 font-bold text-slate-800 truncate max-w-[140px]">${r.Name}</td>
          <td class="p-2.5 text-slate-600">${r.City}</td>
          <td class="p-2.5 text-right font-bold text-amber-600">${r.Rating || '-'}</td>
        </tr>
      `).join('');
    }

    function changeRecordsPage(delta) {
      const maxPage = currentRecordsResp.total_pages || 1;
      currentPage = Math.max(1, Math.min(maxPage, currentPage + delta));
      fetchCurrentRecords();
    }

    function updateMapMarkers(records) {
      if (!map || !markersGroup) return;
      markersGroup.clearLayers();
      let count = 0;

      records.forEach(r => {
        const lat = parseFloat(r.Latitude);
        const lon = parseFloat(r.Longitude);
        if (!isNaN(lat) && !isNaN(lon) && lat !== 0 && lon !== 0) {
          const marker = L.marker([lat, lon]).bindPopup(`
            <div style="font-family: sans-serif; font-size: 12px;">
              <strong>${r.Name}</strong><br/>
              ${r.Address}<br/>
              Phone: ${r.Phone || 'N/A'}
            </div>
          `);
          markersGroup.addLayer(marker);
          count++;
        }
      });

      document.getElementById('map-counter').innerText = `${count} markers`;
      if (count > 0 && records[0].Latitude) {
        const firstLat = parseFloat(records[0].Latitude);
        const firstLon = parseFloat(records[0].Longitude);
        if (!isNaN(firstLat) && !isNaN(firstLon)) {
          map.setView([firstLat, firstLon], 12);
        }
      }
    }

    function showDetailModal(index) {
      const r = (currentRecordsResp.records || [])[index];
      if (!r) return;

      document.getElementById('modal-title').innerText = r.Name;
      document.getElementById('modal-body').innerHTML = `
        <div class="space-y-2">
          <div><strong class="text-slate-600 w-32 inline-block">ID Tempat:</strong> <span class="text-slate-900">${r.PlaceID}</span></div>
          <div><strong class="text-slate-600 w-32 inline-block">Nama Toko:</strong> <span class="text-slate-900">${r.Name}</span></div>
          <div><strong class="text-slate-600 w-32 inline-block">Alamat:</strong> <span class="text-slate-900">${r.Address}</span></div>
          <div><strong class="text-slate-600 w-32 inline-block">Kota / Kec.:</strong> <span class="text-slate-900">${r.City} / ${r.Kecamatan}</span></div>
          <div><strong class="text-slate-600 w-32 inline-block">Koordinat:</strong> <span class="text-slate-900">${r.Latitude}, ${r.Longitude}</span></div>
          <div><strong class="text-slate-600 w-32 inline-block">Rating:</strong> <span class="text-slate-900">${r.Rating}</span></div>
          <div><strong class="text-slate-600 w-32 inline-block">No. Telepon:</strong> <span class="text-slate-900">${r.Phone}</span></div>
          <div><strong class="text-slate-600 w-32 inline-block">Jam Buka:</strong> <span class="text-slate-900">${r.OpeningHours || '-'}</span></div>
          <div><strong class="text-slate-600 w-32 inline-block">Kategori:</strong> <span class="text-slate-900">${r.Types}</span></div>
          ${r.LinkSetinganTitik ? `<div class="pt-2"><a href="${r.LinkSetinganTitik}" target="_blank" class="text-blue-600 hover:underline">Buka di Google Maps &rarr;</a></div>` : ''}
        </div>
      `;
      document.getElementById('modal-detail').classList.remove('hidden');
    }

    function closeModal() {
      document.getElementById('modal-detail').classList.add('hidden');
    }

    function getStatusClass(status) {
      switch (status) {
        case 'RUNNING': return 'bg-blue-100 text-blue-700 border-blue-200';
        case 'COMPLETED': return 'bg-emerald-100 text-emerald-700 border-emerald-200';
        case 'PAUSED': return 'bg-amber-100 text-amber-700 border-amber-200';
        case 'STOPPED': return 'bg-rose-100 text-rose-700 border-rose-200';
        case 'FAILED': return 'bg-rose-100 text-rose-700 border-rose-200';
        default: return 'bg-slate-100 text-slate-700 border-slate-200';
      }
    }

    function getWorkerStatusClass(status) {
      switch (status) {
        case 'RUNNING': return 'bg-cyan-500/20 text-cyan-400';
        case 'COMPLETED': return 'bg-emerald-500/20 text-emerald-400';
        case 'FAILED': return 'bg-rose-500/20 text-rose-400';
        case 'PAUSED': return 'bg-amber-500/20 text-amber-400';
        default: return 'bg-slate-800 text-slate-400';
      }
    }

    async function handleCreateJob(e) {
      e.preventDefault();
      
      let queries = document.getElementById('input-queries').value.split('\n').filter(q => q.trim());
      if (queries.length === 0) {
          generateQueries();
          queries = document.getElementById('input-queries').value.split('\n').filter(q => q.trim());
      }
      
      const payload = {
        target: document.getElementById('input-target').value,
        location: {
          country: document.getElementById('select-country').value,
          province: document.getElementById('select-province').value,
          city: document.getElementById('select-city').value,
          district: document.getElementById('select-district').value,
          custom_location: document.getElementById('input-custom-location').value
        },
        queries: queries,
        target_records: parseInt(document.getElementById('input-target-records').value) || 1000,
        workers: parseInt(document.getElementById('input-workers').value) || 5,
        headless: document.getElementById('select-headless').value === 'true',
        settings: {
          validate_location: true,
          validate_category: true,
          remove_duplicates: true,
          skip_permanently_closed: false
        }
      };

      try {
        const res = await fetch('http://localhost:8080/api/jobs', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify(payload)
        });
        if (!res.ok) {
          const errText = await res.text();
          throw new Error(errText || 'Gagal membuat scraping job baru');
        }
        const jobState = await res.json();
        
        // Immediately switch active job context to newly created job
        currentJob = jobState;
        currentRecordsResp = { total: 0, page: 1, total_pages: 1, records: [] };
        currentPage = 1;

        // Clear terminal log for clean new job start
        const terminal = document.getElementById("log-terminal");
        if (terminal) {
          terminal.innerHTML = `<div class="text-blue-600 font-bold">[SYSTEM] Job baru dibuat: ${jobState.config.title || jobState.config.id}</div>`;
        }

        // Render immediately
        renderGlobalJobCard(jobState);
        render5WorkersCards(jobState.workers || [], jobState.config.workers || 5);
        if (typeof renderIterationUI === 'function') renderIterationUI(jobState);
        renderRecordsTable();
        updateHeaderStats();

        // Start the job
        await fetch(`http://localhost:8080/api/jobs/${jobState.config.id}/start`, {method: 'POST'});
        
        switchTab('dashboard');
        await refreshAllData();
      } catch (err) {
        alert("Error creating job: " + err.message);
      }
    }

    async function controlJob(jobId, action) {
      try {
        await fetch(`http://localhost:8080/api/jobs/${jobId}/${action}`, {method: 'POST'});
        refreshAllData();
      } catch (err) {}
    }

    function exportCurrentJob(format) {
      const jobId = currentJob ? currentJob.config.id : (jobs[0] ? jobs[0].config.id : 'default');
      window.open(`http://localhost:8080/api/jobs/${jobId}/export?format=${format}`);
    }

    function renderJobsTable() {
      const tbody = document.getElementById('jobs-table-body');
      if (jobs.length === 0) {
        tbody.innerHTML = `<tr><td colspan="6" class="p-4 text-center text-slate-500">Belum ada riwayat pencarian.</td></tr>`;
        return;
      }

      tbody.innerHTML = jobs.map(j => `
        <tr class="hover:bg-slate-100/40 border-b border-slate-200/40">
          <td class="p-3">
            <div class="font-semibold text-slate-800">${j.config.title}</div>
            <div class="text-[10px] text-slate-500 font-mono">${j.config.id}</div>
          </td>
          <td class="p-3 font-medium text-slate-800">${j.config.location.city || 'Kustom'}</td>
          <td class="p-3 font-mono">${j.config.queries.length} kata kunci</td>
          <td class="p-3 font-mono">${j.current_count} / ${j.target_count} (${j.progress.toFixed(0)}%)</td>
          <td class="p-3"><span class="px-2 py-0.5 rounded text-[10px] font-bold ${getStatusClass(j.status)}">${j.status}</span></td>
          <td class="p-3 text-right space-x-1.5 whitespace-nowrap">
            <button onclick="onSelectJob('${j.config.id}'); switchTab('dashboard');" class="bg-blue-600 hover:bg-blue-700 text-white font-medium px-2.5 py-1 rounded text-xs shadow-sm transition" title="Jadikan job aktif & buka di Dashboard untuk iterasi">Pilih Job</button>
            <button onclick="onSelectJob('${j.config.id}'); switchTab('records');" class="bg-white hover:bg-slate-100 text-blue-600 border border-slate-300 px-2.5 py-1 rounded text-xs shadow-sm">Lihat Data</button>
            <button onclick="window.open('http://localhost:8080/api/jobs/${j.config.id}/export?format=csv')" class="bg-emerald-50 hover:bg-emerald-100 text-emerald-700 border border-emerald-300 px-2.5 py-1 rounded text-xs font-semibold shadow-sm">Download CSV</button>
            <button onclick="showDeleteJobModal('${j.config.id}')" class="bg-rose-50 hover:bg-rose-100 text-rose-600 border border-rose-200 px-2.5 py-1 rounded text-xs font-semibold shadow-sm transition" title="Hapus riwayat job ini secara permanen">Hapus</button>
          </td>
        </tr>
      `).join('');
    }

    function showToast(message, type = 'success') {
      const container = document.getElementById('toast-container');
      if (!container) return;
      const toast = document.createElement('div');
      const isSuccess = type === 'success';
      const isError = type === 'error';
      const bg = isError ? 'bg-rose-600 text-white' : (isSuccess ? 'bg-slate-900 text-white' : 'bg-amber-600 text-white');
      const icon = isError ? 'alert-circle' : (isSuccess ? 'check-circle' : 'info');
      toast.className = `${bg} px-4 py-3 rounded-lg shadow-xl text-xs font-medium flex items-center gap-2.5 transition-all duration-300 transform translate-y-2 opacity-0 pointer-events-auto`;
      toast.innerHTML = `<i data-lucide="${icon}" class="w-4 h-4 shrink-0"></i><span>${message}</span>`;
      container.appendChild(toast);
      lucide.createIcons();
      setTimeout(() => {
        toast.classList.remove('translate-y-2', 'opacity-0');
      }, 10);
      setTimeout(() => {
        toast.classList.add('opacity-0', 'translate-y-2');
        setTimeout(() => toast.remove(), 300);
      }, 3500);
    }

    function showDeleteJobModal(jobId) {
      const job = jobs.find(j => j.config.id === jobId);
      if (!job) {
        showToast('Job tidak ditemukan.', 'error');
        return;
      }
      if (job.status === 'RUNNING') {
        showToast('Job sedang berjalan. Hentikan (Stop) job terlebih dahulu sebelum menghapus.', 'error');
        return;
      }

      document.getElementById('del-job-title').innerText = job.config.title || '-';
      document.getElementById('del-job-id').innerText = job.config.id || '-';
      document.getElementById('del-job-location').innerText = job.config.location.city || job.config.location.province || 'Kustom';
      document.getElementById('del-job-records').innerText = `${job.current_count || 0} toko`;
      document.getElementById('del-job-id-target').value = job.config.id;

      document.getElementById('modal-delete-job').classList.remove('hidden');
      lucide.createIcons();
    }

    function closeDeleteJobModal() {
      const modal = document.getElementById('modal-delete-job');
      if (modal) modal.classList.add('hidden');
    }

    async function confirmDeleteJob() {
      const targetId = document.getElementById('del-job-id-target').value;
      if (!targetId) return;

      const btn = document.getElementById('btn-confirm-delete-job');
      const originalText = btn.innerHTML;
      btn.disabled = true;
      btn.innerHTML = `<span>Menghapus...</span>`;

      try {
        const res = await fetch(`http://localhost:8080/api/jobs/${targetId}`, {
          method: 'DELETE'
        });

        if (res.ok) {
          closeDeleteJobModal();
          showToast(`Riwayat Job ${targetId} berhasil dihapus.`);

          // Update local jobs list immediately
          jobs = jobs.filter(j => j.config.id !== targetId);

          // If currentJob was the deleted job, pick another or null
          if (currentJob && currentJob.config.id === targetId) {
            if (jobs.length > 0) {
              await onSelectJob(jobs[0].config.id);
            } else {
              currentJob = null;
              currentRecordsResp = { total: 0, page: 1, total_pages: 1, records: [] };
              currentPage = 1;
              renderRecordsTable();
            }
          }

          await refreshAllData();
        } else {
          const errText = await res.text();
          showToast(`Gagal menghapus job: ${errText}`, 'error');
        }
      } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
      } finally {
        btn.disabled = false;
        btn.innerHTML = originalText;
      }
    }

    function showWipeAllModal() {
      const modal = document.getElementById('modal-wipe-all');
      if (modal) {
        modal.classList.remove('hidden');
        lucide.createIcons();
      }
    }

    function closeWipeAllModal() {
      const modal = document.getElementById('modal-wipe-all');
      if (modal) modal.classList.add('hidden');
    }

    async function wipeAllData() {
      showWipeAllModal();
    }

    async function confirmWipeAll() {
      const btn = document.getElementById('btn-confirm-wipe-all');
      const originalText = btn.innerHTML;
      btn.disabled = true;
      btn.innerHTML = `<span>Membersihkan seluruh data...</span>`;

      try {
        const res = await fetch('http://localhost:8080/api/system/wipe', { method: 'POST' });
        if (res.ok) {
          closeWipeAllModal();
          showToast('Seluruh data scraping dan riwayat job berhasil dibersihkan.');
          currentJob = null;
          currentRecordsResp = { total: 0, page: 1, total_pages: 1, records: [] };
          currentPage = 1;
          await refreshAllData();
        } else {
          showToast('Gagal menghapus seluruh data.', 'error');
        }
      } catch (err) {
        showToast('Terjadi kesalahan: ' + err.message, 'error');
      } finally {
        btn.disabled = false;
        btn.innerHTML = originalText;
      }
    }

    let sseConnection = null;

    function initSSE() {
      if (sseConnection) return;
      
      sseConnection = new EventSource('http://localhost:8080/api/events');
      
      sseConnection.onmessage = function(event) {
        try {
          const payload = JSON.parse(event.data);
          if (payload.type === 'log') {
            const terminal = document.getElementById('log-terminal');
            if (terminal.innerHTML.includes('[SYSTEM] Logs panel ready')) {
                terminal.innerHTML = ''; // clear placeholder
            }
            const isScrolledToBottom = terminal.scrollHeight - terminal.clientHeight <= terminal.scrollTop + 50;
            
            const d = payload.data || {};
            const ts = d.timestamp || d.Timestamp || '00:00:00';
            const lvl = (d.level || d.Level || 'INFO').toUpperCase();
            const msg = d.message || d.Message || '';

            const logLine = document.createElement('div');
            const color = lvl === 'ERROR' ? 'text-rose-400' : (lvl === 'WARN' ? 'text-amber-400' : 'text-slate-300');
            logLine.className = color;
            logLine.innerText = `[${ts}] [${lvl}] ${msg}`;
            terminal.appendChild(logLine);
            
            if (isScrolledToBottom) {
              terminal.scrollTop = terminal.scrollHeight;
            }
            
            while (terminal.childElementCount > 500) {
              terminal.removeChild(terminal.firstChild);
            }
          } else if (payload.type === 'state') {
            const state = payload.data;
            if (state && state.config) {
              const idx = jobs.findIndex(j => j.config.id === state.config.id);
              if (idx >= 0) {
                jobs[idx] = state;
              } else {
                jobs.unshift(state);
              }
              if (!currentJob || currentJob.config.id === state.config.id) {
                currentJob = state;
                renderGlobalJobCard(state);
                if (typeof renderIterationUI === 'function') renderIterationUI(state);
                render5WorkersCards(state.workers || [], state.config.workers || 5);
              }
              renderJobsTable();
            }
          } else if (payload.type === 'record') {
            if (currentJob && currentJob.config.id === payload.job_id) {
              currentJob.current_count = (currentJob.current_count || 0) + 1;
              const totalEl = document.getElementById('stat-total-records');
              const validEl = document.getElementById('stat-valid-records');
              if (totalEl) totalEl.innerText = currentJob.current_count.toLocaleString();
              if (validEl) validEl.innerText = currentJob.current_count.toLocaleString();
            }
          }
        } catch (e) {}
      };
      
      sseConnection.onerror = function() {
        console.error("SSE Connection Error. Attempting to reconnect...");
        if (sseConnection) {
          sseConnection.close();
          sseConnection = null;
        }
        setTimeout(initSSE, 5000);
      };
    }

    function setupCombobox(inputId, dropdownId, items, onSelectCallback) {
      const input = document.getElementById(inputId);
      const dropdown = document.getElementById(dropdownId);
      if (!input || !dropdown) return;

      const render = (filterText) => {
        const filtered = items.filter(i => i.toLowerCase().includes(filterText.toLowerCase()));
        if (filtered.length === 0) {
          dropdown.innerHTML = `<div class="p-2 text-slate-500 text-xs">No matches found</div>`;
          return;
        }
        dropdown.innerHTML = filtered.map(i => `<div class="p-2 cursor-pointer hover:bg-slate-700 text-slate-200 text-sm border-b border-slate-700/50 last:border-0 combobox-item">${i}</div>`).join('');
        
        dropdown.querySelectorAll('.combobox-item').forEach(el => {
          el.addEventListener('mousedown', (e) => {
            e.preventDefault(); // prevent blur
            input.value = el.innerText;
            dropdown.classList.add('hidden');
            if (onSelectCallback) onSelectCallback(el.innerText);
          });
        });
      };

      input.addEventListener('focus', () => {
        render(input.value);
        dropdown.classList.remove('hidden');
      });

      input.addEventListener('input', () => {
        render(input.value);
        dropdown.classList.remove('hidden');
      });

      input.addEventListener('blur', () => {
        setTimeout(() => dropdown.classList.add('hidden'), 150);
      });
    }

    async function initLocationDropdowns() {
      const provInput = document.getElementById('select-province');
      const cityInput = document.getElementById('select-city');
      const distInput = document.getElementById('select-district');
      
      let provinces = [];
      try {
        const res = await fetch('http://localhost:8080/api/locations/provinces');
        if (res.ok) provinces = await res.json();
      } catch (e) {}

      setupCombobox('select-province', 'dropdown-province', provinces, async (val) => {
        cityInput.value = '';
        distInput.value = '';
        cityInput.disabled = true;
        distInput.disabled = true;
        
        if (val) {
          cityInput.disabled = false;
          cityInput.placeholder = "Loading cities...";
          try {
            const res = await fetch(`http://localhost:8080/api/locations/cities?province=${encodeURIComponent(val)}`);
            if (res.ok) {
              const cities = await res.json();
              cityInput.placeholder = "Search city...";
              setupCombobox('select-city', 'dropdown-city', cities, async (cVal) => {
                distInput.value = '';
                distInput.disabled = true;
                
                if (cVal) {
                  distInput.disabled = false;
                  distInput.placeholder = "Loading districts...";
                  try {
                    const res = await fetch(`http://localhost:8080/api/locations/districts?city=${encodeURIComponent(cVal)}`);
                    if (res.ok) {
                      const dists = await res.json();
                      distInput.placeholder = "Search district...";
                      // Add 'All Districts' as the first option
                      const allDists = ["All Districts"].concat(dists || []);
                      setupCombobox('select-district', 'dropdown-district', allDists, null);
                    }
                  } catch (e) {}
                }
              });
            }
          } catch (e) {}
        }
      });
    }

    function generateQueries() {
      const target = document.getElementById('input-target').value.trim() || 'toko bangunan';
      const prov = document.getElementById('select-province').value.trim();
      const city = document.getElementById('select-city').value.trim();
      const dist = document.getElementById('select-district').value.trim();
      
      const baseQueries = target.split(',').map(s => s.trim()).filter(s => s);
      const generated = [];

      let suffix = '';
      if (dist && dist !== 'All Districts') suffix += ` in ${dist}`;
      if (city) suffix += (suffix ? `, ${city}` : ` in ${city}`);
      if (prov) suffix += (suffix ? `, ${prov}` : ` in ${prov}`);
      
      if (!suffix) suffix = ' in Indonesia';

      for (let base of baseQueries) {
        generated.push(`${base}${suffix}`);
        
        // Optional variations based on common terms
        if (base.toLowerCase() === 'toko bangunan') {
           generated.push(`toko material bangunan${suffix}`);
           generated.push(`toko semen${suffix}`);
           generated.push(`supplier bahan bangunan${suffix}`);
           generated.push(`building material store${suffix}`);
        }
      }
      
      document.getElementById('input-queries').value = generated.join('\n');
      }
      
      // Iteration Control Logic
      function toggleIterStrategy() {
        const strategy = document.getElementById('iter-strategy').value;
        if (strategy === 'query') {
          document.getElementById('iter-query-inputs').classList.remove('hidden');
          document.getElementById('iter-stores-inputs').classList.add('hidden');
        } else {
          document.getElementById('iter-query-inputs').classList.add('hidden');
          document.getElementById('iter-stores-inputs').classList.remove('hidden');
        }
      }

      function showStartIterationModal() {
        if (!currentJob) {
            alert("No active job selected.");
            return;
        }
        const targetInput = document.getElementById('iter-target-stores');
        if (targetInput) targetInput.value = currentJob.config.batch_size || 50;
        document.getElementById('modal-start-iteration').classList.remove('hidden');
      }

      async function submitStartIteration() {
        if (!currentJob) return;
        const targetStores = parseInt(document.getElementById('iter-target-stores').value) || (currentJob.config.batch_size || 50);
        const payload = {
            strategy: 'stores',
            target_stores: targetStores
        };
        
        try {
            const res = await fetch(`http://localhost:8080/api/jobs/${currentJob.config.id}/iterations/start`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });
            if (!res.ok) {
                const text = await res.text();
                alert(`Error starting iteration: ${text}`);
                return;
            }
            document.getElementById('modal-start-iteration').classList.add('hidden');
            refreshAllData();
        } catch (e) {
            alert(`Network error: ${e.message}`);
        }
      }

      async function pauseIteration(iterID) {
        if (!currentJob) return;
        try {
            await fetch(`http://localhost:8080/api/jobs/${currentJob.config.id}/iterations/${iterID}/pause`, { method: 'POST' });
            refreshAllData();
        } catch (e) {}
      }

      async function resumeIteration(iterID) {
        if (!currentJob) return;
        try {
            await fetch(`http://localhost:8080/api/jobs/${currentJob.config.id}/iterations/${iterID}/resume`, { method: 'POST' });
            refreshAllData();
        } catch (e) {}
      }
      
      async function stopIteration(iterID) {
        if (!currentJob) return;
        if (!confirm("Are you sure you want to stop this iteration?")) return;
        try {
            await fetch(`http://localhost:8080/api/jobs/${currentJob.config.id}/iterations/${iterID}/stop`, { method: 'POST' });
            refreshAllData();
        } catch (e) {}
      }

      async function retryIteration(iterID) {
        if (!currentJob) return;
        try {
            await fetch(`http://localhost:8080/api/jobs/${currentJob.config.id}/iterations/${iterID}/retry`, { method: 'POST' });
            refreshAllData();
        } catch (e) {}
      }
      
      function renderIterationUI(job) {
          const monitorContainer = document.getElementById('active-iteration-monitor');
          const timelineContainer = document.getElementById('iteration-timeline');
          
          if (!job.iterations || job.iterations.length === 0) {
              monitorContainer.innerHTML = '<div class="text-center py-6 text-slate-500 italic">Belum ada sesi yang berjalan.</div>';
              timelineContainer.innerHTML = '<div class="text-center py-6 text-[11px] text-slate-500 italic">Belum ada riwayat sesi.</div>';
              return;
          }
          
          let activeIter = null;
          let timelineHTML = '';
          
          // Reverse iterations for timeline (newest first)
          const sortedIters = [...job.iterations].reverse();
          
          sortedIters.forEach(it => {
              if (it.iteration_id === job.active_iteration_id && (it.status === 'RUNNING' || it.status === 'PAUSED')) {
                  activeIter = it;
              }
              
              let statusBadge = `<span class="px-2 py-0.5 rounded text-[10px] font-bold bg-slate-100 text-slate-500 border border-slate-200">${it.status}</span>`;
              if (it.status === 'RUNNING') statusBadge = `<span class="px-2 py-0.5 rounded text-[10px] font-bold bg-blue-100 text-blue-600 border border-blue-200">BERJALAN</span>`;
              if (it.status === 'COMPLETED') statusBadge = `<span class="px-2 py-0.5 rounded text-[10px] font-bold bg-emerald-100 text-emerald-600 border border-emerald-200">SELESAI</span>`;
              if (it.status === 'FAILED' || it.status === 'INTERRUPTED') statusBadge = `<span class="px-2 py-0.5 rounded text-[10px] font-bold bg-rose-100 text-rose-600 border border-rose-200">${it.status}</span>`;
              if (it.status === 'PAUSED') statusBadge = `<span class="px-2 py-0.5 rounded text-[10px] font-bold bg-amber-100 text-amber-600 border border-amber-200">JEDA</span>`;
              
              let pct = 0;
              if (it.target_queries > 0) pct = Math.min(100, (it.queries_processed / it.target_queries) * 100);
              else if (it.target_stores > 0) pct = Math.min(100, (it.stores_found / it.target_stores) * 100);
              else if (it.end_query > 0 && it.start_query > 0) {
                  let total = Math.abs(it.start_query - it.end_query) + 1;
                  pct = Math.min(100, (it.queries_processed / total) * 100);
              }
              
              let details = `Target: ${it.target_stores || 50} toko`;
              
              timelineHTML += `
              <div class="border-b border-slate-200 pb-3 mb-3 last:border-0 last:mb-0 last:pb-0">
                  <div class="flex justify-between items-center mb-1">
                      <div class="font-bold text-slate-800">Sesi ${it.iteration_number} <span class="text-slate-500 text-[10px] font-normal ml-1">(${details})</span></div>
                      ${statusBadge}
                  </div>
                  <div class="flex justify-between text-[10px] font-mono mb-1.5 text-slate-600">
                      <span>${it.queries_processed} kata kunci (${it.queries_success}✓ ${it.queries_failed}✗)</span>
                      <span>${it.stores_found} toko</span>
                  </div>
                  <div class="w-full bg-slate-200 h-1 rounded-full overflow-hidden">
                      <div class="bg-blue-600 h-full" style="width: ${pct}%"></div>
                  </div>
              </div>`;
          });
          
          timelineContainer.innerHTML = timelineHTML;
          
          if (activeIter) {
              const it = activeIter;
              let targetLabel = `${it.stores_found} / ${it.target_stores} toko`;
              
              let actionBtns = '';
              if (it.status === 'RUNNING') {
                  actionBtns = `
                      <button onclick="pauseIteration('${it.iteration_id}')" class="flex-1 bg-amber-600 hover:bg-amber-500 text-white text-xs font-semibold py-1.5 rounded transition">Jeda</button>
                      <button onclick="stopIteration('${it.iteration_id}')" class="flex-1 bg-rose-600 hover:bg-rose-500 text-white text-xs font-semibold py-1.5 rounded transition">Berhenti</button>
                  `;
              } else if (it.status === 'PAUSED') {
                  actionBtns = `
                      <button onclick="resumeIteration('${it.iteration_id}')" class="flex-1 bg-emerald-600 hover:bg-emerald-500 text-white text-xs font-semibold py-1.5 rounded transition">Lanjutkan</button>
                      <button onclick="stopIteration('${it.iteration_id}')" class="flex-1 bg-rose-600 hover:bg-rose-500 text-white text-xs font-semibold py-1.5 rounded transition">Berhenti</button>
                  `;
              }
              
              monitorContainer.innerHTML = `
                  <div class="flex items-center justify-between mb-3 border-b border-slate-200 pb-2">
                      <span class="text-blue-600 font-bold text-sm">Sesi ${it.iteration_number}</span>
                      <span class="bg-blue-50 text-blue-600 px-2 py-0.5 rounded text-[10px] font-mono font-bold border border-blue-200">AKTIF</span>
                  </div>
                  
                  <div class="grid grid-cols-2 gap-3 mb-4">
                      <div>
                          <div class="text-[10px] text-slate-500 font-medium">PROGRES</div>
                          <div class="font-mono text-slate-800">${targetLabel}</div>
                      </div>
                      <div>
                          <div class="text-[10px] text-slate-500 font-medium">TOKO DITEMUKAN</div>
                          <div class="font-mono text-slate-800 text-emerald-600">${it.stores_found}</div>
                      </div>
                      <div>
                          <div class="text-[10px] text-slate-500 font-medium">PENCARIAN GAGAL</div>
                          <div class="font-mono ${it.queries_failed > 0 ? 'text-rose-600' : 'text-slate-800'}">${it.queries_failed}</div>
                      </div>
                      <div>
                          <div class="text-[10px] text-slate-500 font-medium">MULAI PADA</div>
                          <div class="font-mono text-[10px] text-slate-600 mt-0.5 truncate" title="${it.started_at}">${it.started_at.split('T')[1] || it.started_at}</div>
                      </div>
                  </div>
                  
                  <div class="flex gap-2 mt-4 pt-4 border-t border-slate-200">
                      ${actionBtns}
                  </div>
              `;
          } else {
              // Maybe there is a failed or interrupted iteration
              let retryTarget = sortedIters.find(i => i.status === 'FAILED' || i.status === 'INTERRUPTED' || i.status === 'PARTIAL');
              if (retryTarget) {
                  monitorContainer.innerHTML = `
                      <div class="text-center py-4">
                          <i data-lucide="alert-triangle" class="w-8 h-8 text-amber-500 mx-auto mb-2"></i>
                          <div class="text-amber-600 font-bold mb-1">Sesi ${retryTarget.iteration_number} ${retryTarget.status}</div>
                          <div class="text-[10px] text-slate-600 mb-4">${retryTarget.queries_processed} kata kunci diproses.</div>
                          <button onclick="retryIteration('${retryTarget.iteration_id}')" class="bg-slate-700 hover:bg-slate-600 text-white text-xs px-4 py-1.5 rounded transition">Coba Lagi / Lanjutkan</button>
                      </div>
                  `;
                  lucide.createIcons();
              } else {
                  monitorContainer.innerHTML = '<div class="text-center py-6 text-slate-500 italic">Belum ada sesi yang berjalan.</div>';
              }
          }
      }
    </script>
    <script src="map.js"></script>
  </body>
</html>
