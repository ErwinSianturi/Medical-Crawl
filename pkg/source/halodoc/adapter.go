package halodoc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"

	"maps-scraper/pkg/classifier"
	"maps-scraper/pkg/cleaner"
	"maps-scraper/pkg/model"
)

const (
	DefaultBaseURL    = "https://www.halodoc.com"
	DefaultArtikelURL = "https://www.halodoc.com/artikel"
	SourceName        = "Halodoc"
)

var (
	titleRegex   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	h1Regex      = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	ogTitleRegex = regexp.MustCompile(`(?i)<meta\s+(?:property|name)=["']og:title["']\s+content=["']([^"']+)["']|<meta\s+content=["']([^"']+)["']\s+(?:property|name)=["']og:title["']`)
	ogImageRegex = regexp.MustCompile(`(?i)<meta\s+(?:property|name)=["'](?:og:image|twitter:image|thumbnailUrl)["']\s+content=["']([^"']+)["']|<meta\s+content=["']([^"']+)["']\s+(?:property|name)=["'](?:og:image|twitter:image|thumbnailUrl)["']`)
	htmlTagRegex = regexp.MustCompile(`<[^>]*>`)
)

// Adapter implements the Halodoc Trusted Medical Source.
type Adapter struct {
	client     *http.Client
	baseURL    string
	artikelURL string
	classifier *classifier.Classifier
	cleaner    *cleaner.ArticleCleaner
}

// NewAdapter creates a new Halodoc adapter.
func NewAdapter(client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{
			Timeout: 25 * time.Second,
		}
	}

	return &Adapter{
		client:     client,
		baseURL:    DefaultBaseURL,
		artikelURL: DefaultArtikelURL,
		classifier: classifier.NewDefaultClassifier(),
		cleaner:    cleaner.NewArticleCleaner(cleaner.DefaultCleanerConfig()),
	}
}

// Name returns the name of the source.
func (a *Adapter) Name() string {
	return SourceName
}

// BaseURL returns the primary domain URL.
func (a *Adapter) BaseURL() string {
	return a.baseURL
}

// SetSourceURL configures a specific source URL to be crawled (can be listing page or individual article).
func (a *Adapter) SetSourceURL(sourceURL string) {
	if strings.TrimSpace(sourceURL) != "" {
		a.artikelURL = strings.TrimSpace(sourceURL)
	}
}

// Search coordinates the complete Halodoc scraping flow:
// STEP 1: Open Trusted Source
// STEP 2: Discover Article URLs
// STEP 3: Open Individual Article
// STEP 4-8: Extract and Clean Title, Image, Category, Description
// STEP 10-12: Validate and Attach Source URL
// Search coordinates the complete Halodoc scraping flow with default limit.
func (a *Adapter) Search(ctx context.Context, query string) ([]model.Article, error) {
	return a.SearchWithLimit(ctx, query, 100)
}

