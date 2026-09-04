let scrapedMap = null;
let mapMarkers = null;
let allMapData = []; 
let currentMapJobId = 'all';

// Initialize map on Map tab
function renderScrapedMap() {
  if (!scrapedMap) {
    scrapedMap = L.map('scraped-map').setView([-2.5489, 118.0149], 5);
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '&copy; OpenStreetMap'
    }).addTo(scrapedMap);

    mapMarkers = L.markerClusterGroup({
      chunkedLoading: true,
      maxClusterRadius: 50
    });
    scrapedMap.addLayer(mapMarkers);

    setupFilterListeners();
  } else {
    setTimeout(() => scrapedMap.invalidateSize(), 200);
  }

  populateJobsDropdown();
}

async function populateJobsDropdown() {
  const select = document.getElementById('map-job-select');
  try {
    const res = await fetch('/api/jobs');
    const jobsData = await res.json();
    
    const currentVal = select.value;
    
    select.innerHTML = '<option value="all">All Jobs Combined</option>';
    if (jobsData && jobsData.length > 0) {
      jobsData.forEach(j => {
        select.innerHTML += `<option value="${j.config.id}">${j.config.name} (${j.config.id.substring(0,6)})</option>`;
      });
    }
    
    if (jobsData.find(j => j.config.id === currentVal) || currentVal === 'all') {
      select.value = currentVal;
    } else {
      select.value = 'all';
    }
    
    if(allMapData.length === 0) fetchMapData();
    
  } catch (err) {
    console.error("Failed to load jobs", err);
  }
}

async function fetchMapData() {
  const jobId = document.getElementById('map-job-select').value;
  const loading = document.getElementById('map-loading');
  const error = document.getElementById('map-error');
  
  loading.classList.remove('hidden');
  error.classList.add('hidden');
  
  try {
    let rawData = [];
    
    if (jobId === 'all') {
      const res = await fetch('/api/jobs');
      const jobsList = await res.json();
      
      for (const j of jobsList) {
        const dRes = await fetch(`/api/jobs/${j.config.id}/export?format=json`);
        if (dRes.ok) {
           const d = await dRes.json();
           if(d) rawData = rawData.concat(d);
        }
      }
    } else {
      const res = await fetch(`/api/jobs/${jobId}/export?format=json`);
      if (res.ok) {
        rawData = await res.json();
      } else {
        throw new Error("Failed to fetch job data");
      }
    }
    
    allMapData = rawData || [];
    populateFilters(allMapData);
    applyMapFilters();
    
  } catch (err) {
    console.error("Map Data Fetch Error", err);
    error.classList.remove('hidden');
  } finally {
    loading.classList.add('hidden');
  }
}

function parseJSONStringList(str) {
    if(!str) return [];
    try {
        let parsed = JSON.parse(str);
        if(Array.isArray(parsed)) return parsed;
    } catch(e) {}
    return str.split(',').map(s=>s.trim()).filter(s=>s);
}

function populateFilters(data) {
  const types = new Set();
  const provinces = new Set();
  
  data.forEach(item => {
    if (item.Types) {
       parseJSONStringList(item.Types).forEach(t => types.add(t));
    }
  });
  
  const cities = new Set();
  data.forEach(item => {
      if(item.City) cities.add(item.City);
  });
  
  const citySelect = document.getElementById('map-city');
  const currentCity = citySelect.value;
  citySelect.innerHTML = '<option value="">All Cities</option>' + 
    Array.from(cities).sort().map(c => `<option value="${c}">${c}</option>`).join('');
  citySelect.value = currentCity;
  citySelect.disabled = false;
  
  const provSelect = document.getElementById('map-province');
  provSelect.innerHTML = '<option value="">N/A (Not in dataset)</option>';
  provSelect.disabled = true;

  const typeSelect = document.getElementById('map-type');
  const currentType = typeSelect.value;
  typeSelect.innerHTML = '<option value="">All Types</option>' + 
    Array.from(types).sort().map(t => `<option value="${t}">${t}</option>`).join('');
  typeSelect.value = currentType;
}

function updateDistrictFilter() {
  const city = document.getElementById('map-city').value;
  const distSelect = document.getElementById('map-district');
  
  if (!city) {
    distSelect.innerHTML = '<option value="">All Kecamatan</option>';
    distSelect.disabled = true;
    return;
  }
  
  const districts = new Set();
  allMapData.forEach(item => {
    if (item.City === city && item.Kecamatan) {
      districts.add(item.Kecamatan);
    }
  });
  
  distSelect.innerHTML = '<option value="">All Kecamatan</option>' + 
    Array.from(districts).sort().map(d => `<option value="${d}">${d}</option>`).join('');
  distSelect.disabled = false;
}

