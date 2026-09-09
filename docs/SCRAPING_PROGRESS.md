# Scraping Progress

## Current Problem
Scraper artikel kesehatan Halodoc sebelumnya menghasilkan data yang tidak valid:
1. Halaman listing `https://www.halodoc.com/artikel` diperlakukan sebagai satu artikel tunggal, sehingga menghasilkan judul website `"Halodoc - Beli Obat, Tanya Dokter, Cek Lab Terpercaya"`, gambar kosong (`"-"`), dan description berisi teks menu navigasi/footer.
2. Fallback menggunakan data hardcoded katalog internal (`defaultHalodocCatalog()`) alih-alih mengambil artikel nyata dari trusted source.
3. Field `image` sebelumnya menyimpan path file lokal (`gambar/<title>.jpg`) atau tanda minus (`"-"`), bukan URL gambar asli dari CDN Halodoc.
4. Field `category` ditimpa oleh classifier heuristik generik, bukan mengambil kategori asli dari metadata/taxonomy artikel.
5. Field `description` dipotong/disintesis menjadi 1-3 paragraf saja, bukan transkrip lengkap isi artikel yang dibersihkan.
6. Schema belum menyertakan `source_url` artikel individual.

## Manual Scraping Flow
Alur manual yang menjadi source of truth:
1. Buka halaman trusted source: `https://www.halodoc.com/artikel`.
2. Temukan link artikel individual dari halaman listing (dari Angular TransferState `<script id="halodoc-state">` dan elemen link `<a>`).
3. Buka URL artikel individual secara terpisah (misal: `https://www.halodoc.com/artikel/dampak-abu-vulkanik-bagi-kesehatan-dan-cara-cegahnya`).
4. Ekstraksi data langsung dari halaman artikel individual:
   - **Title**: dari `<h1>` artikel utama / `og:title` / `<title>`.
   - **Image**: dari atribut `src` elemen `<img class="default-image-style">` atau `og:image`.
   - **Category**: dari `<meta property="article:tag">`, `halodoc-state` categories, atau label header artikel (contoh: `"ISPA , Infeksi Saluran Pernapasan"`).
   - **Description**: transkrip lengkap teks artikel dari `<div id="articleContent" class="article__content ql-editor">`, mempertahankan urutan paragraf, heading, list, tanpa ringkasan atau parafrase.
   - **Source URL**: URL spesifik artikel yang dikunjungi.
5. Validasi integritas seluruh field sebelum disimpan.

## Current Implementation
- Kode sebelumnya di `pkg/source/halodoc/adapter.go` hanya mencocokkan query string dengan slice statis `defaultHalodocCatalog()` dan menyintesis 1-3 paragraf.
- Jika query berupa URL listing `https://www.halodoc.com/artikel`, sistem gagal menemukan artikel nyata karena tidak ada mekanisme dynamic discovery dari listing page.
- Image diunduh ke disk lokal dan nilainya diisi path lokal.

## Root Cause
1. **Ketiadaan Discovery**: Tidak ada parser untuk menemukan link artikel individual dari halaman listing `https://www.halodoc.com/artikel`.
2. **Hardcoded Fallback**: Adapter mengandalkan mock catalog jika endpoint live gagal atau berbeda.
3. **Format Image Keliru**: Image disimpan sebagai path file lokal (`gambar/...`) bukan direct image URL (`https://.../img.jpg.webp`).
4. **Taxonomy Ignored**: Kategori asli artikel diabaikan dan ditimpa oleh rule classifier.
5. **Deskripsi Terpotong**: Logika cleaner membatasi deskripsi maksimal 3 paragraf.

## Changes Made
1. **Dynamic Article Discovery (`pkg/source/halodoc/adapter.go`)**:
   - Menambahkan method `DiscoverArticleURLs(listingHTML, baseURL)` yang mengekstrak slug artikel live dari Angular TransferState JSON (`<script id="halodoc-state">`) dan anchor links `<a href="/artikel/<slug>">`.
   - Menghapus ketergantungan pada katalog statis (`defaultHalodocCatalog()`).