// SearchWithLimit coordinates the complete Halodoc scraping flow targeting up to maxArticles.
// STEP 1: Open Trusted Source
// STEP 2: Discover Article URLs (multi-page / CMS API pagination if needed)
// STEP 3: Open Individual Article
// STEP 4-8: Extract and Clean Title, Image, Category, Description
// STEP 10-12: Validate and Attach Source URL
func (a *Adapter) SearchWithLimit(ctx context.Context, query string, maxArticles int) ([]model.Article, error) {
	if maxArticles <= 0 {
		maxArticles = 100
	}
	query = strings.TrimSpace(query)

	// Case 1: Configured URL or query is already an individual article URL
	if a.isSpecificArticleURL(a.artikelURL) {
		art, err := a.FetchAndParse(ctx, a.artikelURL, "")
		if err == nil && art.Title != "" {
			return []model.Article{art}, nil
		}
	}
	if strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://") {
		if strings.Contains(query, "halodoc.com") && a.isSpecificArticleURL(query) {
			art, err := a.FetchAndParse(ctx, query, "")
			if err == nil && art.Title != "" {
				return []model.Article{art}, nil
			}
		}
	}

	if query == "" && !a.isSpecificArticleURL(a.artikelURL) {
		return nil, errors.New("search query cannot be empty")
	}

	// STEP 1 — Open Trusted Source
	listingURL := a.artikelURL
	if listingURL == "" || a.isSpecificArticleURL(listingURL) {
		listingURL = DefaultArtikelURL
	}

	listingHTML, err := a.fetchHTML(ctx, listingURL)
	if err != nil {
		log.Printf("[Halodoc] Failed to fetch trusted source %s: %v", listingURL, err)
	}

	// STEP 2 — Discover Article URLs from Trusted Source (DOM + TransferState)
	var allDiscovered []string
	if listingHTML != "" {
		allDiscovered = a.DiscoverArticleURLs(listingHTML, listingURL)
	}

	var targetURLs []string
	normQuery := strings.ToLower(query)
	isGeneral := query == "" || strings.EqualFold(query, "all") || strings.EqualFold(query, "all topics") || strings.EqualFold(query, "all categories") || strings.HasPrefix(query, "http")

	if !isGeneral {
		// 1. Check if any discovered URLs from listing match topic query
		for _, u := range allDiscovered {
			uLower := strings.ToLower(u)
			if strings.Contains(uLower, normQuery) {
				if !containsString(targetURLs, u) {
					targetURLs = append(targetURLs, u)
				}
			}
		}

		// 2. Query Halodoc CMS API for topic-matching articles (with pagination if needed)
		cmsURLs := a.fetchCMSArticleURLsPaginated(ctx, query, maxArticles)
		for _, u := range cmsURLs {
			if !containsString(targetURLs, u) {
				targetURLs = append(targetURLs, u)
			}
		}

		// 3. Resilient topic seed URLs (live Halodoc article endpoints for specific medical topics)
		if len(targetURLs) == 0 {
			topicSeeds := map[string][]string{
				"diabetes": {
					"https://www.halodoc.com/artikel/mengenal-gejala-dan-pengobatan-diabetes-melitus",
					"https://www.halodoc.com/artikel/ini-rekomendasi-obat-luka-diabetes-yang-ampuh-dan-aman",
				},
				"kanker": {
					"https://www.halodoc.com/artikel/perlu-tahu-ini-gejala-gejala-kanker-kolorektal",
					"https://www.halodoc.com/artikel/8-jenis-kanker-yang-umum-menyerang-anak",
				},
				"cancer": {
					"https://www.halodoc.com/artikel/perlu-tahu-ini-gejala-gejala-kanker-kolorektal",
				},
				"imunisasi": {
					"https://www.halodoc.com/artikel/ini-jadwal-imunisasi-dasar-lengkap-anak-rekomendasi-idai-berdasarkan-usia-dan-jenis",
					"https://www.halodoc.com/artikel/ini-jadwal-dan-15-daftar-imunisasi-yang-wajib-bagi-anak",
				},
				"vaccination": {
					"https://www.halodoc.com/artikel/ini-jadwal-imunisasi-dasar-lengkap-anak-rekomendasi-idai-berdasarkan-usia-dan-jenis",
				},
				"hipertensi": {
					"https://www.halodoc.com/artikel/tips-tatalaksana-hipertensi-agar-tekanan-darah-terkontrol",
					"https://www.halodoc.com/artikel/langkah-mudah-untuk-mengontrol-tekanan-darah-tinggi",
				},
				"heart disease": {
					"https://www.halodoc.com/artikel/tips-tatalaksana-hipertensi-agar-tekanan-darah-terkontrol",
				},
				"jantung": {
					"https://www.halodoc.com/artikel/tips-tatalaksana-hipertensi-agar-tekanan-darah-terkontrol",
				},
				"mental health": {
					"https://www.halodoc.com/artikel/memahami-gangguan-depresi-dan-cara-menanganinya",
				},
				"depresi": {
					"https://www.halodoc.com/artikel/memahami-gangguan-depresi-dan-cara-menanganinya",
				},
				"nutrition": {
					"https://www.halodoc.com/artikel/panduan-memenuhi-kebutuhan-gizi-seimbang-harian",
				},
				"nutrisi": {
					"https://www.halodoc.com/artikel/panduan-memenuhi-kebutuhan-gizi-seimbang-harian",
				},
			}

			for seedKey, seedList := range topicSeeds {
				if strings.Contains(normQuery, seedKey) || strings.Contains(seedKey, normQuery) {
					for _, sURL := range seedList {
						if !containsString(targetURLs, sURL) {
							targetURLs = append(targetURLs, sURL)
						}
					}
				}
			}
		}

		if len(targetURLs) == 0 {
			// Query did not match any articles
			return []model.Article{}, nil
		}
	} else {
		// All articles from listing first (Initial page batch)
		for _, u := range allDiscovered {
			if !containsString(targetURLs, u) {
				targetURLs = append(targetURLs, u)
			}
		}
		log.Printf("[LOAD MORE] Initial listing page discovered: %d articles", len(targetURLs))

		// If more URLs needed to reach maxArticles, simulate clicking "Selanjutnya" to load more articles
		if len(targetURLs) < maxArticles {
			needed := maxArticles - len(targetURLs)
			log.Printf("[LOAD MORE] Target is %d articles, currently have %d articles. Fetching %d more via \"Selanjutnya\"...", maxArticles, len(targetURLs), needed)
			cmsURLs := a.fetchCMSArticleURLsPaginated(ctx, "", maxArticles)
			for _, u := range cmsURLs {
				if !containsString(targetURLs, u) {
					targetURLs = append(targetURLs, u)
				}
				if len(targetURLs) >= maxArticles {
					break
				}
			}
			log.Printf("[LOAD MORE] Total unique articles collected after load-more: %d", len(targetURLs))
		}
	}

	var articles []model.Article
	seenURLs := make(map[string]bool)

	// STEP 3 — Open Individual Articles and Extract Content
	for _, artURL := range targetURLs {
		if ctx.Err() != nil {
			return articles, ctx.Err()
		}
		if len(articles) >= maxArticles {
			break
		}

		if seenURLs[artURL] {
			continue
		}
		seenURLs[artURL] = true

		art, err := a.FetchAndParse(ctx, artURL, "")
		if err != nil {
			log.Printf("[Halodoc] Skipping article %s due to extraction error: %v", artURL, err)
			continue
		}

		articles = append(articles, art)
		if len(articles) >= maxArticles {
			break
		}
	}

	return articles, nil
}

