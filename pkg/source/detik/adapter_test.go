package detik_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"maps-scraper/pkg/model"
	"maps-scraper/pkg/source/detik"
	"maps-scraper/pkg/source/halodoc"
)

// Sample Detik article HTML matching https://health.detik.com/infografis/d-8652432/infografis-efektivitas-pakai-2-masker-di-double-untuk-cegah-abu-vulkanik
const sampleDetikInfografisHTML = `<!DOCTYPE html>
<html lang="id-ID">
<head>
    <title>Infografis: Efektivitas Pakai 2 Masker Di-double untuk Cegah Abu Vulkanik - detikHealth</title>
    <meta name="description" content="Penggunaan dua masker atau double masking menjadi salah satu pilihan di tengah erupsi Gunung Anak Krakatau. Benarkah efektif untuk menangkal abu vulkanik?" />
    <meta property="og:title" content="Infografis: Efektivitas Pakai 2 Masker Di-double untuk Cegah Abu Vulkanik" />
    <meta property="og:image" content="https://awsimages.detik.net.id/api/wm/2026/09/07/penggunaan-doubel-masker-benarkah-efektif-menangkal-debu-vulkanik-1788766948939_169.jpeg?w=1200" />
    <meta name="keywords" content="masker,double masking,abu vulkanik,perlindungan,kesehatan,rsup persahabatan,agus dwi susanto,n95,gunung anak krakatau" />
    <script type="application/ld+json">
    {
        "@context": "https://schema.org",
        "@type": "NewsArticle",
        "headline": "Infografis: Efektivitas Pakai 2 Masker Di-double untuk Cegah Abu Vulkanik",
        "image": {
            "@type": "ImageObject",
            "url": "https://awsimages.detik.net.id/community/media/visual/2026/09/07/penggunaan-doubel-masker-benarkah-efektif-menangkal-debu-vulkanik-1788766948939_169.jpeg?w=1200"
        }
    }
    </script>
</head>
<body>
    <article class="detail">
        <div class="detail__header">
            <h2 class="detail__subtitle">Infografis</h2>
            <h1 class="detail__title">
                Infografis: Efektivitas Pakai 2 Masker Di-double untuk Cegah Abu Vulkanik
            </h1>
            <div class="detail__author">Suci Risanti Rahmadania - <span class="detail__label">detikHealth</span></div>
            <div class="detail__date">Senin, 07 Sep 2026 16:04 WIB</div>
        </div>

        <div class="detail__media">
            <figure class="detail__media-image">
                <img src="https://akcdn.detik.net.id/community/media/visual/2026/09/07/penggunaan-doubel-masker-benarkah-efektif-menangkal-debu-vulkanik-1788766948939.jpeg?w=700&q=90" alt="penggunaan doubel masker" title="penggunaan doubel masker" class="p_img_zoomin" />
                <figcaption class="detail__media-caption">Foto: Suci Risanti Rahmadania/detikHealth</figcaption>
            </figure>
        </div>

        <div class="detail__body itp_bodycontent_wrapper">
            <div class="detail__body-text itp_bodycontent">
                <p><strong>Jakarta</strong> - Harga masker mendadak melonjak di tengah erupsi Gunung Anak Krakatau dan sebaran abu vulkanik ke sejumlah wilayah. Kenaikan harga terutama terjadi pada masker jenis respirator seperti KN95 dan N95, sehingga masker biasa atau bedah banyak dipilih sebagai alternatif untuk mengurangi paparan abu vulkanik.</p>
                <p>Penggunaan dua masker atau double masking kemudian menjadi salah satu pilihan. Namun, seberapa efektif penggunaan dua masker untuk melindungi diri dari paparan abu vulkanik?</p>
                <p>Dokter paru dari RSUP Persahabatan, Prof Dr dr Agus Dwi Susanto, SpP(K), mengatakan penggunaan dua masker diperbolehkan untuk meningkatkan perlindungan dari paparan partikel. Meski demikian, belum ada penelitian yang secara spesifik mengukur seberapa besar peningkatan kemampuan filtrasi saat dua masker digunakan bersamaan.</p>
                <p>"Di-double boleh. Tapi memang di dalam literatur-literatur itu belum ada studi menunjukkan kalau di-double bisa memfiltrasi berapa persen," ujar Prof Agus dalam tayangan detikPagi, Senin (7/9/2026).</p>
                <p>Prof Agus menjelaskan masker bedah memiliki kemampuan filtrasi sekitar 50-60 persen, sedangkan N95 sekitar 95 persen. Menurutnya, menggunakan dua masker dapat meningkatkan perlindungan, tetapi besarnya peningkatan tersebut belum dapat dipastikan.</p>
                <table align="center" class="pic_artikel_sisip_table"><tbody><tr><td><div class="pic_artikel_sisip"><span>penggunaan doubel masker, benarkah efektif menangkal debu vulkanik? Foto: Suci Risanti Rahmadania/detikHealth</span></div></td></tr></tbody></table>
                <div class="noncontent"><table class="linksisip" width="100%"><tbody><tr><td><div class="lihatjg"><strong>Baca juga: </strong><a href="https://health.detik.com/berita-detikhealth/d-8651891/pakai-dua-masker">Pakai Dua Masker Dirangkap, Seberapa Efektif Tangkal Abu Vulkanik? Ini Kata Dokter Paru</a></div></td></tr></tbody></table></div>
                <p>Ia juga mengingatkan masker perlu diganti secara berkala, terutama setelah digunakan dalam waktu lama atau ketika sudah lembap akibat uap dari pernapasan.</p>
                <p>Sementara itu, masker kain memiliki kemampuan filtrasi yang jauh lebih rendah terhadap partikulat halus, yakni sekitar 10 persen. Penggunaan masker kain bersama masker medis memang dapat meningkatkan perlindungan dibandingkan masker kain saja, tetapi belum ada data yang menunjukkan secara pasti seberapa besar peningkatannya.</p>
                <p>Karena itu, jika ingin menggunakan metode double masking, Prof Agus lebih menyarankan dua masker bedah dibandingkan kombinasi masker kain dan masker medis.</p>
                <p>"Anda lebih baik masker bedah di-double dua saja itu lebih bagus, karena sudah jelas satu masker 50 persen. Kalau dua masker mungkin naik," pungkasnya.</p>
                <br> <strong>(suc/up)</strong>
            </div>
        </div>

        <div class="detail__body-tag mgt-16">
            <div class="nav">
                <a class="nav__item" dtr-evt="tag" dtr-sec="" dtr-act="tag" dtr-idx="1" dtr-ttl="masker" href="https://www.detik.com/tag/masker/">masker</a>
                <a class="nav__item" dtr-evt="tag" dtr-sec="" dtr-act="tag" dtr-idx="2" dtr-ttl="double masking" href="https://www.detik.com/tag/double-masking/">double masking</a>
                <a class="nav__item" dtr-evt="tag" dtr-sec="" dtr-act="tag" dtr-idx="3" dtr-ttl="abu vulkanik" href="https://www.detik.com/tag/abu-vulkanik/">abu vulkanik</a>
                <a class="nav__item" dtr-evt="tag" dtr-sec="" dtr-act="tag" dtr-idx="4" dtr-ttl="perlindungan" href="https://www.detik.com/tag/perlindungan/">perlindungan</a>
                <a class="nav__item" dtr-evt="tag" dtr-sec="" dtr-act="tag" dtr-idx="5" dtr-ttl="kesehatan" href="https://www.detik.com/tag/kesehatan/">kesehatan</a>
            </div>
        </div>
    </article>
</body>
</html>`