2. **Individual Page Extraction (`ParseArticleHTMLWithContext`)**:
   - **Title**: Mengambil judul asli artikel dari `og:title` / `<h1>` / `<title>`, memotong branding suffix Halodoc.
   - **Image**: Mengambil direct remote CDN URL dari `<img class="default-image-style">` (`src`/`srcset`/`data-src`) atau `og:image`. Menolak path lokal `gambar/...`, placeholder, atau logo.
   - **Category**: Mengambil taksonomi asli dari seluruh tag `<meta property="article:tag">` dan menggabungkannya dengan delimiter `" , "`.
   - **Description**: Mengekstrak transkrip utuh dari container `.article__content`, mempertahankan urutan paragraf, heading, list, membersihkan script/iklan tanpa memotong jumlah paragraf.
   - **Source URL**: Menyertakan URL artikel individual yang valid.
3. **Data Model & Cleaning (`pkg/model/article.go` & `pkg/cleaner/cleaner.go`)**:
   - Menambahkan field `SourceURL string` dengan tag `json:"source_url,omitempty"`.
   - Menambahkan tipe `ArticleDescription []string` dengan custom JSON serializer fleksibel.
   - Menghapus batas artifisial 3 paragraf di `CleanParagraphs` dan `SplitAndCleanDescription` agar seluruh isi artikel tersimpan utuh.
4. **Verification & Storage (`pkg/crawler/storage.go`)**:
   - Memperbarui `VerifyArticlesJSON` untuk mendukung `source_url` dan deskripsi fleksibel.
5. **CLI Runner Integration (`cmd/crawler/main.go`)**:
   - Mendaftarkan `halodoc` adapter pada CLI runner dengan flag `-source` (opsi: `all`, `halodoc`, `kemenkes`, `medlineplus`).

## Extraction Strategy
- **Article URL**: Mendeteksi slug artikel dari Angular SSR TransferState (`<script id="halodoc-state">`) dan link tag `<a>` pada listing page `https://www.halodoc.com/artikel`.
- **Title**: Mengambil teks dari `<h1>`, fallback ke `og:title` dan `<title>`, dengan pembersihan branding suffix Halodoc.
- **Image**: Mengambil direct URL dari `<img class="default-image-style">` (`src`, `srcset`, `data-src`) atau `og:image`. Memastikan URL valid dan bukan logo/icon/avatar.
- **Category**: Mengambil seluruh tag dari `<meta property="article:tag">` atau `halodoc-state`, digabungkan dengan delimiter ` , `.
- **Description**: Mengekstraksi seluruh elemen `<p>`, heading `<h2>`-`<h4>`, dan `<li>` dari kontainer `id="articleContent"`, membersihkan script, iklan, dan promo, menghasilkan teks multi-paragraf lengkap.
- **Source URL**: Menyimpan URL artikel individual sebenarnya.

## Validation Rules
Artikel hanya valid dan disimpan jika memenuhi kriteria:
1. `title != ""` dan bukan judul listing/homepage Halodoc (`"Halodoc - Beli Obat, Tanya Dokter, Cek Lab Terpercaya"` ditolak).
2. `image != ""` dan berupa direct HTTP(S) image URL valid (bukan "-", bukan logo, bukan avatar).
3. `category != ""` (bukan nilai default kosong).
4. `description != ""` dengan panjang konten memadai (>= 100 karakter).
5. `source_url != ""` dan merupakan link artikel spesifik.

## Testing & Verification Results
Semua pengujian unit test dan end-to-end integrasi telah dijalankan dan lulus 100%:
- `go test -v ./pkg/source/halodoc/...`: **PASS**
- `go test -v ./pkg/source/kemenkes/...`: **PASS**
- `go test -v ./pkg/source/medlineplus/...`: **PASS**
- `go test -v ./pkg/api/...`: **PASS**
- `go test -v ./test/...`: **PASS** (Termasuk `TestE2E_GymArticleExtraction_ReferenceCase`, `TestE2E_TrustedSources_URLManagementAndCrawling`, `TestE2E_UI_FullFlowAndSchema`, `TestE2E_RequiredMedicalTopics`).

### Live Crawl Test: 5 Real Halodoc Articles
Eksekusi crawl live berhasil dijalankan dari trusted source `https://www.halodoc.com/artikel` dan disimpan ke `output/articles.json`:

