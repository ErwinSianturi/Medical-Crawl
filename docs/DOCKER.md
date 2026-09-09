# Medical Article Crawler - Docker Documentation

Panduan komprehensif untuk menjalankan aplikasi **Medical Article Crawler** menggunakan Docker.

---

## 1. Requirements

Sebelum memulai, pastikan perangkat Anda telah terinstall:
- **Docker Engine** (atau Docker Desktop pada Windows/macOS)
- **Docker Compose** v2+ (sudah include di Docker Desktop / CLI modern `docker compose`)

---

## 2. Architecture & Persistent Volumes

Aplikasi berjalan dalam satu container mandiri yang mencakup:
- **Frontend**: Single-Page Application (HTML5/CSS/JS) di `web/`
- **Backend**: Go REST API server & Telemetry SSE di port `8080`
- **Crawler Engine**: Worker pool concurrent scraper (Halodoc, Detik Health, Kemenkes, MedlinePlus)
- **Scheduler**: In-process Daily CRON scheduler (otomatis berjalan pukul 01:00 WIB)

### Persistent Data Volumes
Data penting dipersistensikan secara aman menggunakan Docker Named Volumes:
- `medical_crawler_output`: Menyimpan file `articles.json` (`/app/output`)
- `medical_crawler_data`: Menyimpan histori run & timeline `runs.json`, `run_articles.json` (`/app/data`)
- `medical_crawler_images`: Menyimpan asset gambar (`/app/gambar`)

Data Anda **tidak akan hilang** ketika container dihentikan, dihapus, atau di-rebuild.

---

## 3. Quick Start

### 3.1 Build & Start Container
Jalankan perintah berikut di root folder project:

```bash
docker compose up -d --build
```

### 3.2 Akses Aplikasi
Buka browser dan navigasikan ke:
```text
http://localhost:8080
```

---

## 4. Useful Commands

### Cek Status Container
```bash
docker compose ps
```

### Melihat Live Logs
```bash
docker compose logs -f
```

### Menghentikan Aplikasi
```bash
docker compose down
```

### Restart Aplikasi
```bash
docker compose restart
```

### Masuk ke Dalam Shell Container (Debugging)
```bash
docker compose exec crawler sh
```

---

## 5. Port Configuration
Port default aplikasi adalah `8080`. Jika port `8080` di komputer Anda bentrok:
1. Buat file `.env` dari `.env.example`:
   ```bash
   cp .env.example .env
   ```
2. Ubah `PORT` ke port lain, misalnya `PORT=9000`.
3. Jalankan kembali:
   ```bash
   docker compose up -d
   ```
4. Akses melalui `http://localhost:9000`.

---

## 6. Daily Crawling & Scheduler
Aplikasi dilengkapi scheduler internal:
- **Jadwal default**: Pukul `01:00 AM WIB` setiap hari.
- **Manual Trigger**: Anda dapat memicu crawling CRON kapan saja melalui tombol **"Run Now"** pada kartu Scheduler di Web UI, atau via HTTP endpoint:
  ```bash
  curl -X POST http://localhost:8080/api/scheduler/trigger
  ```

---

## 7. Troubleshooting

| Masalah | Penyebab | Solusi |
|---|---|---|
| Port 8080 already in use | Port sedang digunakan service lain di komputer | Ubah `PORT=8081` pada file `.env` lalu restart docker compose |
| Container status Unhealthy | Server butuh waktu inisialisasi atau gagal start | Cek log dengan `docker compose logs crawler` |
| Scraper gagal koneksi | Koneksi internet host terputus | Pastikan koneksi internet aktif dan Docker memiliki akses network |
| Data hilang setelah down | Flag `-v` digunakan saat down (`docker compose down -v`) | Jangan gunakan flag `-v` saat mematikan container |