// TestDetik_SampleArticleExtraction tests the exact mandated example article extraction.
func TestDetik_SampleArticleExtraction(t *testing.T) {
	adapter := detik.NewAdapter(nil)
	pageURL := "https://health.detik.com/infografis/d-8652432/infografis-efektivitas-pakai-2-masker-di-double-untuk-cegah-abu-vulkanik"

	art, err := adapter.ParseArticleHTMLWithContext(context.Background(), sampleDetikInfografisHTML, pageURL, "")
	if err != nil {
		t.Fatalf("ParseArticleHTMLWithContext failed: %v", err)
	}

	// 1. Title verification
	expectedTitle := "Infografis: Efektivitas Pakai 2 Masker Di-double untuk Cegah Abu Vulkanik"
	if art.Title != expectedTitle {
		t.Errorf("Expected title %q, got %q", expectedTitle, art.Title)
	}

	// 2. Category verification (tags joined by ", ")
	expectedCategory := "masker, double masking, abu vulkanik, perlindungan, kesehatan"
	if art.Category != expectedCategory {
		t.Errorf("Expected category %q, got %q", expectedCategory, art.Category)
	}

	// 3. Image verification
	expectedImage := "https://akcdn.detik.net.id/community/media/visual/2026/09/07/penggunaan-doubel-masker-benarkah-efektif-menangkal-debu-vulkanik-1788766948939.jpeg?w=700&q=90"
	if art.Image != expectedImage {
		t.Errorf("Expected image %q, got %q", expectedImage, art.Image)
	}

	// 4. Content verification
	if len(art.Description) < 5 {
		t.Fatalf("Expected at least 5 paragraphs, got %d", len(art.Description))
	}

	firstPara := art.Description[0]
	if !strings.Contains(firstPara, "Harga masker mendadak melonjak") {
		t.Errorf("First paragraph does not contain expected start text: %q", firstPara)
	}

	// Verify no "Baca juga", author initial "(suc/up)", or caption noise leaked into description
	fullContent := strings.Join(art.Description, "\n\n")
	if strings.Contains(strings.ToLower(fullContent), "baca juga") {
		t.Errorf("Content contains leaked 'Baca juga' link")
	}
	if strings.Contains(fullContent, "(suc/up)") {
		t.Errorf("Content contains leaked author initial '(suc/up)'")
	}
	if strings.Contains(strings.ToLower(fullContent), "foto: suci risanti") {
		t.Errorf("Content contains leaked photo caption")
	}

	// 5. Schema verification
	validateDetikArticle(t, art, "infografis", 0)
}