1. **Artikel 1: Manfaat Luar Biasa Tanaman Paku Ekor Kuda Untuk Kesehatan**
   - **URL**: `https://www.halodoc.com/artikel/manfaat-luar-biasa-tanaman-paku-ekor-kuda-untuk-kesehatan`
   - **Image**: `https://d1vbn70lmn1nqe.cloudfront.net/prod/wp-content/uploads/2026/09/02102726/Tanaman-Herbal.png-6.jpg.webp`
   - **Category**: `Pharmacy`
   - **Description**: Teks bersih multi-paragraf lengkap (4.500+ karakter) mencakup manfaat, tips aman, kapan ke dokter, studi terkait, dan FAQ.
   - **Status**: **PASS**

2. **Artikel 2: Mengenal Keunikan Pohon Oak dan Manfaatnya Bagi Lingkungan**
   - **URL**: `https://www.halodoc.com/artikel/mengenal-keunikan-pohon-oak-dan-manfaatnya-bagi-lingkungan`
   - **Image**: `https://d1vbn70lmn1nqe.cloudfront.net/prod/wp-content/uploads/2026/09/02102750/Pohon.png-7.jpg.webp`
   - **Category**: `Pharmacy`
   - **Description**: Teks bersih multi-paragraf lengkap (4.000+ karakter) mencakup ekologi, cara perawatan, studi terkait, dan FAQ.
   - **Status**: **PASS**

3. **Artikel 3: Mengenal Pentingnya Padang Lamun bagi Ekosistem Laut Kita**
   - **URL**: `https://www.halodoc.com/artikel/mengenal-pentingnya-padang-lamun-bagi-ekosistem-laut-kita`
   - **Image**: `https://d1vbn70lmn1nqe.cloudfront.net/prod/wp-content/uploads/2026/09/02102659/Pelajaran-Biologi.png-21.jpg.webp`
   - **Category**: `Pharmacy`
   - **Description**: Teks bersih multi-paragraf lengkap (3.800+ karakter) mencakup peran bagi laut, cara menjaga, studi terkait, dan FAQ.
   - **Status**: **PASS**

4. **Artikel 4: Tips Menanam Pohon Sawo Agar Cepat Berbuah Lebat di Rumah**
   - **URL**: `https://www.halodoc.com/artikel/tips-menanam-pohon-sawo-agar-cepat-berbuah-lebat-di-rumah`
   - **Image**: `https://d1vbn70lmn1nqe.cloudfront.net/prod/wp-content/uploads/2026/09/02102712/Pohon.png-6.jpg.webp`
   - **Category**: `Nutrition`
   - **Description**: Teks bersih multi-paragraf lengkap (4.200+ karakter) mencakup cara menanam, panduan perawatan, dan tanya jawab seputar budidaya.
   - **Status**: **PASS**

5. **Artikel 5: Waspadai Berbagai Ciri Tumor Otak yang Perlu Diketahui**
   - **URL**: `https://www.halodoc.com/artikel/waspadai-berbagai-ciri-tumor-otak-yang-perlu-diketahui`
   - **Image**: `https://d1vbn70lmn1nqe.cloudfront.net/prod/wp-content/uploads/2026/09/02102738/Penyakit.jpg.webp`
   - **Category**: `Neurology`
   - **Description**: Teks bersih multi-paragraf lengkap (4.300+ karakter) mencakup ciri klinis tumor otak, penanganan darurat, studi medis, dan FAQ.
   - **Status**: **PASS**

## Status
Semua perbaikan dan fitur baru telah selesai diimplementasikan, diverifikasi dengan pengujian komprehensif, dan diverifikasi terhadap file output nyata `output/articles.json`.

---

## New Capabilities: UI Detail, Individual Deletion & Scraping Timestamp

### 1. Full Article Detail View (UI)
- **Modal Component**: Modal responsif (`#article-detail-modal`) yang dapat dibuka dengan mengklik card artikel, judul, atau tombol `[Detail]`.
- **Hero Image Banner**: Menampilkan gambar remote resolusi penuh dari CDN sumber dengan graceful fallback ke placeholder SVG jika gambar gagal dimuat (`handleDetailImageError`).
- **Full Transcripts**: Seluruh paragraf asli ditampilkan secara utuh dipisahkan rapi dalam container teks dengan formatting yang bersih tanpa adanya ringkasan atau parafrase AI.
- **Scraping Metadata**:
  - **Waktu Scraping (`scraped_at`)**: Diformat ke waktu lokal Indonesia/WIB (`7 September 2026, 10:35:42 WIB`) menggunakan `Intl.DateTimeFormat('id-ID', { timeZone: 'Asia/Jakarta' })`. Jika artikel legacy tidak memiliki timestamp, ditampilkan secara aman sebagai `"Not available"` (mencegah `"Invalid Date"`).
  - **URL Sumber Asli**: Link aktif (`target="_blank" rel="noopener noreferrer"`) yang langsung membuka halaman artikel individual di tab baru.
  - **Unique Article ID**: Badge identifier unik berbasis SHA256 hex digest (`art_...`).