function setupFilterListeners() {
  document.getElementById('map-job-select').addEventListener('change', fetchMapData);
  document.getElementById('map-city').addEventListener('change', () => {
    updateDistrictFilter();
    applyMapFilters();
  });
  document.getElementById('map-district').addEventListener('change', applyMapFilters);
  document.getElementById('map-type').addEventListener('change', applyMapFilters);
  document.getElementById('map-rating').addEventListener('change', applyMapFilters);
  
  let searchTimeout;
  document.getElementById('map-search').addEventListener('input', () => {
    clearTimeout(searchTimeout);
    searchTimeout = setTimeout(applyMapFilters, 300);
  });
}

function clearMapFilters() {
  document.getElementById('map-search').value = '';
  document.getElementById('map-city').value = '';
  document.getElementById('map-district').value = '';
  document.getElementById('map-type').value = '';
  document.getElementById('map-rating').value = '0';
  
  updateDistrictFilter();
  applyMapFilters();
}

function applyMapFilters() {
  const search = document.getElementById('map-search').value.toLowerCase();
  const city = document.getElementById('map-city').value;
  const dist = document.getElementById('map-district').value;
  const type = document.getElementById('map-type').value;
  const minRating = parseFloat(document.getElementById('map-rating').value) || 0;
  
  const filtered = allMapData.filter(item => {
    if (city && item.City !== city) return false;
    if (dist && item.Kecamatan !== dist) return false;
    
    if (minRating > 0) {
      const itemRating = parseFloat(item.Rating);
      if (isNaN(itemRating) || itemRating < minRating) return false;
    }
    
    if (type) {
      const itemTypes = parseJSONStringList(item.Types);
      if (!itemTypes.includes(type)) return false;
    }
    
    if (search) {
      const s = search;
      if (!(
        (item.Name && item.Name.toLowerCase().includes(s)) ||
        (item.Address && item.Address.toLowerCase().includes(s)) ||
        (item.PlaceID && item.PlaceID.toLowerCase().includes(s)) ||
        (item.Kecamatan && item.Kecamatan.toLowerCase().includes(s)) ||
        (item.City && item.City.toLowerCase().includes(s))
      )) {
        return false;
      }
    }
    
    return true;
  });
  
  renderMarkers(filtered);
}

function renderMarkers(data) {
  mapMarkers.clearLayers();
  
  let mappedCount = 0;
  let missingCoords = 0;
  let markers = [];
  
  data.forEach(item => {
    const lat = parseFloat(item.Latitude);
    const lng = parseFloat(item.Longitude);
    
    if (!isNaN(lat) && !isNaN(lng)) {
      
      const popupContent = `
        <div class="text-sm min-w-[200px]">
          <h4 class="font-bold text-slate-800 mb-1">${escapeHtml(item.Name)}</h4>
          <p class="text-slate-600 text-xs mb-2">${escapeHtml(item.Address || 'Address not available')}</p>
          
          <div class="grid grid-cols-2 gap-1 text-[11px] mb-3">
            <div class="text-slate-500">Rating:</div>
            <div class="font-medium">⭐ ${escapeHtml(item.Rating) || 'N/A'}</div>
            
            <div class="text-slate-500">Phone:</div>
            <div class="font-medium">${escapeHtml(item.Phone) || 'N/A'}</div>
            
            <div class="text-slate-500">City:</div>
            <div class="font-medium">${escapeHtml(item.City) || escapeHtml(item.Kecamatan) || 'N/A'}</div>
            
            <div class="text-slate-500">Hours:</div>
            <div class="font-medium">${item.OpeningHours ? 'Available' : 'N/A'}</div>
          </div>
          
          ${item.LinkSetinganTitik ? `<a href="${item.LinkSetinganTitik}" target="_blank" class="block w-full text-center bg-blue-600 text-white py-1.5 rounded text-xs font-semibold hover:bg-blue-700 transition">Open in Google Maps</a>` : ''}
        </div>
      `;
      
      const marker = L.marker([lat, lng]);
      marker.bindPopup(popupContent);
      markers.push(marker);
      mappedCount++;
    } else {
      missingCoords++;
    }
  });
  
  mapMarkers.addLayers(markers);
  
  document.getElementById('map-results-count').innerText = `${mappedCount} locations`;
  
  const emptyState = document.getElementById('map-empty');
  if (mappedCount === 0) {
    emptyState.classList.remove('hidden');
  } else {
    emptyState.classList.add('hidden');
    if (markers.length > 0) {
      scrapedMap.fitBounds(mapMarkers.getBounds(), { padding: [50, 50], maxZoom: 15 });
    }
  }
  
  const missingEl = document.getElementById('map-missing-coords');
  if (missingCoords > 0) {
    missingEl.innerText = `${missingCoords} records missing coordinates (not shown)`;
    missingEl.classList.remove('hidden');
  } else {
    missingEl.classList.add('hidden');
  }
}

function escapeHtml(unsafe) {
    if (!unsafe) return "";
    return (unsafe + "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#039;");
}