// TestDetik_URLValidation tests article URL filtering.
func TestDetik_URLValidation(t *testing.T) {
	adapter := detik.NewAdapter(nil)

	validURLs := []string{
		"https://health.detik.com/infografis/d-8652432/infografis-efektivitas-pakai-2-masker-di-double-untuk-cegah-abu-vulkanik",
		"https://health.detik.com/berita-detikhealth/d-8651891/pakai-dua-masker-dirangkap-seberapa-efektif-tangkal-abu-vulkanik-ini-kata-dokter-paru",
		"https://health.detik.com/wellness-diet/d-8651234/tips-diet-sehat-pagi-hari",
		"https://health.detik.com/penyakit/d-7654321/gejala-diabetes-melitus",
	}

	for _, u := range validURLs {
		if !adapter.IsValidArticleURL(u) {
			t.Errorf("Expected URL %q to be VALID", u)
		}
	}

	invalidURLs := []string{
		"https://health.detik.com",
		"https://health.detik.com/",
		"https://health.detik.com/indeks",
		"https://health.detik.com/berita-detikhealth",
		"https://health.detik.com/wellness-diet",
		"https://health.detik.com/tag/masker/",
		"https://www.detik.com/search/searchall?query=masker",
		"https://finance.detik.com/berita-ekonomi-bisnis/d-8652077/airnav-abu-vulkanik",
		"https://news.detik.com/berita/d-8652313/paper-test",
		"https://www.google.com",
		"not-a-url",
	}

	for _, u := range invalidURLs {
		if adapter.IsValidArticleURL(u) {
			t.Errorf("Expected URL %q to be INVALID", u)
		}
	}
}

// TestDetik_ArticleDiscovery verifies extracting clean URLs from HTML with deduplication.
func TestDetik_ArticleDiscovery(t *testing.T) {
	adapter := detik.NewAdapter(nil)

	listingHTML := `
    <div>
        <a href="https://health.detik.com/berita-detikhealth/d-8652625/harga-melonjak-dan-susah-dicari?utm_source=facebook">Artikel 1</a>
        <a href="/berita-detikhealth/d-8652564/cerita-warga-alami-pembuluh-darah-pecah">Artikel 2</a>
        <a href="https://health.detik.com/berita-detikhealth/d-8652625/harga-melonjak-dan-susah-dicari?utm_source=twitter">Artikel 1 Dup</a>
        <a href="https://health.detik.com/tag/masker/">Tag Page</a>
        <a href="https://finance.detik.com/berita-ekonomi-bisnis/d-8652077/airnav">Finance Link</a>
        <a href="https://health.detik.com/infografis/d-8652432/infografis-efektivitas-pakai-2-masker">Artikel 3</a>
    </div>`

	urls := adapter.DiscoverArticleURLs(listingHTML, "https://health.detik.com")

	if len(urls) != 3 {
		t.Fatalf("Expected 3 unique valid articles discovered, got %d: %v", len(urls), urls)
	}

	for _, u := range urls {
		if strings.Contains(u, "utm_source") {
			t.Errorf("Discovered URL was not normalized: %q", u)
		}
		if !adapter.IsValidArticleURL(u) {
			t.Errorf("Discovered URL is invalid: %q", u)
		}
	}
}