- **Interactive Actions**:
  - Tombol `[Buka Sumber Asli]` untuk navigasi langsung ke halaman artikel asli.
  - Tombol `[Hapus Artikel]` untuk memicu dialog konfirmasi penghapusan artikel saat ini.
  - Tombol `[Tutup]` dan event listener `Escape` serta klik backdrop overlay.

### 2. Individual Article Deletion (Backend API & UI)
- **Backend API**:
  - `DELETE /api/articles/{id}` dan fallback `DELETE /api/articles?id={id}`.
  - Menemukan artikel berdasarkan ID unik (bukan judul semata untuk menghindari collision).
  - Menghapus record secara atomik dan thread-safe dari penyimpanan file (`SafeWriteFile`) dan state controller (`MedicalCrawlerController`).
  - Mengembalikan status HTTP 200 dengan payload `{"status":"deleted", "id": "...", "remaining": N}` jika berhasil, atau 404 jika ID tidak ditemukan.
  - **Data Safety Guard**: Hanya menghapus artikel yang ditargetkan tanpa menghapus artikel lain, tanpa mereset progress job crawling yang sedang berjalan, dan tanpa menghapus riwayat sesi.
- **Frontend Confirmation Dialog**:
  - Modal konfirmasi (`#delete-confirm-modal`) mencegah klik tidak sengaja.
  - Menampilkan judul artikel spesifik yang akan dihapus.
  - Opsi `[Batal]` dan `[Ya, Hapus Artikel]`.
- **In-Memory Reactive Update**:
  - Menghapus artikel dari memori client (`allArticles`) tanpa melakukan full reload seluruh halaman web.
  - Memperbarui grid tampilan secara seketika dan menampilkan toast notification sukses: `Artikel "[title]" berhasil dihapus.`.
  - Jika request API gagal, artikel tetap dipertahankan di UI dan memunculkan toast error.

### 3. Scraping Timestamp Storage (`scraped_at`)
- **Backend Capture**:
  - Dicatat otomatis menggunakan waktu server UTC format RFC3339 (`time.Now().UTC().Format(time.RFC3339)`) saat artikel berhasil diekstrak dan divalidasi (`halodoc.Adapter` dan `crawler.ArticleProcessor`).
- **Storage Persistence**:
  - Disimpan langsung ke dalam `output/articles.json` dengan key `"scraped_at"`.
  - Didukung penuh oleh model `model.Article`, `crawler.VerifyArticlesJSON`, dan seluruh suite pengujian integritas schema.
- **Backward Compatibility**:
  - Artikel lama yang belum memiliki `scraped_at` tetap valid, ditangani secara transparan, dan ditampilkan sebagai `"Not available"` di antarmuka pengguna tanpa error.

### 4. Article Sorting (Newest & Oldest)
- **Results Toolbar Dropdown**: Pilihan sort antara `Terbaru (Newest First)` sebagai default dan `Terlama (Oldest First)`.
- **Dual Sorting Support**:
  - Backend API: `GET /api/articles?sort=newest` dan `GET /api/articles?sort=oldest`.
  - Frontend Client: Sorting reaktif in-memory instan saat pengguna mengubah opsi dropdown.

---

## Final Verification Summary
- **Unit & Integration Tests**: 100% lulus di seluruh paket (`pkg/model`, `pkg/crawler`, `pkg/cleaner`, `pkg/api`, `pkg/source/halodoc`, `pkg/source/kemenkes`, `pkg/source/medlineplus`, `test/`).
- **Live Output Integrity**: Diverifikasi bahwa `output/articles.json` memuat artikel hasil scraping live Halodoc dengan 7 canonical fields: `id`, `title`, `image`, `category`, `description`, `source_url`, dan `scraped_at`.
- **Zero AI Paraphrasing**: Seluruh konten deskripsi adalah teks asli hasil ekstraksi DOM Halodoc tanpa peringkasan otomatis.

---

## Capabilities Added: Content Cleaning, All Topics Default & Cumulative Persistence Across Crawls

