package halodoc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"maps-scraper/pkg/model"
	"maps-scraper/pkg/source/halodoc"
)

func TestHalodoc_Search(t *testing.T) {
	adapter := halodoc.NewAdapter(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	topics := []string{"diabetes", "kanker", "imunisasi", "hipertensi"}

	for _, topic := range topics {
		articles, err := adapter.Search(ctx, topic)
		if err != nil {
			t.Fatalf("Search failed for topic %q: %v", topic, err)
		}

		if len(articles) == 0 {
			t.Errorf("Expected at least one article for topic %q, got 0", topic)
		}

		for i, art := range articles {
			validateHalodocArticleStrict(t, art, topic, i)
		}
	}
}

func TestHalodoc_ParseHTML(t *testing.T) {
	imgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("fake-jpeg-image-bytes-data-over-100-bytes-12345678901234567890123456789012345678901234567890123456789012345678901234567890"))
	}))
	defer imgServer.Close()

	adapter := halodoc.NewAdapter(imgServer.Client())

	rawHTML := fmt.Sprintf(`
		<html>
		<head>
			<title>Cara Efektif Mengontrol Gula Darah - Halodoc</title>
			<meta property="og:image" content="%s/diabetes-hero.jpg" />
			<meta property="article:tag" content="Diabetes" />
			<meta name="description" content="Mengontrol kadar gula darah sangat penting bagi penderita diabetes melitus." />
		</head>
		<body>
			<h1>Cara Efektif Mengontrol Gula Darah</h1>
			<p>Pola makan rendah karbohidrat sederhana membantu menstabilkan respon glukosa setelah makan.</p>
			<p>Aktivitas fisik ringan seperti jalan kaki selama 30 menit per hari meningkatkan sensitivitas insulin.</p>
			<p>Pemeriksaan rutin dengan dokter spesialis penyakit dalam membantu mendeteksi komplikasi lebih awal.</p>
			<p>Iklan: Dapatkan diskon obat diabetes sekarang.</p>
		</body>
		</html>
	`, imgServer.URL)

	art, err := adapter.ParseArticleHTML(rawHTML, "https://www.halodoc.com/artikel/gula-darah", "Cara Efektif Mengontrol Gula Darah")
	if err != nil {
		t.Fatalf("ParseArticleHTML failed: %v", err)
	}

	if art.Title != "Cara Efektif Mengontrol Gula Darah" {
		t.Errorf("Unexpected title: %q", art.Title)
	}

	expectedImage := fmt.Sprintf("%s/diabetes-hero.jpg", imgServer.URL)
	if art.Image != expectedImage {
		t.Errorf("Unexpected image: got %q, want %q", art.Image, expectedImage)
	}

	if art.Category != "Diabetes" {
		t.Errorf("Expected category 'Diabetes', got %q", art.Category)
	}

	if len(art.Description) < 1 {
		t.Errorf("Expected at least 1 paragraph, got %d", len(art.Description))
	}

	validateHalodocArticleStrict(t, art, "html-test", 0)
}

