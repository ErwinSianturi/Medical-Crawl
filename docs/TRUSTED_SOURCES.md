# Trusted Medical Sources

This document tracks approved and implemented trusted medical sources for the Medical Article Crawler, along with URL management specifications and canonical schema constraints.

---

## 1. Specification: Trusted Source Must Be a URL

In accordance with the latest system requirements:

1. **Trusted Sources MUST be full URLs**:
   - Users cannot enter plain site names (e.g., `Halodoc`, `halodoc.com`, `Kemkes`).
   - URLs must begin with `http://` or `https://` (e.g., `https://www.halodoc.com/artikel` or with query parameters like `https://www.halodoc.com/artikel?srsltid=AfmBOopyT-LxR0xJWEsdUpHKAmCeE8NYq6XR08QUzD12IMc1VrjZsgbG`).
   - Plain strings without `http://` or `https://` are rejected with the message:
     `"URL tidak valid. Harus diawali dengan http:// atau https:// (contoh: https://www.halodoc.com/artikel)"`
2. **Dynamic URL Management via UI**:
   - The UI includes a dedicated section to view active URLs, add new URLs with validation, and delete existing URLs.
   - Stored persistently in `data/trusted_sources.json`.
3. **No Hardcoded Sources**:
   - The crawler dynamically queries the registry (`data/trusted_sources.json`) at runtime.
   - Only active URLs currently listed in storage are crawled. If a URL is deleted by the user, it is never crawled.
4. **Canonical Article Model**:
   ```json
   {
     "title": "...",
     "image": "...",
     "category": "...",
     "description": [
       "Paragraf pertama...",
       "Paragraf kedua..."
     ]
   }
   ```
   - `description`: JSON array of strings containing between 1 and 3 paragraphs.
   - `image`: Valid HTTP/HTTPS URL or `"-"` fallback.

---

## Initial Default Sources

### Source #1: Halodoc Artikel
- **Default URL**: `https://www.halodoc.com/artikel`
- **Supported URLs**: Any Halodoc article or category URL (including URLs with query params such as `?srsltid=...`)
- **Access Method**: Direct HTTP Fetcher & Authoritative Medical Catalog Extraction (`pkg/source/halodoc/adapter.go`)
- **Title**: Extracted from `<h1>` or article headline
- **Description**: 1 to 3 non-empty paragraphs extracted from `<article>`, `<p>`, or structured content
- **Image**: Article feature image URL or `"-"`
- **Category**: Canonical classifier mapping (e.g. `Diabetes`, `Cancer`, `Heart Disease`, `Vaccination`, `General Health`)
- **Status**: IMPLEMENTED & VERIFIED

### Source #2: detikHealth
- **Default URL**: `https://health.detik.com/`
- **Supported URLs**: Any detikHealth article URL matching `/d-[0-9]+/` (e.g. `https://health.detik.com/infografis/d-8652432/infografis-efektivitas-pakai-2-masker-di-double-untuk-cegah-abu-vulkanik`)
- **Access Method**: Direct HTTP Fetcher & HTML Semantic Parsing (`pkg/source/detik/adapter.go`)
- **Discovery**: Crawls `/indeks?page=N`, category pages, and search index (`detik.com/search/searchall?query=...&siteid=55`)
- **Title**: Extracted from `<h1>`, `.detail__title`, `og:title`, or `<title>`
- **Description**: 1 to 3 non-empty paragraphs extracted from `div.detail__body-text.itp_bodycontent`, filtering out editorial notes, ads, captions, sisip links (`div.noncontent`), and author initials
- **Image**: Main article image from `figure.detail__media-image img`, `og:image`, or JSON-LD
- **Category**: Tags/keywords formatted as comma-separated list (e.g. `masker, double masking, abu vulkanik, perlindungan, kesehatan`)
- **Status**: IMPLEMENTED & VERIFIED

### Source #3: Kementerian Kesehatan RI (Ayo Sehat)
- **Default URL**: `https://ayosehat.kemkes.go.id/topik-az`
- **Supported URLs**: Ayo Sehat topic/category URLs (`/topik-penyakit/{kategori}/{slug}`, etc.)
- **Access Method**: Direct HTTP HTML Extraction & Topic Directory Indexing (`pkg/source/kemenkes/adapter.go`)
- **Title**: Extracted from `<h1>` or `<title>`
- **Description**: 1 to 3 substantive paragraphs extracted from article text
- **Image**: Extracted from `<meta property="og:image">` or `"-"`
- **Category**: Canonical bilingual classifier mapping
- **Status**: IMPLEMENTED & VERIFIED

### Optional Global Source: NIH MedlinePlus
- **URL**: `https://medlineplus.gov`
- **Access Method**: NLM Web Service XML Query (`pkg/source/medlineplus/adapter.go`)
- **Description**: 1 to 3 paragraphs extracted from topic summary
- **Status**: IMPLEMENTED & VERIFIED

---

## Persistence & Registry Architecture

- **Storage Location**: `data/trusted_sources.json`
- **Thread Safety**: Protected with `sync.RWMutex`
- **API Endpoints**:
  - `GET /api/sources`: Returns array of active trusted source items (`url`, `hostname`, `name`, `is_active`)
  - `POST /api/sources`: Validates scheme (`http://` or `https://`), checks duplicate, saves to disk, returns 201 Created
  - `DELETE /api/sources?url={encoded_url}`: Removes URL from disk, returns 200 OK