### 1. Article Content Cleaning
- **Daftar Isi / Table of Contents Removal**:
  - Dihilangkan secara menyeluruh pada level HTML DOM parsing regex (`reTOC` dan `reTOCDiv`) mencakup heading `DAFTAR ISI` / `Table of Contents`, seluruh anchor links `#h-...`, list `<ul>`, dan horizontal rule separator `<hr>`.
  - Leading TOC bullet points dibersihkan otomatis jika tersisa di awal konten.
- **Promo Halodoc Doctor Consultation**:
  - Blok promosi dokter ("Kenapa Harus Chat Dokter di Halodoc?", kartu biodata dokter, rating, biaya, SIP/STR, ketersediaan 24/7, tombol CTA konsultasi) disupresi menggunakan pendeteksi seksi stateful promo (`inPromoSection`).
  - Bagian medis esensial seperti **"Kapan Harus ke Dokter?"** dan **"Kapan Harus Menghubungi Dokter?"** diproteksi secara eksplisit (`isMedicalKeepHeading`) agar tidak terhapus.
- **Promo Apotek / Toko Kesehatan Halodoc**:
  - Blok promosi toko ("Kenapa Beli Obat di Halodoc?", "Toko Kesehatan Halodoc", "Apotek Online Terlengkap", CTA pembelian "Dapatkan ... di Toko Kesehatan Halodoc", "Tebus resep", pengiriman 1 jam, WhatsApp order) disupresi bersih.
- **Asisten AI Hilda**:
  - Seluruh heading dan teks promosi Hilda disaring tanpa kompromi.
- **Daftar Pustaka & Referensi**:
  - Heading referensi ("Referensi:", "Daftar Pustaka", "References") beserta teks sitasi akademis/jurnal (PubMed, WHO, Kemenkes, BPOM, "Diakses pada 202...") disupresi secara otomatis.
- **Preservasi Konten Medis Inti**:
  - Definisi penyakit, gejala, penyebab, diagnosis, opsi pengobatan, pencegahan, tips gaya hidup, dan kesimpulan tetap dipertahankan 100% utuh tanpa manipulasi AI.

### 2. Medical Topic / Category Default: "All Topics"
- **Default Input Field**: Nilai awal input topik di UI adalah `All Topics` dengan quick-pill selector `All Topics`.
- **Empty Query / General Scraping**:
  - Jika input topik dikosongkan atau bernilai `All Topics`, crawler tidak menerapkan filter kata kunci ke listing artikel, melainkan mengambil seluruh artikel yang ditemukan dari halaman listing sumber terpercaya.
  - Adapter `halodoc` mendukung query `all`, `all topics`, atau kosong tanpa melempar error validasi query.

### 3. Data Persistence Across Crawls (Cumulative Storage & Deduplication)
- **Cumulative Append**: Memulai crawl baru TIDAK PERNAH menghapus data hasil crawl sebelumnya. Artikel baru ditambahkan secara kumulatif ke dalam penyimpanan database `output/articles.json`.
- **Deduplication Priority**:
  1. Priority 1: Normalized `source_url` (case-insensitive, trailing slashes trimmed).
  2. Priority 2: Normalized `title` (case-insensitive, trimmed).
  Artikel duplikat dilewati secara otomatis tanpa menambah duplikasi ke file.
- **Strict 7-Field JSON Contract on Disk**:
  - Model disk `model.DiskArticle` memastikan `output/articles.json` hanya menyimpan maksimal 7 field canonical (`id`, `title`, `image`, `category`, `description`, `source_url`, `scraped_at`), menjamin 100% kompatibilitas dengan suite pengujian skema (`test/e2e_full_test.go`, `test/trusted_sources_e2e_test.go`).
- **Session History & Filter**:
  - Backend controller melacak riwayat sesi (`Crawl #1`, `Crawl #2`, dsb.) melalui `articleSessionMap` dan menyediakan endpoint `GET /api/crawl/sessions`.
  - Frontend UI menyediakan dropdown session filter (`All Crawls (N)`, `Crawl #1 (N)`, `Crawl #2 (N)`) di toolbar hasil crawl, dengan default `All Crawls`.
- **Selective Deletion**:
  - Menghapus satu artikel hanya menghapus artikel spesifik tersebut dari disk dan memori, tanpa mengganggu artikel lain atau sesi crawl lainnya.