func TestHalodoc_GymArticleReferenceCase(t *testing.T) {
	imgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("fake-gym-image-bytes-data-over-100-bytes-12345678901234567890123456789012345678901234567890123456789012345678901234567890"))
	}))
	defer imgServer.Close()

	adapter := halodoc.NewAdapter(imgServer.Client())

	sampleURL := "https://www.halodoc.com/artikel/nge-gym-artinya-pahami-tujuan-sehatmu-sekarang?srsltid=AfmBOoo9lLhqhlCVc3ltAXaRKE6L5-Jzp8PTsTOGfBLDd0EZQtsf6sy7"
	sampleHTML := fmt.Sprintf(`
		<html>
		<head>
			<title>Nge Gym Artinya: Pahami Tujuan Sehatmu Sekarang</title>
			<meta property="og:title" content="Nge Gym Artinya: Pahami Tujuan Sehatmu Sekarang" />
			<meta property="og:image" content="%s/nge-gym-artinya.jpg" />
			<meta property="article:tag" content="Gym" />
		</head>
		<body>
			<div class="article__content ql-editor">
				<p>Nge-gym merupakan istilah populer di Indonesia yang merujuk pada aktivitas olahraga atau latihan fisik yang dilakukan di pusat kebugaran atau fitness center. Kegiatan ini bertujuan utama untuk meningkatkan kebugaran, membentuk otot, menjaga berat badan ideal, serta mendukung kesehatan secara menyeluruh.</p>
				<p>Di dalam pusat kebugaran, terdapat banyak jenis latihan yang dapat dilakukan mulai dari latihan kekuatan (strength training) seperti angkat beban, hingga latihan kardiovaskular seperti berlari di atas treadmill atau bersepeda statis.</p>
				<p>Memulai kegiatan gym dengan persiapan yang tepat adalah kunci keberhasilan jangka panjang bagi pemula agar terhindar dari cedera. Sangat disarankan untuk berkonsultasi dengan pelatih kebugaran di awal sesi.</p>
			</div>
			<div class="article-page__article-info"></div>
		</body>
		</html>
	`, imgServer.URL)

	art, err := adapter.ParseArticleHTML(sampleHTML, sampleURL, "")
	if err != nil {
		t.Fatalf("ParseArticleHTML failed: %v", err)
	}

	expectedTitle := "Nge-Gym Artinya: Pahami Tujuan Sehatmu Sekarang"
	if art.Title != expectedTitle {
		t.Errorf("Title: got %q, want %q", art.Title, expectedTitle)
	}

	expectedImage := fmt.Sprintf("%s/nge-gym-artinya.jpg", imgServer.URL)
	if art.Image != expectedImage {
		t.Errorf("Image: got %q, want %q", art.Image, expectedImage)
	}

	if art.Category != "Gym" {
		t.Errorf("Category: got %q, want 'Gym'", art.Category)
	}

	if len(art.Description) < 1 {
		t.Fatalf("Expected at least 1 paragraph, got %d", len(art.Description))
	}

	validateHalodocArticleStrict(t, art, "gym-reference", 0)
}

func TestHalodoc_MissingImageValidationRejection(t *testing.T) {
	adapter := halodoc.NewAdapter(nil)

	rawHTML := `
		<html>
		<head><title>Penyakit Alergi Dingin - Halodoc</title></head>
		<body>
			<h1>Penyakit Alergi Dingin</h1>
			<p>Alergi dingin atau cold urticaria adalah reaksi kulit terhadap suhu dingin yang menyebabkan bentol kemerahan dan gatal yang cukup mengganggu.</p>
		</body>
		</html>
	`

	// Per Step 10: Missing image must fail validation, do not save data!
	_, err := adapter.ParseArticleHTML(rawHTML, "https://www.halodoc.com/artikel/alergi", "Penyakit Alergi Dingin")
	if err == nil {
		t.Errorf("Expected validation error on missing image, got nil")
	}
}

func TestHalodoc_HTTPErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := halodoc.NewAdapter(server.Client())
	ctx := context.Background()

	// Empty query error
	_, err := adapter.Search(ctx, "")
	if err == nil {
		t.Errorf("expected error on empty query, got nil")
	}

	// Unknown query returns 0 articles without fatal error
	arts, err := adapter.Search(ctx, "nonexistent999999")
	if err != nil {
		t.Errorf("unexpected error on unknown query: %v", err)
	}
	if len(arts) != 0 {
		t.Errorf("expected 0 articles, got %d", len(arts))
	}
}