// TestDetik_MultipleArticles verifies extraction from various article formats.
func TestDetik_MultipleArticles(t *testing.T) {
	adapter := detik.NewAdapter(nil)

	article2HTML := `<!DOCTYPE html>
<html>
<head>
    <title>Mengenal Gejala Diabetes Melitus - detikHealth</title>
    <meta property="og:image" content="https://awsimages.detik.net.id/visual/2026/09/01/diabetes.jpeg?w=650" />
    <meta name="keywords" content="diabetes, gula darah, insulin, pankreas" />
</head>
<body>
    <h1 class="detail__title">Mengenal Gejala Diabetes Melitus</h1>
    <div class="detail__body-text itp_bodycontent">
        <p>Diabetes melitus adalah penyakit metabolik kronis yang ditandai dengan peningkatan kadar glukosa darah di atas normal.</p>
        <p>Kondisi ini terjadi ketika pankreas tidak cukup menghasilkan insulin atau saat tubuh tidak dapat secara efektif menggunakan insulin yang dihasilkan.</p>
        <p>Pencegahan dapat dilakukan dengan menjaga pola makan bergizi seimbang, rutin berolahraga, dan mengontrol berat badan ideal.</p>
    </div>
</body>
</html>`

	art, err := adapter.ParseArticleHTMLWithContext(context.Background(), article2HTML, "https://health.detik.com/penyakit/d-8123456/mengenal-gejala-diabetes-melitus", "")
	if err != nil {
		t.Fatalf("ParseArticleHTMLWithContext failed: %v", err)
	}

	if art.Title != "Mengenal Gejala Diabetes Melitus" {
		t.Errorf("Expected title 'Mengenal Gejala Diabetes Melitus', got %q", art.Title)
	}
	if !strings.Contains(art.Category, "diabetes") {
		t.Errorf("Expected category to contain 'diabetes', got %q", art.Category)
	}
	if art.Image != "https://awsimages.detik.net.id/visual/2026/09/01/diabetes.jpeg?w=650" {
		t.Errorf("Expected image URL, got %q", art.Image)
	}
	if len(art.Description) != 3 {
		t.Errorf("Expected 3 paragraphs, got %d", len(art.Description))
	}
}

// TestDetik_HalodocRegression ensures adding detikHealth did not alter or break Halodoc adapter behavior.
func TestDetik_HalodocRegression(t *testing.T) {
	hAdapter := halodoc.NewAdapter(nil)

	sampleHalodocHTML := `<!DOCTYPE html>
<html>
<head>
    <title>Mengenal Gejala Kanker - Halodoc</title>
    <meta property="og:image" content="https://cdn.halodoc.com/kanker.jpg" />
    <meta property="article:tag" content="Kanker, Onkologi, Kesehatan" />
</head>
<body>
    <h1>Mengenal Gejala Kanker</h1>
    <div class="article__content">
        <p>Kanker merupakan penyakit yang disebabkan oleh pertumbuhan sel yang tidak terkendali dalam tubuh manusia.</p>
        <p>Pemeriksaan dini dan konsultasi dengan dokter spesialis onkologi sangat penting untuk keberhasilan pengobatan.</p>
    </div>
    <div class="article-page__article-info"></div>
</body>
</html>`

	art, err := hAdapter.ParseArticleHTMLWithContext(context.Background(), sampleHalodocHTML, "https://www.halodoc.com/artikel/mengenal-gejala-kanker", "")
	if err != nil {
		t.Fatalf("Halodoc parsing regression error: %v", err)
	}

	if art.Title != "Mengenal Gejala Kanker" {
		t.Errorf("Halodoc title regression: expected 'Mengenal Gejala Kanker', got %q", art.Title)
	}
	if art.Image != "https://cdn.halodoc.com/kanker.jpg" {
		t.Errorf("Halodoc image regression: expected 'https://cdn.halodoc.com/kanker.jpg', got %q", art.Image)
	}
	if len(art.Description) != 2 {
		t.Errorf("Halodoc description regression: expected 2 paragraphs, got %d", len(art.Description))
	}
}