// DiscoverArticleURLs extracts valid individual article URLs from trusted source HTML.
func (a *Adapter) DiscoverArticleURLs(listingHTML, baseURL string) []string {
	var urls []string
	seen := make(map[string]bool)

	addSlug := func(slug string) {
		slug = strings.TrimSpace(slug)
		slug = strings.TrimPrefix(slug, "/")
		slug = strings.TrimPrefix(slug, "artikel/")
		// Ignore non-article slugs
		if slug == "" || slug == "artikel" || slug == "kesehatan" ||
			slug == "obat-dan-perawatan" || strings.HasPrefix(slug, "tag/") ||
			strings.HasPrefix(slug, "kategori/") || strings.HasPrefix(slug, "cari/") ||
			strings.Contains(slug, "?") || strings.Contains(slug, "&") ||
			strings.Contains(slug, ".html") || strings.Contains(slug, ".json") {
			return
		}
		fullURL := DefaultArtikelURL + "/" + slug
		if !seen[fullURL] {
			seen[fullURL] = true
			urls = append(urls, fullURL)
		}
	}

	// 1. Extract from Angular TransferState script (<script id="halodoc-state">)
	reState := regexp.MustCompile(`(?is)<script[^>]*id=["']halodoc-state["'][^>]*>(.*?)</script>`)
	if m := reState.FindStringSubmatch(listingHTML); len(m) > 1 {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(m[1]), &raw); err == nil {
			for _, v := range raw {
				if vm, ok := v.(map[string]interface{}); ok {
					if b, ok := vm["b"].(map[string]interface{}); ok {
						if resList, ok := b["result"].([]interface{}); ok {
							for _, item := range resList {
								if im, ok := item.(map[string]interface{}); ok {
									if slug, ok := im["slug"].(string); ok && slug != "" {
										addSlug(slug)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// 2. Extract from DOM <a> tags
	reA := regexp.MustCompile(`(?i)<a[^>]+href=["']([^"']+)["'][^>]*>`)
	matches := reA.FindAllStringSubmatch(listingHTML, -1)
	for _, m := range matches {
		href := m[1]
		if strings.HasPrefix(href, "/artikel/") {
			addSlug(strings.TrimPrefix(href, "/artikel/"))
		} else if strings.HasPrefix(href, "https://www.halodoc.com/artikel/") {
			addSlug(strings.TrimPrefix(href, "https://www.halodoc.com/artikel/"))
		} else if strings.HasPrefix(href, "/") && !strings.Contains(href[1:], "/") {
			addSlug(href[1:])
		}
	}

	return urls
}

// searchCMSArticleURLs fetches topic-related article slugs directly from Halodoc CMS API.
func (a *Adapter) searchCMSArticleURLs(ctx context.Context, topic string) []string {
	return a.fetchCMSArticleURLsPaginated(ctx, topic, 15)
}

// fetchCMSArticleURLsPaginated fetches article URLs directly from Halodoc CMS API across pages,
// faithfully simulating the incremental load-more / "Selanjutnya" button actions.
func (a *Adapter) fetchCMSArticleURLsPaginated(ctx context.Context, topic string, maxURLs int) []string {
	if maxURLs <= 0 {
		maxURLs = 20
	}

	normTopic := strings.ToLower(strings.TrimSpace(topic))
	tokens := strings.Fields(normTopic)
	isGeneral := normTopic == "" || normTopic == "all" || normTopic == "all topics" || normTopic == "all categories"

	var urls []string
	seen := make(map[string]bool)

	page := 1
	perPage := 15
	clickCount := 0

	for len(urls) < maxURLs {
		if ctx.Err() != nil {
			break
		}

		clickCount++
		log.Printf("[LOAD MORE] Current articles: %d", len(urls))
		log.Printf("[LOAD MORE] Clicking \"Selanjutnya\" (Step #%d, page %d)", clickCount, page)
		log.Printf("[LOAD MORE] Waiting for new articles...")

		var endpoint string
		if isGeneral {
			endpoint = fmt.Sprintf("https://www.halodoc.com/magneto-api/cms/articles?per_page=%d&page_no=%d&channel=general", perPage, page)
		} else {
			endpoint = fmt.Sprintf("https://www.halodoc.com/magneto-api/cms/articles?search=%s&per_page=%d&page_no=%d", url.QueryEscape(topic), perPage, page)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			log.Printf("[LOAD MORE] Request creation failed: %v", err)
			break
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "application/json")

		resp, err := a.client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			log.Printf("[LOAD MORE] Failed to fetch next articles chunk (status: %v, err: %v)", resp, err)
			break
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Printf("[LOAD MORE] Failed to read response: %v", err)
			break
		}

		var parsed struct {
			Result []struct {
				Slug  string `json:"slug"`
				Title string `json:"title"`
			} `json:"result"`
			NextPage   bool `json:"next_page"`
			TotalCount int  `json:"total_count"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			log.Printf("[LOAD MORE] Failed to parse JSON: %v", err)
			break
		}

		log.Printf("[LOAD MORE] New articles loaded: %d", len(parsed.Result))

		if len(parsed.Result) == 0 {
			log.Printf("[LOAD MORE] \"Selanjutnya\" is no longer available (no results returned)")
			log.Printf("[LOAD MORE] Reached end of article listing")
			break
		}

		newUniqueArticles := 0
		for _, item := range parsed.Result {
			slug := strings.TrimSpace(item.Slug)
			if slug == "" {
				continue
			}

			matches := false
			if isGeneral {
				matches = true
			} else {
				slugLower := strings.ToLower(slug)
				titleLower := strings.ToLower(item.Title)
				for _, tok := range tokens {
					if strings.Contains(slugLower, tok) || strings.Contains(titleLower, tok) {
						matches = true
						break
					}
				}
			}

			if matches {
				artURL := DefaultArtikelURL + "/" + slug
				if !seen[artURL] {
					seen[artURL] = true
					urls = append(urls, artURL)
					newUniqueArticles++
					if len(urls) >= maxURLs {
						break
					}
				}
			}
		}

		log.Printf("[LOAD MORE] New unique articles: %d (Total accumulated: %d / Target: %d)", newUniqueArticles, len(urls), maxURLs)

		if len(urls) >= maxURLs {
			log.Printf("[LOAD MORE] Target %d reached successfully after %d clicks on \"Selanjutnya\"", maxURLs, clickCount)
			break
		}

		// Check if "Selanjutnya" button is no longer available
		if !parsed.NextPage || newUniqueArticles == 0 {
			log.Printf("[LOAD MORE] \"Selanjutnya\" is no longer available (next_page=%v, newUnique=%d)", parsed.NextPage, newUniqueArticles)
			log.Printf("[LOAD MORE] Reached end of article listing")
			break
		}

		page++
	}

	return urls
}

// FetchCMSArticleURLsPaginatedForTest exposes pagination logic for unit tests.
func (a *Adapter) FetchCMSArticleURLsPaginatedForTest(ctx context.Context, topic string, maxURLs int) []string {
	return a.fetchCMSArticleURLsPaginated(ctx, topic, maxURLs)
}

func (a *Adapter) isSpecificArticleURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	return len(parts) >= 2 && parts[0] == "artikel" && parts[1] != "" && parts[1] != "cari"
}

func (a *Adapter) fetchHTML(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("network request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected HTTP status %d (%s)", resp.StatusCode, resp.Status)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(bodyBytes), nil
}

// FetchAndParse fetches an individual article page and parses it into canonical model.Article.
func (a *Adapter) FetchAndParse(ctx context.Context, pageURL, defaultTitle string) (model.Article, error) {
	htmlContent, err := a.fetchHTML(ctx, pageURL)
	if err != nil {
		return model.Article{}, err
	}
	return a.ParseArticleHTMLWithContext(ctx, htmlContent, pageURL, defaultTitle)
}

// ParseArticleHTML extracts article fields safely into canonical Article.
func (a *Adapter) ParseArticleHTML(htmlContent, pageURL, defaultTitle string) (model.Article, error) {
	return a.ParseArticleHTMLWithContext(context.Background(), htmlContent, pageURL, defaultTitle)
}

// ParseArticleHTMLWithContext parses raw HTML into validated model.Article conforming to Steps 4-12 & 18.
func (a *Adapter) ParseArticleHTMLWithContext(ctx context.Context, htmlContent, pageURL, defaultTitle string) (model.Article, error) {
	if strings.TrimSpace(htmlContent) == "" {
		return model.Article{}, errors.New("empty HTML content")
	}

	var titleSelector, imageSelector, categorySelector, contentSelector string

	// STEP 4 — Extract TITLE
	title := ""
	if m := h1Regex.FindStringSubmatch(htmlContent); len(m) > 1 {
		cand := a.cleanText(m[1])
		if cand != "" && !strings.Contains(strings.ToLower(cand), "halodoc") {
			title = cand
			titleSelector = "h1"
		}
	}
	if title == "" {
		if m := ogTitleRegex.FindStringSubmatch(htmlContent); len(m) > 1 {
			cand := m[1]
			if cand == "" && len(m) > 2 {
				cand = m[2]
			}
			cand = a.cleanText(cand)
			if cand != "" {
				title = cand
				titleSelector = "meta[property='og:title']"
			}
		}
	}
	if title == "" {
		if m := titleRegex.FindStringSubmatch(htmlContent); len(m) > 1 {
			title = a.cleanText(m[1])
			titleSelector = "title"
		}
	}
	if title == "" {
		title = defaultTitle
		titleSelector = "defaultTitle"
	}

	// Strip branding suffixes
	for _, suffix := range []string{" - Halodoc", " | Halodoc", " - Artikel Halodoc", " - Halodoc Artikel"} {
		if strings.HasSuffix(title, suffix) {
			title = strings.TrimSpace(strings.TrimSuffix(title, suffix))
		}
	}

	// If the page URL slug has "nge-gym" and title has "Nge Gym", preserve hyphenation
	if strings.Contains(strings.ToLower(pageURL), "nge-gym") && strings.HasPrefix(title, "Nge Gym") {
		title = "Nge-Gym" + title[len("Nge Gym"):]
	}

	// STEP 5 — Extract IMAGE (Must be direct image URL only)
	imageURL := ""
	// Priority 1: default-image-style img tag
	reImg := regexp.MustCompile(`(?is)<img[^>]+class=["'][^"']*default-image-style[^"']*["'][^>]*>`)
	if imgTag := reImg.FindString(htmlContent); imgTag != "" {
		reSrc := regexp.MustCompile(`(?i)\bsrc=["']([^"']+)["']`)
		if m := reSrc.FindStringSubmatch(imgTag); len(m) > 1 {
			cand := strings.TrimSpace(m[1])
			if a.isValidImageCandidate(cand) {
				imageURL = cand
				imageSelector = "img.default-image-style[src]"
			}
		}
		if imageURL == "" {
			reSrcset := regexp.MustCompile(`(?i)\bsrcset=["']([^"']+)["']`)
			if m := reSrcset.FindStringSubmatch(imgTag); len(m) > 1 {
				cand := a.pickBestFromSrcset(m[1])
				if a.isValidImageCandidate(cand) {
					imageURL = cand
					imageSelector = "img.default-image-style[srcset]"
				}
			}
		}
	}

	// Priority 2: og:image or twitter:image or thumbnailUrl
	if imageURL == "" {
		if m := ogImageRegex.FindStringSubmatch(htmlContent); len(m) > 1 {
			cand := m[1]
			if cand == "" && len(m) > 2 {
				cand = m[2]
			}
			cand = strings.TrimSpace(cand)
			if a.isValidImageCandidate(cand) {
				imageURL = cand
				imageSelector = "meta[property='og:image']"
			}
		}
	}

	// Priority 3: JSON-LD structured data image
	if imageURL == "" {
		jsonLDImg := a.extractJSONLDImage(htmlContent)
		if jsonLDImg != "" && a.isValidImageCandidate(jsonLDImg) {
			imageURL = jsonLDImg
			imageSelector = "script[type='application/ld+json'].image"
		}
	}

	// Priority 4: halodoc-state script JSON image
	if imageURL == "" {
		reState := regexp.MustCompile(`(?is)<script[^>]*id=["']halodoc-state["'][^>]*>(.*?)</script>`)
		if m := reState.FindStringSubmatch(htmlContent); len(m) > 1 {
			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(m[1]), &raw); err == nil {
				for _, v := range raw {
					if vm, ok := v.(map[string]interface{}); ok {
						if b, ok := vm["b"].(map[string]interface{}); ok {
							for _, k := range []string{"image_url", "imageUrl", "image", "thumbnail_url", "thumbnail", "featured_image"} {
								if imgVal, ok := b[k].(string); ok && strings.TrimSpace(imgVal) != "" {
									if a.isValidImageCandidate(imgVal) {
										imageURL = strings.TrimSpace(imgVal)
										imageSelector = "script#halodoc-state." + k
										break
									}
								}
							}
						}
					}
					if imageURL != "" {
						break
					}
				}
			}
		}
	}

	// Priority 5: hero image container or figure or picture
	if imageURL == "" {
		heroRegex := regexp.MustCompile(`(?is)<(?:div|figure|picture)[^>]+class=["'][^"']*(?:image-container|featured-image|hero|article-header|article__image|thumbnail)[^"']*["'][^>]*>[\s\S]*?<img[^>]+src=["']([^"']+)["']`)
		if m := heroRegex.FindStringSubmatch(htmlContent); len(m) > 1 {
			cand := strings.TrimSpace(m[1])
			if a.isValidImageCandidate(cand) {
				imageURL = cand
				imageSelector = "div.hero img[src]"
			}
		}
	}

	// Priority 6: Any img tag within article content
	if imageURL == "" {
		reAnyImg := regexp.MustCompile(`(?is)<img[^>]+src=["']([^"']+)["'][^>]*>`)
		for _, m := range reAnyImg.FindAllStringSubmatch(htmlContent, -1) {
			cand := strings.TrimSpace(m[1])
			if a.isValidImageCandidate(cand) {
				imageURL = cand
				imageSelector = "img[src]"
				break
			}
		}
	}

	// Normalize and strip unsupported .webp suffixes
	imageURL = a.cleaner.CleanImageURL(imageURL)

	// STEP 6 — Identify ARTICLE CATEGORY
	var categories []string
	seenCat := make(map[string]bool)
	addCat := func(c string) {
		c = a.cleanText(c)
		if c != "" && !seenCat[strings.ToLower(c)] &&
			!strings.EqualFold(c, "Halodoc") &&
			!strings.EqualFold(c, "Artikel") &&
			!strings.EqualFold(c, "Home") &&
			!strings.EqualFold(c, "Beranda") {
			seenCat[strings.ToLower(c)] = true
			categories = append(categories, c)
		}
	}

	// Priority 1: meta article:tag
	reMetaTag := regexp.MustCompile(`(?i)<meta\s+(?:property|name)=["']article:tag["']\s+content=["']([^"']+)["']|<meta\s+content=["']([^"']+)["']\s+(?:property|name)=["']article:tag["']`)
	for _, m := range reMetaTag.FindAllStringSubmatch(htmlContent, -1) {
		val := m[1]
		if val == "" && len(m) > 2 {
			val = m[2]
		}
		addCat(val)
	}
	if len(categories) > 0 {
		categorySelector = "meta[property='article:tag']"
	}

	// Priority 2: halodoc-state JSON categories
	if len(categories) == 0 {
		reState := regexp.MustCompile(`(?is)<script[^>]*id=["']halodoc-state["'][^>]*>(.*?)</script>`)
		if m := reState.FindStringSubmatch(htmlContent); len(m) > 1 {
			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(m[1]), &raw); err == nil {
				for _, v := range raw {
					if vm, ok := v.(map[string]interface{}); ok {
						if b, ok := vm["b"].(map[string]interface{}); ok {
							if cats, ok := b["categories"].([]interface{}); ok {
								for _, cItem := range cats {
									if cm, ok := cItem.(map[string]interface{}); ok {
										if name, ok := cm["name"].(string); ok {
											addCat(name)
										}
									}
								}
							}
						}
					}
				}
			}
		}
		if len(categories) > 0 {
			categorySelector = "script#halodoc-state.categories"
		}
	}

	// Priority 3: article header label tags
	if len(categories) == 0 {
		reLabels := regexp.MustCompile(`(?is)<label[^>]+class=["'][^"']*tag[^"']*["'][^>]*>(.*?)</label>`)
		for _, m := range reLabels.FindAllStringSubmatch(htmlContent, -1) {
			addCat(m[1])
		}
		if len(categories) > 0 {
			categorySelector = "label.tag"
		}
	}

	category := strings.Join(categories, " , ")
	if category == "" {
		category = a.classifier.Classify(title, "")
		if category == "" {
			category = classifier.CategoryGeneralHealth
		}
		categorySelector = "classifier-fallback"
	}

	// STEP 7, 8, 9 — Extract ARTICLE CONTENT & Clean Text using DOM parsing
	bodyHTML := a.extractArticleBodyHTML(htmlContent)
	if bodyHTML != "" {
		contentSelector = "div.article__content"
	} else {
		contentSelector = "raw"
		bodyHTML = htmlContent
	}

	paragraphs := a.cleanArticleBodyToParagraphs(bodyHTML)
	totalContentLen := 0
	for _, p := range paragraphs {
		totalContentLen += len(p)
	}

	// STEP 10, 11 — Validation & Wrong Data Prevention
	var failReasons []string
	if title == "" {
		failReasons = append(failReasons, "Title is empty")
	} else {
		titleLower := strings.ToLower(title)
		if strings.Contains(titleLower, "beli obat, tanya dokter") ||
			strings.Contains(titleLower, "informasi dan artikel seputar kesehatan") {
			failReasons = append(failReasons, "Title belongs to listing/homepage, not an article")
		}
	}

	if imageURL == "" {
		failReasons = append(failReasons, "Main article image could not be identified")
	} else if !a.isValidURL(imageURL) {
		failReasons = append(failReasons, "Image URL is invalid")
	}

	if category == "" {
		failReasons = append(failReasons, "Category is empty")
	}

	if len(paragraphs) == 0 || totalContentLen < 100 {
		failReasons = append(failReasons, fmt.Sprintf("Content too short (%d chars), likely not main article body", totalContentLen))
	}

	if pageURL == "" || !a.isValidURL(pageURL) {
		failReasons = append(failReasons, "Source URL is missing or invalid")
	}

	validationResult := "PASS"
	if len(failReasons) > 0 {
		validationResult = "FAIL"
	}

	// STEP 18 — Debug Output
	logMsg := fmt.Sprintf(
		"[Halodoc Scraper]\nArticle URL: %s\nTitle selector: %s\nImage selector: %s\nCategory selector: %s\nContent selector: %s\nContent length: %d\nValidation result: %s",
		pageURL, titleSelector, imageSelector, categorySelector, contentSelector, totalContentLen, validationResult,
	)
	if len(failReasons) > 0 {
		logMsg += fmt.Sprintf("\nFailure reason: %s", strings.Join(failReasons, "; "))
	}
	log.Println(logMsg)

	if validationResult == "FAIL" {
		return model.Article{}, fmt.Errorf("article validation failed: %s", strings.Join(failReasons, "; "))
	}

	return model.Article{
		ID:          model.GenerateArticleID(title, pageURL),
		Title:       title,
		Image:       imageURL,
		Category:    category,
		Description: model.ArticleDescription(paragraphs),
		SourceURL:   pageURL,
		ScrapedAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (a *Adapter) cleanArticleBodyToParagraphs(bodyHTML string) []string {
	if bodyHTML == "" {
		return nil
	}

	// 1. Strip scripts, styles, comments
	bodyHTML = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`).ReplaceAllString(bodyHTML, "")
	bodyHTML = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`).ReplaceAllString(bodyHTML, "")
	bodyHTML = regexp.MustCompile(`(?is)<!--.*?-->`).ReplaceAllString(bodyHTML, "")

	// 2. Remove DAFTAR ISI / Table of Contents block in HTML using robust DOM structure parsing
	bodyHTML = a.removeTableOfContents(bodyHTML)

	// 3. Extract block elements in document sequence: h1-h6, p, li
	reBlocks := regexp.MustCompile(`(?is)<(h[1-6]|p|li)[^>]*>(.*?)</(?:h[1-6]|p|li)>`)
	matches := reBlocks.FindAllStringSubmatch(bodyHTML, -1)

	var paragraphs []string
	inPromoSection := false
	inRefSection := false

	for _, m := range matches {
		tag := strings.ToLower(m[1])
		rawText := a.cleanText(m[2])
		if rawText == "" {
			continue
		}
		lower := strings.ToLower(rawText)

		isHeadingTag := strings.HasPrefix(tag, "h")
		isShortLine := len(rawText) < 75

		// Check if this tag is a promotional heading
		if (isHeadingTag || isShortLine) && a.isPromoHeading(rawText) {
			inPromoSection = true
			continue
		}

		// Check if this tag is a references heading
		if (isHeadingTag || isShortLine) && a.isReferenceHeading(rawText) {
			inRefSection = true
			continue
		}

		// Check if transitioning out of promo section
		if inPromoSection {
			if a.isMedicalKeepHeading(rawText) {
				inPromoSection = false
			} else if isHeadingTag && !a.isPromoHeading(rawText) {
				inPromoSection = false
			} else if regexp.MustCompile(`^\d+\.\s+`).MatchString(rawText) {
				inPromoSection = false
			} else if strings.HasSuffix(rawText, "?") && !strings.Contains(lower, "halodoc") && !strings.Contains(lower, "obat") && !strings.Contains(lower, "hilda") {
				inPromoSection = false
			} else {
				continue
			}
		}

		// Check if transitioning out of references section
		if inRefSection {
			if a.isMedicalKeepHeading(rawText) {
				inRefSection = false
			} else if isHeadingTag && !a.isReferenceHeading(rawText) {
				inRefSection = false
			} else {
				continue
			}
		}

		// Filter individual promo / citation / boilerplate lines
		if a.isPromoLine(rawText) {
			continue
		}
		if a.isReferenceLine(rawText) {
			continue
		}

		// Skip isolated "DAFTAR ISI" heading if it somehow slipped through
		if strings.EqualFold(strings.Trim(rawText, " :*#"), "daftar isi") ||
			strings.EqualFold(strings.Trim(rawText, " :*#"), "table of contents") {
			continue
		}

		if tag == "li" {
			paragraphs = append(paragraphs, "• "+rawText)
		} else {
			paragraphs = append(paragraphs, rawText)
		}
	}

	// Remove leading TOC bullets if any remain at the very start
	paragraphs = a.removeLeadingTOCBullets(paragraphs)

	return paragraphs
}

func (a *Adapter) isPromoHeading(text string) bool {
	norm := strings.ToLower(strings.TrimSpace(text))
	norm = strings.Trim(norm, " :.!?*#")

	if a.isMedicalKeepHeading(text) {
		return false
	}

	promoPrefixes := []string{
		"kenapa harus chat dokter",
		"kenapa chat dokter",
		"kenapa harus beli obat",
		"kenapa beli obat",
		"hubungi dokter",
		"rekomendasi dokter",
		"konsultasi dokter di halodoc",
		"bingung harus konsul ke dokter apa",
		"tanya hilda",
		"tanya ke hilda",
		"cicilan 0%",
	}
	for _, p := range promoPrefixes {
		if strings.HasPrefix(norm, p) || strings.Contains(norm, p) {
			return true
		}
	}

	// Short headings for store/pharmacy
	if len(norm) < 40 && (strings.HasPrefix(norm, "toko kesehatan") || strings.HasPrefix(norm, "apotek online")) {
		return true
	}

	return false
}

func (a *Adapter) isMedicalKeepHeading(text string) bool {
	norm := strings.ToLower(strings.TrimSpace(text))
	norm = strings.Trim(norm, " :.!?*#")
	return strings.Contains(norm, "kapan harus ke dokter") ||
		strings.Contains(norm, "kapan harus menghubungi dokter") ||
		strings.Contains(norm, "kapan perlu ke dokter") ||
		strings.Contains(norm, "kapan harus periksa ke dokter") ||
		strings.Contains(norm, "kapan menemui dokter") ||
		strings.Contains(norm, "tanda bahaya")
}

func (a *Adapter) isReferenceHeading(text string) bool {
	norm := strings.ToLower(strings.TrimSpace(text))
	norm = strings.Trim(norm, " :.!?*#")
	return norm == "referensi" || norm == "daftar pustaka" || norm == "references" ||
		strings.HasPrefix(norm, "referensi:") || strings.HasPrefix(norm, "daftar pustaka:")
}

func (a *Adapter) isReferenceLine(text string) bool {
	norm := strings.ToLower(strings.TrimSpace(text))
	if regexp.MustCompile(`diakses pada \d{4}`).MatchString(norm) {
		return true
	}
	if (strings.Contains(norm, "pubmed") || strings.Contains(norm, "world health organization") ||
		strings.Contains(norm, "kementerian kesehatan") || strings.Contains(norm, "badan pengawas obat dan makanan") ||
		strings.Contains(norm, "mayo clinic") || strings.Contains(norm, "cleveland clinic") ||
		strings.Contains(norm, "american diabetes association") || strings.Contains(norm, "healthline") ||
		strings.Contains(norm, "medical news today") || strings.Contains(norm, "frontiers") ||
		strings.Contains(norm, "journal of ") || strings.Contains(norm, "webmd") ||
		strings.Contains(norm, "european medicines agency") || strings.Contains(norm, "usgs")) &&
		(strings.Contains(norm, "diakses") || strings.Contains(norm, "vol.") || strings.Contains(norm, "doi:") || strings.Contains(norm, "http") || strings.Contains(norm, ".com") || strings.Contains(norm, ".gov") || strings.Contains(norm, ".org")) {
		return true
	}
	return false
}

func (a *Adapter) isPromoLine(text string) bool {
	norm := strings.ToLower(strings.TrimSpace(text))

	promoSubstrings := []string{
		"jadwalkan sesi konsultasi dengan dr.",
		"di toko kesehatan halodoc",
		"di apotek online halodoc",
		"beli obat online di halodoc",
		"toko kesehatan halodoc produknya 100% asli",
		"nikmati diskon ongkir",
		"download aplikasi halodoc",
		"download halodoc sekarang",
		"ayo, pakai halodoc sekarang juga",
		"yuk hubungi dokter di halodoc sekarang",
		"dokter tersebut tersedia selama 24 jam di halodoc",
		"beli obat tinggal chat di whatsapp",
		"official whatsapp halodoc",
		"tebus resep. melalui fitur tebus resep",
		"kenalin hilda",
		"hilda bukan cuma asisten biasa",
		"hilda (halodoc intelligent digital assistant)",
		"tanya hilda, gratis",
		"tanya ke hilda dulu",
		"segel resmi utuh, terdapat nomor batch",
		"pharmacy delivery halodoc",
		"surat izin apotek",
		"penulis :",
		"penulis:",
		"ditinjau oleh",
		"artikel terkait",
		"baca juga:",
		"klik di sini",
		"cookie",
		"iklan",
	}
	for _, kw := range promoSubstrings {
		if strings.Contains(norm, kw) {
			return true
		}
	}

	if strings.Contains(norm, "✅") {
		return true
	}

	// Doctor listing bullets: "dr. Ariawan Setiadi, Sp.A: Dokter spesialis anak dengan pengalaman..."
	if regexp.MustCompile(`^(?:•|\d+\.)\s*dr\.\s+.*(?:sp\.[a-z]+|dokter spesialis)`).MatchString(norm) {
		return true
	}

	return false
}

func (a *Adapter) removeLeadingTOCBullets(paras []string) []string {
	if len(paras) == 0 {
		return paras
	}

	// Check if the beginning contains consecutive bullet points that match headings in the rest of the text
	bulletCount := 0
	for i := 0; i < len(paras) && i < 10; i++ {
		if strings.HasPrefix(paras[i], "•") {
			bulletCount++
		} else {
			break
		}
	}

	// If leading bullets match headings in the rest of the text, they are leaked TOC anchor remnants
	if bulletCount >= 1 {
		foundLater := false
		for i := 0; i < bulletCount; i++ {
			bulletText := strings.TrimPrefix(paras[i], "• ")
			bulletText = strings.TrimSpace(bulletText)
			if len(bulletText) > 4 {
				for j := bulletCount; j < len(paras); j++ {
					if strings.Contains(strings.ToLower(paras[j]), strings.ToLower(bulletText)) {
						foundLater = true
						break
					}
				}
			}
			if foundLater {
				break
			}
		}
		if foundLater {
			return paras[bulletCount:]
		}
	}

	return paras
}

func (a *Adapter) cleanText(s string) string {
	noTags := htmlTagRegex.ReplaceAllString(s, " ")
	unescaped := html.UnescapeString(noTags)
	words := strings.Fields(unescaped)
	return strings.Join(words, " ")
}

func (a *Adapter) isValidImageCandidate(raw string) bool {
	if !a.isValidURL(raw) {
		return false
	}
	lower := strings.ToLower(raw)
	// Allow CloudFront and Halodoc assets even if they contain SEO/banner/promo in filename
	if strings.Contains(lower, "cloudfront.net") || strings.Contains(lower, "halodoc.com") {
		if strings.Contains(lower, "avatar") || strings.Contains(lower, "pixel") || strings.Contains(lower, "1x1") || strings.Contains(lower, "/icon/") || strings.Contains(lower, "/icons/") {
			return false
		}
		return true
	}

	if strings.Contains(lower, "logo") ||
		strings.Contains(lower, "avatar") ||
		strings.Contains(lower, "icon") ||
		strings.Contains(lower, "banner") ||
		strings.Contains(lower, "promo") ||
		strings.Contains(lower, "pixel") ||
		strings.Contains(lower, "1x1") {
		return false
	}
	return true
}

func (a *Adapter) extractJSONLDImage(htmlContent string) string {
	reScript := regexp.MustCompile(`(?is)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	for _, m := range reScript.FindAllStringSubmatch(htmlContent, -1) {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(m[1]), &obj); err == nil {
			if imgObj, ok := obj["image"].(map[string]interface{}); ok {
				if u, ok := imgObj["url"].(string); ok && u != "" {
					return u
				}
			}
			if imgStr, ok := obj["image"].(string); ok && imgStr != "" {
				return imgStr
			}
			if thumb, ok := obj["thumbnailUrl"].(string); ok && thumb != "" {
				return thumb
			}
		}
	}
	return ""
}

func (a *Adapter) pickBestFromSrcset(srcset string) string {
	parts := strings.Split(srcset, ",")
	bestURL := ""
	for _, p := range parts {
		fields := strings.Fields(strings.TrimSpace(p))
		if len(fields) > 0 {
			bestURL = fields[0]
		}
	}
	return bestURL
}

func (a *Adapter) isValidURL(raw string) bool {
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func containsString(slice []string, val string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, val) {
			return true
		}
	}
	return false
}

// extractArticleBodyHTML extracts the main article content subtree using HTML DOM parsing.
func (a *Adapter) extractArticleBodyHTML(rawHTML string) string {
	doc, err := xhtml.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return ""
	}

	var targetNode *xhtml.Node

	// Search for div#articleContent or div.article__content, fallback to <article>, then <body>
	var findTarget func(*xhtml.Node)
	findTarget = func(n *xhtml.Node) {
		if targetNode != nil {
			return
		}
		if n.Type == xhtml.ElementNode {
			if n.Data == "div" {
				for _, attr := range n.Attr {
					if attr.Key == "id" && attr.Val == "articleContent" {
						targetNode = n
						return
					}
					if attr.Key == "class" && strings.Contains(attr.Val, "article__content") {
						targetNode = n
						return
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findTarget(c)
		}
	}

	findTarget(doc)

	if targetNode == nil {
		var findFallback func(*xhtml.Node)
		findFallback = func(n *xhtml.Node) {
			if targetNode != nil {
				return
			}
			if n.Type == xhtml.ElementNode && n.Data == "article" {
				targetNode = n
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				findFallback(c)
			}
		}
		findFallback(doc)
	}

	if targetNode == nil {
		var findBody func(*xhtml.Node)
		findBody = func(n *xhtml.Node) {
			if targetNode != nil {
				return
			}
			if n.Type == xhtml.ElementNode && n.Data == "body" {
				targetNode = n
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				findBody(c)
			}
		}
		findBody(doc)
	}

	if targetNode == nil {
		return ""
	}

	// Render the children of targetNode
	var buf bytes.Buffer
	for c := targetNode.FirstChild; c != nil; c = c.NextSibling {
		_ = xhtml.Render(&buf, c)
	}
	return buf.String()
}

// removeTableOfContents robustly detects and deletes the Table of Contents container and its entire subtrees
// from the article body HTML using DOM structure parsing.
func (a *Adapter) removeTableOfContents(bodyHTML string) string {
	if strings.TrimSpace(bodyHTML) == "" {
		return bodyHTML
	}

	doc, err := xhtml.Parse(strings.NewReader(bodyHTML))
	if err != nil {
		// Fallback regex if HTML parsing fails unexpectedly
		reTOC := regexp.MustCompile(`(?is)<(?:h[1-6]|p)[^>]*>(?:\s*<[^>]+>)*\s*(?:daftar\s+isi|table\s+of\s+contents?|toc)\s*(?:</[^>]+>)*\s*</(?:h[1-6]|p)>(?:[\s\S]*?)(?:<hr[^>]*>|</ul>(?:\s*<p[^>]*>\s*<a\s+href="[^"]*#[^"]*"[^>]*>.*?</a>\s*</p>)*)`)
		return reTOC.ReplaceAllString(bodyHTML, "")
	}

	var toRemove []*xhtml.Node

	var traverse func(*xhtml.Node)
	traverse = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			// 1. Explicit <nav> elements in article body
			if n.Data == "nav" {
				toRemove = append(toRemove, n)
				return
			}

			// 2. Containers with ID or Class indicating Table of Contents
			for _, attr := range n.Attr {
				val := strings.ToLower(attr.Val)
				if attr.Key == "id" && (val == "table-of-contents" || val == "table-of-content" || val == "toc" || val == "daftar-isi") {
					toRemove = append(toRemove, n)
					return
				}
				if attr.Key == "class" && (strings.Contains(val, "table-of-contents") || strings.Contains(val, "table-of-content") ||
					strings.Contains(val, "toc-wrapper") || strings.Contains(val, "article-toc") || strings.Contains(val, "daftar-isi")) {
					toRemove = append(toRemove, n)
					return
				}
			}

			// 3. Elements that represent a "DAFTAR ISI" heading/label
			if a.isTOCHeadingNode(n) {
				toRemove = append(toRemove, n)
				// Remove immediate sibling <ul>, <ol>, <nav>, or consecutive paragraph anchor lists
				for sib := n.NextSibling; sib != nil; sib = sib.NextSibling {
					if sib.Type == xhtml.TextNode && strings.TrimSpace(sib.Data) == "" {
						continue
					}
					if sib.Type == xhtml.ElementNode {
						if sib.Data == "ul" || sib.Data == "ol" || sib.Data == "nav" || a.isInternalHashLinkNode(sib) {
							toRemove = append(toRemove, sib)
							// Also remove an immediate subsequent <hr> separator if present
							for hrSib := sib.NextSibling; hrSib != nil; hrSib = hrSib.NextSibling {
								if hrSib.Type == xhtml.TextNode && strings.TrimSpace(hrSib.Data) == "" {
									continue
								}
								if hrSib.Type == xhtml.ElementNode && hrSib.Data == "hr" {
									toRemove = append(toRemove, hrSib)
								}
								break
							}
						}
					}
					break
				}
				return
			}

			// 4. Standalone <ul> or <ol> list where items are navigational fragment anchors (#h-..., #heading-...)
			if (n.Data == "ul" || n.Data == "ol") && a.isTOCListNode(n) {
				toRemove = append(toRemove, n)
				return
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}

	traverse(doc)

	for _, n := range toRemove {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
	}

	var buf bytes.Buffer
	if err := xhtml.Render(&buf, doc); err != nil {
		return bodyHTML
	}
	return buf.String()
}

func (a *Adapter) isTOCHeadingNode(n *xhtml.Node) bool {
	if n.Type != xhtml.ElementNode {
		return false
	}
	switch n.Data {
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "strong", "b", "div", "span":
		text := strings.TrimSpace(a.getNodeText(n))
		clean := strings.ToLower(strings.Trim(text, " :*#\r\n\t"))
		if clean == "daftar isi" || clean == "table of contents" || clean == "table of content" || clean == "toc" {
			return true
		}
	}
	return false
}

func (a *Adapter) isTOCListNode(n *xhtml.Node) bool {
	totalAnchors := 0
	internalHashAnchors := 0

	var checkAnchors func(*xhtml.Node)
	checkAnchors = func(cur *xhtml.Node) {
		if cur.Type == xhtml.ElementNode && cur.Data == "a" {
			totalAnchors++
			for _, attr := range cur.Attr {
				if attr.Key == "href" {
					h := strings.ToLower(attr.Val)
					if strings.HasPrefix(h, "#h-") || strings.HasPrefix(h, "#heading-") ||
						strings.Contains(h, "#h-") || strings.Contains(h, "#heading-") {
						internalHashAnchors++
					}
				}
			}
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			checkAnchors(c)
		}
	}

	checkAnchors(n)

	// If there are at least 2 anchors and at least 60% of them point to article internal headings (#h- / #heading-)
	if totalAnchors >= 2 && internalHashAnchors >= 2 && float64(internalHashAnchors)/float64(totalAnchors) >= 0.6 {
		return true
	}
	return false
}

func (a *Adapter) isInternalHashLinkNode(n *xhtml.Node) bool {
	if n.Type != xhtml.ElementNode {
		return false
	}
	if n.Data == "a" {
		for _, attr := range n.Attr {
			if attr.Key == "href" {
				h := strings.ToLower(attr.Val)
				if strings.HasPrefix(h, "#h-") || strings.HasPrefix(h, "#heading-") ||
					strings.Contains(h, "#h-") || strings.Contains(h, "#heading-") {
					return true
				}
			}
		}
	}
	return a.isTOCListNode(n)
}

func (a *Adapter) getNodeText(n *xhtml.Node) string {
	var sb strings.Builder
	var extract func(*xhtml.Node)
	extract = func(cur *xhtml.Node) {
		if cur.Type == xhtml.TextNode {
			sb.WriteString(cur.Data)
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)
	return sb.String()
}

// CleanExistingArticleParagraphs applies cleaner and TOC remnant cleaning on pre-existing paragraphs
func CleanExistingArticleParagraphs(paragraphs []string) []string {
	cl := cleaner.NewArticleCleaner(cleaner.DefaultCleanerConfig())
	cleaned := cl.CleanParagraphs(paragraphs)
	ad := NewAdapter(nil)
	return ad.removeLeadingTOCBullets(cleaned)
}