func validateHalodocArticleStrict(t *testing.T, art model.Article, topic string, idx int) {
	if strings.TrimSpace(art.Title) == "" {
		t.Errorf("[%s #%d] Title is empty", topic, idx)
	}
	if strings.TrimSpace(art.Category) == "" {
		t.Errorf("[%s #%d] Category is empty", topic, idx)
	}
	if strings.TrimSpace(art.Image) == "" || art.Image == "-" {
		t.Errorf("[%s #%d] Image is empty or placeholder: %q", topic, idx, art.Image)
	}
	if len(art.Description) < 1 {
		t.Errorf("[%s #%d] Description paragraph count is 0", topic, idx)
	}
	for pIdx, p := range art.Description {
		if strings.TrimSpace(p) == "" {
			t.Errorf("[%s #%d] Paragraph #%d is empty", topic, idx, pIdx+1)
		}
	}

	// Strict JSON verification: includes title, image, category, description, source_url
	data, err := json.Marshal(art)
	if err != nil {
		t.Errorf("[%s #%d] JSON marshal error: %v", topic, idx, err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Errorf("[%s #%d] JSON unmarshal error: %v", topic, idx, err)
	}

	for _, k := range []string{"title", "image", "category", "description", "source_url"} {
		if _, ok := m[k]; !ok {
			t.Errorf("[%s #%d] Missing required key %q in JSON output", topic, idx, k)
		}
	}
}

func TestHalodoc_ContentCleaning_TOC_Promo_References(t *testing.T) {
	adapter := halodoc.NewAdapter(nil)

	rawHTML := `
		<!DOCTYPE html>
		<html>
		<head>
			<title>Panduan Kesehatan Pencernaan dan Lambung</title>
			<meta property="og:title" content="Panduan Kesehatan Pencernaan dan Lambung" />
			<meta property="og:image" content="https://example.com/digestive.jpg" />
			<meta property="article:tag" content="Pencernaan" />
		</head>
		<body>
			<div class="article__content">
				<p class="wp-block-paragraph"><strong>DAFTAR ISI</strong></p>
				<p><a href="#h-gejala">Gejala Gangguan Lambung</a></p>
				<ul>
					<li><a href="#h-kapan-harus-ke-dokter">Kapan Harus ke Dokter?</a></li>
					<li><a href="#h-chat-dokter">Kenapa Harus Chat Dokter di Halodoc?</a></li>
					<li><a href="#h-beli-obat">Kenapa Harus Beli Obat di Halodoc?</a></li>
				</ul>
				<hr class="wp-block-separator" />

				<p>Gangguan asam lambung merupakan kondisi saat asam lambung mengalir kembali ke kerongkongan, memicu rasa tidak nyaman pada ulu hati.</p>
				
				<h2>Gejala Gangguan Lambung</h2>
				<p>Gejala umum meliputi heartburn, rasa asam di mulut, perut kembung, dan rasa cepat kenyang.</p>

				<h3>Kapan Harus ke Dokter?</h3>
				<p>Segera periksakan diri ke dokter spesialis gastroenterologi jika kamu mengalami muntah darah, feses berwarna hitam, sesak napas berat, atau kesulitan menelan makanan yang memburuk secara progresif.</p>

				<h3>Hubungi Dokter Ini untuk Konsultasi Gangguan Lambung</h3>
				<p>Untuk penanganan lebih akurat, kamu bisa berkonsultasi langsung dengan dokter spesialis di Halodoc:</p>
				<ul>
					<li>dr. Anton Wijaya, Sp.PD: Dokter spesialis penyakit dalam dengan pengalaman 15 tahun...</li>
					<li>dr. Sarah Amanda, Sp.GK: Dokter spesialis gizi klinik dengan pengalaman 10 tahun...</li>
				</ul>
				<p>Jadwalkan Sesi Konsultasi dengan dr. Anton Wijaya di Halodoc Mulai dari Rp 55.000,-.</p>
				<p>✅ Dokter tersedia 24 jam.</p>
				<p>Ayo, pakai Halodoc sekarang juga!</p>

				<h3>Bingung Harus Konsul ke Dokter Apa? Tanya HILDA, Gratis!</h3>
				<p>Kenalin HILDA (Halodoc Intelligent Digital Assistant), asisten AI pintar yang siap memandu kebutuhan kesehatanmu.</p>

				<h3>Kenapa Harus Beli Obat di Halodoc?</h3>
				<p>Kamu bisa mendapatkan obat dan suplemen dengan mudah di Toko Kesehatan Halodoc.</p>
				<p>• ✅ Tebus resep resmi dokter.</p>
				<p>• ✅ Obat diantar dalam 1 jam langsung ke rumah.</p>
				<p>Beli obat tinggal chat di WhatsApp resmi Halodoc.</p>

				<h6>Referensi:</h6>
				<p>World Health Organization. Diakses pada 2026. Digestive Health Guidelines.</p>
				<p>PubMed. Diakses pada 2026. Clinical Gastrointestinal Disorders.</p>
			</div>
			<div class="article-page__article-info"></div>
		</body>
		</html>
	`

	art, err := adapter.ParseArticleHTML(rawHTML, "https://www.halodoc.com/artikel/panduan-lambung", "")
	if err != nil {
		t.Fatalf("ParseArticleHTML failed: %v", err)
	}

	fullDesc := art.Description.String()

	// 1. Table of contents must be removed
	if strings.Contains(fullDesc, "DAFTAR ISI") {
		t.Errorf("DAFTAR ISI was not removed from description")
	}

	// 2. Halodoc Promotional content must be removed
	promoCheckPhrases := []string{
		"Kenapa Harus Chat Dokter",
		"dr. Anton Wijaya",
		"Jadwalkan Sesi Konsultasi",
		"Tanya HILDA",
		"Kenapa Harus Beli Obat",
		"Toko Kesehatan Halodoc",
		"Tebus resep",
		"Ayo, pakai Halodoc",
	}
	for _, phrase := range promoCheckPhrases {
		if strings.Contains(fullDesc, phrase) {
			t.Errorf("Promotional phrase %q was not removed from description", phrase)
		}
	}

	// 3. References must be removed
	if strings.Contains(fullDesc, "Referensi:") || strings.Contains(fullDesc, "Diakses pada 2026") {
		t.Errorf("References section was not removed from description")
	}

	// 4. Genuine medical keep heading and symptoms MUST be preserved!
	if !strings.Contains(fullDesc, "Kapan Harus ke Dokter?") {
		t.Errorf("Medical keep heading 'Kapan Harus ke Dokter?' was mistakenly removed!")
	}
	if !strings.Contains(fullDesc, "muntah darah, feses berwarna hitam") {
		t.Errorf("Medical warning symptoms text was mistakenly removed!")
	}
	if !strings.Contains(fullDesc, "Gangguan asam lambung merupakan kondisi") {
		t.Errorf("Article introductory content was mistakenly removed!")
	}
}

func TestHalodoc_AbuVulkanikArticleExtraction(t *testing.T) {
	adapter := halodoc.NewAdapter(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	targetURL := "https://www.halodoc.com/artikel/dampak-abu-vulkanik-bagi-kesehatan-dan-cara-cegahnya"
	art, err := adapter.FetchAndParse(ctx, targetURL, "")
	if err != nil {
		t.Fatalf("FetchAndParse failed for Abu Vulkanik article: %v", err)
	}

	if art.Title == "" || !strings.Contains(strings.ToLower(art.Title), "abu vulkanik") {
		t.Errorf("Expected title containing 'abu vulkanik', got: %q", art.Title)
	}
	if art.Image == "" || !strings.HasPrefix(art.Image, "http") {
		t.Errorf("Expected valid HTTP image URL, got: %q", art.Image)
	}
	if art.Category == "" {
		t.Errorf("Expected non-empty category, got empty")
	}
	if len(art.Description) < 1 {
		t.Errorf("Expected description paragraphs, got %d", len(art.Description))
	}
	if art.SourceURL != targetURL {
		t.Errorf("Expected SourceURL %q, got %q", targetURL, art.SourceURL)
	}
}

func TestHalodoc_MultiPageDiscovery(t *testing.T) {
	adapter := halodoc.NewAdapter(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Query more than 10 articles (e.g. 15) to verify pagination beyond old limit of 10
	articles, err := adapter.SearchWithLimit(ctx, "all", 15)
	if err != nil {
		t.Fatalf("SearchWithLimit failed: %v", err)
	}

	if len(articles) <= 10 {
		t.Errorf("Expected > 10 articles to verify multi-page discovery, got %d", len(articles))
	}

	for i, a := range articles {
		if a.Title == "" {
			t.Errorf("Article #%d has empty title", i)
		}
		if a.SourceURL == "" {
			t.Errorf("Article #%d has empty SourceURL", i)
		}
	}
}

func TestHalodoc_LoadMoreIncremental(t *testing.T) {
	adapter := halodoc.NewAdapter(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Target 60 articles (greater than the initial 43 batch)
	target := 60
	urls := adapter.FetchCMSArticleURLsPaginatedForTest(ctx, "", target)
	if len(urls) < target {
		t.Errorf("Expected at least %d articles from incremental load-more, got %d", target, len(urls))
	}

	// Verify all returned URLs are valid and unique
	seen := make(map[string]bool)
	for _, u := range urls {
		if seen[u] {
			t.Errorf("Duplicate URL detected from load more: %s", u)
		}
		seen[u] = true
		if !strings.HasPrefix(u, "https://www.halodoc.com/artikel/") {
			t.Errorf("Unexpected article URL structure: %s", u)
		}
	}
}

func TestHalodoc_TableOfContentsRemoval(t *testing.T) {
	adapter := halodoc.NewAdapter(nil)

	// Test 1 — Standard TOC
	t.Run("Test1_StandardTOC", func(t *testing.T) {
		rawHTML := `
			<html>
			<head><title>Test Artikel Standard TOC</title><meta property="og:image" content="https://example.com/img.jpg"></head>
			<body>
				<div class="article__content">
					<p><strong>DAFTAR ISI</strong></p>
					<ul>
						<li><a href="#h-rekomendasi">Rekomendasi</a></li>
						<li><a href="#h-tips">Tips</a></li>
					</ul>
					<hr>
					<p>Isi artikel sebenarnya yang valid dan informatif tentang kesehatan tubuh kita secara menyeluruh. Menjaga pola makan seimbang dan berolahraga secara teratur merupakan fondasi utama gaya hidup sehat.</p>
				</div>
			</body>
			</html>
		`
		art, err := adapter.ParseArticleHTML(rawHTML, "https://www.halodoc.com/artikel/test-standard-toc", "")
		if err != nil {
			t.Fatalf("ParseArticleHTML failed: %v", err)
		}
		fullDesc := art.Description.String()
		if strings.Contains(strings.ToLower(fullDesc), "daftar isi") {
			t.Errorf("expected 'daftar isi' to be removed, got: %s", fullDesc)
		}
		if strings.Contains(fullDesc, "• Rekomendasi") || strings.Contains(fullDesc, "• Tips") {
			t.Errorf("expected TOC bullet links to be removed, got: %s", fullDesc)
		}
		if !strings.Contains(fullDesc, "Isi artikel sebenarnya yang valid dan informatif") {
			t.Errorf("expected main article content to be preserved, got: %s", fullDesc)
		}
	})

	// Test 2 — Nested TOC
	t.Run("Test2_NestedTOC", func(t *testing.T) {
		rawHTML := `
			<html>
			<head><title>Test Artikel Nested TOC</title><meta property="og:image" content="https://example.com/img.jpg"></head>
			<body>
				<div class="article__content">
					<div class="table-of-contents">
						<p><strong>Daftar Isi</strong></p>
						<ul>
							<li><a href="#h-buah">Buah Segar</a>
								<ul>
									<li><a href="#h-apel">Apel</a></li>
									<li><a href="#h-jeruk">Jeruk</a></li>
								</ul>
							</li>
							<li><a href="#h-sayur">Sayur Sehat</a></li>
						</ul>
					</div>
					<h2>Buah Segar</h2>
					<p>Buah apel dan jeruk memiliki banyak kandungan vitamin yang bermanfaat untuk kebugaran fisik dan daya tahan tubuh sepanjang hari.</p>
				</div>
			</body>
			</html>
		`
		art, err := adapter.ParseArticleHTML(rawHTML, "https://www.halodoc.com/artikel/test-nested-toc", "")
		if err != nil {
			t.Fatalf("ParseArticleHTML failed: %v", err)
		}
		fullDesc := art.Description.String()
		if strings.Contains(strings.ToLower(fullDesc), "daftar isi") {
			t.Errorf("expected 'daftar isi' removed")
		}
		if strings.Contains(fullDesc, "• Apel") || strings.Contains(fullDesc, "• Jeruk") {
			t.Errorf("expected nested TOC bullets to be completely removed, got: %s", fullDesc)
		}
		if !strings.Contains(fullDesc, "Buah apel dan jeruk memiliki banyak kandungan vitamin") {
			t.Errorf("expected article content to remain intact, got: %s", fullDesc)
		}
	})

	// Test 3 — Different Capitalization & Nav Tag
	t.Run("Test3_DifferentCapitalizationAndNav", func(t *testing.T) {
		variations := []string{"DAFTAR ISI", "Daftar Isi", "daftar isi", "TABLE OF CONTENTS", "Table of Contents"}
		for _, v := range variations {
			rawHTML := fmt.Sprintf(`
				<html>
				<head><title>Test TOC Variations</title><meta property="og:image" content="https://example.com/img.jpg"></head>
				<body>
					<div class="article__content">
						<nav>
							<h3>%s</h3>
							<ul>
								<li><a href="#h-1">Bab Satu</a></li>
								<li><a href="#h-2">Bab Dua</a></li>
							</ul>
						</nav>
						<p>Penjelasan detail bab satu dan bab dua mengenai pencegahan penyakit menular di masyarakat melalui vaksinasi berkala dan menjaga sanitasi lingkungan dengan baik.</p>
					</div>
				</body>
				</html>
			`, v)
			art, err := adapter.ParseArticleHTML(rawHTML, "https://www.halodoc.com/artikel/test-toc-var", "")
			if err != nil {
				t.Fatalf("ParseArticleHTML failed for variation %q: %v", v, err)
			}
			fullDesc := art.Description.String()
			if strings.Contains(strings.ToLower(fullDesc), strings.ToLower(v)) {
				t.Errorf("expected variation %q to be removed, got: %s", v, fullDesc)
			}
			if strings.Contains(fullDesc, "Bab Satu") && strings.HasPrefix(fullDesc, "•") {
				t.Errorf("expected TOC links to be removed for variation %q", v)
			}
			if !strings.Contains(fullDesc, "Penjelasan detail bab satu dan bab dua") {
				t.Errorf("expected content preserved for variation %q", v)
			}
		}
	})

	// Test 4 — Valid Article List (Non-TOC) Must Be Preserved
	t.Run("Test4_ValidArticleListPreserved", func(t *testing.T) {
		rawHTML := `
			<html>
			<head><title>Test Valid List Article</title><meta property="og:image" content="https://example.com/img.jpg"></head>
			<body>
				<div class="article__content">
					<h2>Rekomendasi Buah</h2>
					<p>Buah memiliki berbagai manfaat untuk memenuhi nutrisi harian tubuh:</p>
					<ul>
						<li>Jambu biji kaya akan kandungan vitamin C alami.</li>
						<li>Jeruk mengandung antioksidan untuk daya tahan tubuh.</li>
					</ul>
					<p>Konsumsilah buah-buahan tersebut secara teratur sebagai bagian dari gaya hidup sehat.</p>
				</div>
			</body>
			</html>
		`
		art, err := adapter.ParseArticleHTML(rawHTML, "https://www.halodoc.com/artikel/test-valid-list", "")
		if err != nil {
			t.Fatalf("ParseArticleHTML failed: %v", err)
		}
		fullDesc := art.Description.String()
		if !strings.Contains(fullDesc, "Jambu biji kaya akan kandungan vitamin C alami") {
			t.Errorf("expected valid article list item 1 to be preserved, got: %s", fullDesc)
		}
		if !strings.Contains(fullDesc, "Jeruk mengandung antioksidan untuk daya tahan tubuh") {
			t.Errorf("expected valid article list item 2 to be preserved, got: %s", fullDesc)
		}
	})

	// Test 5 — Article Without TOC
	t.Run("Test5_ArticleWithoutTOC", func(t *testing.T) {
		rawHTML := `
			<html>
			<head><title>Test Clean Article Without TOC</title><meta property="og:image" content="https://example.com/img.jpg"></head>
			<body>
				<div class="article__content">
					<h2>Pentingnya Istirahat Cukup</h2>
					<p>Tidur berkualitas selama 7 hingga 8 jam setiap malam sangat penting untuk menjaga kekebalan tubuh dan fungsi kognitif otak.</p>
					<p>Kurang tidur kronis dapat memicu peningkatan hormon stres dan risiko penyakit metabolik.</p>
				</div>
			</body>
			</html>
		`
		art, err := adapter.ParseArticleHTML(rawHTML, "https://www.halodoc.com/artikel/test-no-toc", "")
		if err != nil {
			t.Fatalf("ParseArticleHTML failed: %v", err)
		}
		fullDesc := art.Description.String()
		if !strings.Contains(fullDesc, "Tidur berkualitas selama 7 hingga 8 jam") {
			t.Errorf("expected content preserved without modification")
		}
	})

	// Test 6 — Existing Article Cleanup Test (Simulating DB migration)
	t.Run("Test6_ExistingArticleCleanup", func(t *testing.T) {
		paragraphs := []string{
			"DAFTAR ISI",
			"• Rekomendasi Buah dari J",
			"• Tips Memilih Buah Segar",
			"Buah merupakan sumber vitamin dan mineral yang sangat baik untuk menjaga kesehatan organ tubuh manusia.",
			"Rekomendasi Buah dari J",
			"Beberapa buah berawalan huruf J antara lain jambu biji dan jeruk bali.",
			"Tips Memilih Buah Segar",
			"Pilihlah buah yang memiliki kulit segar tanpa bercak busuk dan memiliki aroma yang manis alami.",
		}
		cleaned := halodoc.CleanExistingArticleParagraphs(paragraphs)
		for _, p := range cleaned {
			if strings.EqualFold(p, "DAFTAR ISI") {
				t.Errorf("TOC title was not cleaned: %q", p)
			}
			if p == "• Rekomendasi Buah dari J" || p == "• Tips Memilih Buah Segar" {
				t.Errorf("TOC anchor remnant was not cleaned: %q", p)
			}
		}
		if len(cleaned) != 5 {
			t.Errorf("expected 5 paragraphs after cleaning, got %d: %v", len(cleaned), cleaned)
		}
	})
}