func validateDetikArticle(t *testing.T, art model.Article, topic string, idx int) {
	if strings.TrimSpace(art.Title) == "" {
		t.Errorf("[Detik %s #%d] Title is empty", topic, idx)
	}
	if strings.TrimSpace(art.Category) == "" {
		t.Errorf("[Detik %s #%d] Category is empty", topic, idx)
	}
	if strings.TrimSpace(art.Image) == "" {
		t.Errorf("[Detik %s #%d] Image is empty", topic, idx)
	}
	if len(art.Description) == 0 {
		t.Errorf("[Detik %s #%d] Description is empty", topic, idx)
	}

	data, err := json.Marshal(art)
	if err != nil {
		t.Errorf("[Detik %s #%d] Marshal error: %v", topic, idx, err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Errorf("[Detik %s #%d] Unmarshal error: %v", topic, idx, err)
	}

	for _, k := range []string{"title", "image", "category", "description"} {
		if _, ok := m[k]; !ok {
			t.Errorf("[Detik %s #%d] Missing canonical key %q", topic, idx, k)
		}
	}
}
// TestDetik_MultiCardSectionExtraction validates that articles using the "multi-card"
// paginated structure (section-detail-1, section-detail-2, ...) are fully extracted.
// This tests the root cause of art_09840b5d5117 truncation.
func TestDetik_MultiCardSectionExtraction(t *testing.T) {
	adapter := detik.NewAdapter(nil)

	// Simulates the structure of the real article at:
	// https://health.detik.com/berita-detikhealth/d-8651891/pakai-dua-masker-dirangkap-...
	// with 2 sections — the second section contains the concluding paragraphs.
	multiCardHTML := `<!DOCTYPE html>
<html lang="id-ID">
<head>
    <title>Pakai Dua Masker Dirangkap, Seberapa Efektif Tangkal Abu Vulkanik? Ini Kata Dokter Paru - detikHealth</title>
    <meta property="og:title" content="Pakai Dua Masker Dirangkap, Seberapa Efektif Tangkal Abu Vulkanik? Ini Kata Dokter Paru" />
    <meta property="og:image" content="https://akcdn.detik.net.id/community/media/visual/2026/09/07/1316392299-1788758671519_169.jpeg?w=700&q=90" />
    <meta name="keywords" content="masker,abu vulkanik,double masking,kesehatan paru" />
</head>
<body>
    <article class="detail">
        <h1 class="detail__title">Pakai Dua Masker Dirangkap, Seberapa Efektif Tangkal Abu Vulkanik? Ini Kata Dokter Paru</h1>
        <div class="detail__body detail__body--skybanner itp_bodycontent_wrapper">
            <div class="detail__body-text itp_bodycontent">
                <section id="section-detail-1" class="multi-card show" data-page="1">
                    <p>Harga masker mendadak melonjak di tengah erupsi Gunung Anak Krakatau dan sebaran abu vulkanik ke sejumlah wilayah.</p>
                    <p>Penggunaan dua masker atau double masking kemudian menjadi salah satu pilihan.</p>
                    <div class="clearfix"></div>
                    <p>"Di-double boleh. Tapi memang di dalam literatur-literatur itu belum ada studi menunjukkan kalau di-double bisa memfiltrasi berapa persen," ujar Prof Agus.</p>
                    <div class="noncontent"><table class="linksisip"><tbody><tr><td><div class="lihatjg"><strong>Baca juga: </strong><a href="https://health.detik.com/berita-detikhealth/d-8652432/link">Related Article</a></div></td></tr></tbody></table></div>
                    <p>Prof Agus menjelaskan masker bedah memiliki kemampuan filtrasi sekitar 50-60 persen, sedangkan N95 sekitar 95 persen.</p>
                    <p>Ia juga mengingatkan masker perlu diganti secara berkala.</p>
                </section>
                <div class="clearfix"></div>
                <section id="section-detail-2" class="multi-card" data-page="2">
                    <p>Sementara itu, masker kain memiliki kemampuan filtrasi yang jauh lebih rendah terhadap partikulat halus, yakni sekitar 10 persen.</p>
                    <p>Karena itu, jika ingin menggunakan metode double masking, Prof Agus lebih menyarankan dua masker bedah dibandingkan kombinasi masker kain dan masker medis.</p>
                    <p>"Anda lebih baik masker bedah di-double dua saja itu lebih bagus, karena sudah jelas satu masker 50 persen. Kalau dua masker mungkin naik," pungkasnya.</p>
                    <br> <strong>(suc/up)</strong>
                </section>
            </div>
        </div>
        <div class="detail__body-tag mgt-16">
            <div class="nav">
                <a class="nav__item" dtr-evt="tag" dtr-ttl="masker" href="https://www.detik.com/tag/masker/">masker</a>
                <a class="nav__item" dtr-evt="tag" dtr-ttl="abu vulkanik" href="https://www.detik.com/tag/abu-vulkanik/">abu vulkanik</a>
            </div>
        </div>
    </article>
</body>
</html>`

	pageURL := "https://health.detik.com/berita-detikhealth/d-8651891/pakai-dua-masker-dirangkap-seberapa-efektif-tangkal-abu-vulkanik-ini-kata-dokter-paru"
	art, err := adapter.ParseArticleHTMLWithContext(context.Background(), multiCardHTML, pageURL, "")
	if err != nil {
		t.Fatalf("ParseArticleHTMLWithContext failed: %v", err)
	}

	fullContent := strings.Join(art.Description, "\n\n")

	// Section 1 content must be present
	if !strings.Contains(fullContent, "Harga masker mendadak melonjak") {
		t.Errorf("Missing section-1 first paragraph")
	}
	if !strings.Contains(fullContent, "Di-double boleh") {
		t.Errorf("Missing section-1 quote paragraph")
	}
	if !strings.Contains(fullContent, "masker bedah memiliki kemampuan filtrasi") {
		t.Errorf("Missing section-1 filtration paragraph")
	}

	// Section 2 content must be present (this was the truncated part)
	if !strings.Contains(fullContent, "masker kain memiliki kemampuan filtrasi yang jauh lebih rendah") {
		t.Errorf("Missing section-2 paragraph (article was truncated before fix)")
	}
	if !strings.Contains(fullContent, "Anda lebih baik masker bedah di-double dua saja") {
		t.Errorf("Missing section-2 conclusion quote (article was truncated before fix)")
	}

	// Author initials must be stripped
	if strings.Contains(fullContent, "(suc/up)") {
		t.Errorf("Author initials leaked into content")
	}

	// Baca juga must be stripped
	if strings.Contains(strings.ToLower(fullContent), "baca juga") {
		t.Errorf("'Baca juga' link leaked into content")
	}

	// Validate paragraph count — should capture all 8 substantive paragraphs
	if len(art.Description) < 7 {
		t.Errorf("Expected at least 7 paragraphs from multi-card article, got %d: %v", len(art.Description), art.Description)
	}

	t.Logf("Multi-card extraction OK: %d paragraphs, %d chars", len(art.Description), len(fullContent))
}

// TestDetik_Case1_RelatedVideoStripping tests Case 1: Article with related video widget (aevp/pip-vid).
// Expected: The Video title and widget content are excluded from article description.
func TestDetik_Case1_RelatedVideoStripping(t *testing.T) {
	adapter := detik.NewAdapter(nil)

	htmlWithVideo := `<!DOCTYPE html>
<html>
<head>
    <title>Pameran Alkes Hospital Expo 2026 Dibuka - detikHealth</title>
    <meta property="og:title" content="Pameran Alkes Hospital Expo 2026 Dibuka" />
    <meta property="og:image" content="https://awsimages.detik.net.id/visual/2026/09/01/expo.jpeg?w=650" />
    <meta name="keywords" content="hospital expo, alkes, kesehatan" />
</head>
<body>
    <h1 class="detail__title">Pameran Alkes Hospital Expo 2026 Dibuka</h1>
    <div class="detail__body-text itp_bodycontent">
        <p>Selain itu, untuk menunjang kenyamanan pengunjung, area pameran juga dilengkapi dengan food court, dan toko merchandise resmi Hospital Expo.</p>
        <p>"Untuk berkunjung ke pameran Hospital Expo, masyarakat dapat melakukan registrasi melalui tautan https://tinyurl.com/reghospex2026 serta membayar tiket masuk sebesar Rp10.000," pungkas Yudha.</p>
        <div class="aevp">
            <div class="aevp__header">
                <h3 class="aevp__title"><a class="aevp_title" href="https://20.detik.com/video">Video BPOM RI Buka Suara soal Standar FDA untuk Alkes Impor AS</a></h3>
            </div>
            <div class="pip-vid sisip_video_ds ratiobox ratiobox--16-9">
                <span class="pip-vid__text"><h3 class="pip-vid__title"><a class="aevp_title" href="https://20.detik.com/video">Video BPOM RI Buka Suara soal Standar FDA untuk Alkes Impor AS</a></h3></span>
            </div>
        </div>
    </div>
</body>
</html>`

	pageURL := "https://health.detik.com/berita-detikhealth/d-8999999/pameran-alkes-hospital-expo-2026-dibuka"
	art, err := adapter.ParseArticleHTMLWithContext(context.Background(), htmlWithVideo, pageURL, "")
	if err != nil {
		t.Fatalf("ParseArticleHTMLWithContext failed: %v", err)
	}

	fullText := strings.Join(art.Description, "\n\n")

	if !strings.Contains(fullText, "Selain itu, untuk menunjang kenyamanan pengunjung") {
		t.Errorf("Missing valid first paragraph")
	}
	if !strings.Contains(fullText, "pungkas Yudha") {
		t.Errorf("Missing valid conclusion paragraph")
	}

	if strings.Contains(fullText, "Video BPOM RI Buka Suara") {
		t.Errorf("FAILED: Related video title leaked into article content: %q", fullText)
	}
	if len(art.Description) != 2 {
		t.Errorf("Expected exactly 2 paragraphs, got %d: %v", len(art.Description), art.Description)
	}
}

// TestDetik_Case2_ValidArticleUnchanged tests Case 2: Valid article without related video.
// Expected: Article content remains unchanged.
func TestDetik_Case2_ValidArticleUnchanged(t *testing.T) {
	adapter := detik.NewAdapter(nil)

	htmlClean := `<!DOCTYPE html>
<html>
<head>
    <title>Pentingnya Olahraga Rutin - detikHealth</title>
    <meta property="og:image" content="https://awsimages.detik.net.id/visual/2026/09/01/olahraga.jpeg?w=650" />
    <meta name="keywords" content="olahraga, kesehatan" />
</head>
<body>
    <h1 class="detail__title">Pentingnya Olahraga Rutin</h1>
    <div class="detail__body-text itp_bodycontent">
        <p>Olahraga rutin setiap hari dapat menjaga kebugaran jasmani dan rohani.</p>
        <p>Aktivitas fisik setidaknya 30 menit sehari dapat memperkuat sistem kekebalan tubuh.</p>
    </div>
</body>
</html>`

	art, err := adapter.ParseArticleHTMLWithContext(context.Background(), htmlClean, "https://health.detik.com/berita-detikhealth/d-8888888/pentingnya-olahraga-rutin", "")
	if err != nil {
		t.Fatalf("ParseArticleHTMLWithContext failed: %v", err)
	}

	if len(art.Description) != 2 {
		t.Errorf("Expected 2 paragraphs, got %d", len(art.Description))
	}
	if art.Description[0] != "Olahraga rutin setiap hari dapat menjaga kebugaran jasmani dan rohani." {
		t.Errorf("Paragraph 0 altered: %q", art.Description[0])
	}
}

// TestDetik_Case3_LegitimateVideoMention tests Case 3: Article legitimately discussing a video in body text.
// Expected: Word "Video" and sentence are preserved.
func TestDetik_Case3_LegitimateVideoMention(t *testing.T) {
	adapter := detik.NewAdapter(nil)

	htmlWithVideoMention := `<!DOCTYPE html>
<html>
<head>
    <title>Dokter Tanggapi Video Viral di Sosmed - detikHealth</title>
    <meta property="og:image" content="https://awsimages.detik.net.id/visual/2026/09/01/viral.jpeg?w=650" />
    <meta name="keywords" content="video, viral, kesehatan" />
</head>
<body>
    <h1 class="detail__title">Dokter Tanggapi Video Viral di Sosmed</h1>
    <div class="detail__body-text itp_bodycontent">
        <p>Video anak sekolah yang makan di warung makan menarik perhatian di media sosial.</p>
        <p>Dokter spesialis gizi menjelaskan bahwa rekaman video tersebut memperlihatkan pentingnya edukasi porsi gizi seimbang sejak dini.</p>
    </div>
</body>
</html>`

	art, err := adapter.ParseArticleHTMLWithContext(context.Background(), htmlWithVideoMention, "https://health.detik.com/berita-detikhealth/d-8777777/dokter-tanggapi-video-viral", "")
	if err != nil {
		t.Fatalf("ParseArticleHTMLWithContext failed: %v", err)
	}

	if len(art.Description) != 2 {
		t.Errorf("Expected 2 paragraphs, got %d", len(art.Description))
	}
	if !strings.HasPrefix(art.Description[0], "Video anak sekolah") {
		t.Errorf("Legitimate video paragraph was erroneously deleted: %q", art.Description[0])
	}
}

// TestDetik_Case4_MultipleRecommendationSections tests Case 4: Multiple recommendation sections after article body.
// Expected: All recommendation sections are excluded.
func TestDetik_Case4_MultipleRecommendationSections(t *testing.T) {
	adapter := detik.NewAdapter(nil)

	htmlMultipleRecs := `<!DOCTYPE html>
<html>
<head>
    <title>Manfaat Minum Air Putih - detikHealth</title>
    <meta property="og:image" content="https://awsimages.detik.net.id/visual/2026/09/01/air.jpeg?w=650" />
    <meta name="keywords" content="air putih, kesehatan" />
</head>
<body>
    <h1 class="detail__title">Manfaat Minum Air Putih</h1>
    <div class="detail__body-text itp_bodycontent">
        <p>Minum air putih secukupnya sangat penting untuk menjaga hidrasi tubuh sepanjang hari.</p>
        <p>Para ahli menyarankan konsumsi 8 gelas air setiap hari," kata dr Andi.</p>
        <div class="cb-berita-terkait">
            <h3>Berita Terkait</h3>
            <p>Tips Menjaga Ginjal Tetap Sehat</p>
        </div>
        <div class="cb-artikel-lainnya">
            <h3>Artikel Lainnya</h3>
            <p>Bahaya Kurang Minum Air</p>
        </div>
        <div class="advertisement">
            <p>ADVERTISEMENT</p>
        </div>
        <div class="koleksi-wrap">
            <p>Latest News Headline</p>
        </div>
    </div>
</body>
</html>`

	art, err := adapter.ParseArticleHTMLWithContext(context.Background(), htmlMultipleRecs, "https://health.detik.com/berita-detikhealth/d-8666666/manfaat-minum-air-putih", "")
	if err != nil {
		t.Fatalf("ParseArticleHTMLWithContext failed: %v", err)
	}

	fullText := strings.Join(art.Description, "\n\n")

	if strings.Contains(fullText, "Berita Terkait") || strings.Contains(fullText, "Bahaya Kurang Minum") ||
		strings.Contains(fullText, "ADVERTISEMENT") || strings.Contains(fullText, "Latest News Headline") {
		t.Errorf("FAILED: Recommendation sections leaked into article description: %q", fullText)
	}

	if len(art.Description) != 2 {
		t.Errorf("Expected exactly 2 paragraphs, got %d: %v", len(art.Description), art.Description)
	}
}

// TestDetik_TableOfContentsStripping tests stripping of collapsible "Daftar Isi" widgets.
func TestDetik_TableOfContentsStripping(t *testing.T) {
	adapter := detik.NewAdapter(nil)

	htmlWithTOC := `<!DOCTYPE html>
<html>
<head>
    <title>Kemenkes Siagakan 413 RS - detikHealth</title>
    <meta property="og:image" content="https://awsimages.detik.net.id/visual/2026/09/01/kemenkes.jpeg?w=650" />
    <meta name="keywords" content="kemenkes, puskesmas, krakatau" />
</head>
<body>
    <h1 class="detail__title">Kemenkes Siagakan 413 RS</h1>
    <div class="detail__body-text itp_bodycontent">
        <div class="collapsible" id="collapsible">
            <div class="itp__toc_title collapsible__top" id="collapsible__top">Daftar Isi</div>
            <div class="collapsible__content" id="collapsible__content">
                <ul class="mgt-0">
                    <li><a class="toc-item color__blue" href="#kasus-ispa">Kasus ISPA Tercatat Ada Kecenderungan Meningkat</a></li>
                </ul>
            </div>
        </div>
        <p>Kementerian Kesehatan (Kemenkes) RI meningkatkan kesiapsiagaan kesehatan menyusul erupsi Gunung Anak Krakatau.</p>
        <h4>Kasus ISPA Tercatat Ada Kecenderungan Meningkat</h4>
        <p>Kemenkes juga mencatat adanya kecenderungan peningkatan kasus Infeksi Saluran Pernapasan Akut (ISPA).</p>
    </div>
</body>
</html>`

	art, err := adapter.ParseArticleHTMLWithContext(context.Background(), htmlWithTOC, "https://health.detik.com/berita-detikhealth/d-8653526/kemenkes-siagakan-413-rs", "")
	if err != nil {
		t.Fatalf("ParseArticleHTMLWithContext failed: %v", err)
	}

	fullText := strings.Join(art.Description, "\n\n")

	if strings.HasPrefix(fullText, "Daftar Isi") {
		t.Errorf("FAILED: 'Daftar Isi' leaked at beginning of article description: %q", fullText)
	}

	if !strings.HasPrefix(art.Description[0], "Kementerian Kesehatan") {
		t.Errorf("Expected first paragraph to start with 'Kementerian Kesehatan', got %q", art.Description[0])
	}

	// Body heading "Kasus ISPA Tercatat Ada Kecenderungan Meningkat" inside article flow MUST be preserved
	if !strings.Contains(fullText, "Kasus ISPA Tercatat Ada Kecenderungan Meningkat") {
		t.Errorf("Sub-heading inside body was lost: %q", fullText)
	}
}



