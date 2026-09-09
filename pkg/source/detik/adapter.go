package detik

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
	DefaultBaseURL = "https://health.detik.com"
	SourceName     = "detikHealth"
)

var (
	titleRegex   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	h1Regex      = regexp.MustCompile(`(?is)<h1[^>]*class=["'][^"']*detail__title[^"']*["'][^>]*>(.*?)</h1>|<h1[^>]*>(.*?)</h1>`)
	ogTitleRegex = regexp.MustCompile(`(?i)<meta\s+(?:property|name)=["']og:title["']\s+content=["']([^"']+)["']|<meta\s+content=["']([^"']+)["']\s+(?:property|name)=["']og:title["']`)
	ogImageRegex = regexp.MustCompile(`(?i)<meta\s+(?:property|name)=["']og:image["']\s+content=["']([^"']+)["']|<meta\s+content=["']([^"']+)["']\s+(?:property|name)=["']og:image["']`)
	htmlTagRegex = regexp.MustCompile(`<[^>]*>`)
	articleIDReg = regexp.MustCompile(`/d-(\d+)`)
)

// Adapter implements the detikHealth crawler as an isolated source adapter.
type Adapter struct {
	client     *http.Client
	baseURL    string
	sourceURL  string
	classifier *classifier.Classifier
	cleaner    *cleaner.ArticleCleaner
}

// NewAdapter creates a new Detik source adapter.
func NewAdapter(client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{
			Timeout: 25 * time.Second,
		}
	}

	return &Adapter{
		client:     client,
		baseURL:    DefaultBaseURL,
		sourceURL:  DefaultBaseURL,
		classifier: classifier.NewDefaultClassifier(),
		cleaner:    cleaner.NewArticleCleaner(cleaner.DefaultCleanerConfig()),
	}
}

// Name returns the display name of the source.
func (a *Adapter) Name() string {
	return SourceName
}

// BaseURL returns the primary domain URL.
func (a *Adapter) BaseURL() string {
	return a.baseURL
}

// SetSourceURL configures a specific source URL to be crawled.
func (a *Adapter) SetSourceURL(sourceURL string) {
	if strings.TrimSpace(sourceURL) != "" {
		a.sourceURL = strings.TrimSpace(sourceURL)
	}
}

// Search retrieves articles matching query with default limit.
func (a *Adapter) Search(ctx context.Context, query string) ([]model.Article, error) {
	return a.SearchWithLimit(ctx, query, 100)
}

// SearchWithLimit coordinates article discovery and content extraction up to maxArticles.
func (a *Adapter) SearchWithLimit(ctx context.Context, query string, maxArticles int) ([]model.Article, error) {
	if maxArticles <= 0 {
		maxArticles = 100
	}
	query = strings.TrimSpace(query)

	// Case 1: Configured sourceURL or query is already an individual article URL
	if a.IsValidArticleURL(a.sourceURL) {
		art, err := a.FetchAndParse(ctx, a.sourceURL, "")
		if err == nil && art.Title != "" {
			return []model.Article{art}, nil
		}
	}
	if strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://") {
		if a.IsValidArticleURL(query) {
			art, err := a.FetchAndParse(ctx, query, "")
			if err == nil && art.Title != "" {
				return []model.Article{art}, nil
			}
		}
	}

	normQuery := strings.ToLower(query)
	isGeneral := query == "" || strings.EqualFold(normQuery, "all") ||
		strings.EqualFold(normQuery, "all topics") ||
		strings.EqualFold(normQuery, "all categories") ||
		strings.EqualFold(normQuery, "semua topik") ||
		strings.HasPrefix(query, "http")

	var targetURLs []string
	seenURLs := make(map[string]bool)

	addURL := func(u string) {
		norm := a.normalizeArticleURL(u)
		if norm != "" && a.IsValidArticleURL(norm) && !seenURLs[norm] {
			seenURLs[norm] = true
			targetURLs = append(targetURLs, norm)
		}
	}

	if isGeneral {
		// Discover from homepage and index pages
		discoveryPages := []string{
			a.baseURL,
			a.baseURL + "/indeks",
			a.baseURL + "/berita-detikhealth",
			a.baseURL + "/wellness-diet",
			a.baseURL + "/infografis",
		}

		for _, pageURL := range discoveryPages {
			if len(targetURLs) >= maxArticles || ctx.Err() != nil {
				break
			}
			htmlContent, err := a.fetchHTML(ctx, pageURL)
			if err != nil {
				log.Printf("[detikHealth] Warning: failed to fetch discovery page %s: %v", pageURL, err)
				continue
			}
			discovered := a.DiscoverArticleURLs(htmlContent, pageURL)
			for _, u := range discovered {
				addURL(u)
				if len(targetURLs) >= maxArticles {
					break
				}
			}
		}

		// Follow pagination on /indeks if more articles needed
		page := 2
		for len(targetURLs) < maxArticles && page <= 25 {
			if ctx.Err() != nil {
				break
			}
			pageURL := fmt.Sprintf("%s/indeks?page=%d", a.baseURL, page)
			htmlContent, err := a.fetchHTML(ctx, pageURL)
			if err != nil {
				break
			}
			discovered := a.DiscoverArticleURLs(htmlContent, pageURL)
			if len(discovered) == 0 {
				break
			}
			newFound := 0
			for _, u := range discovered {
				if !seenURLs[a.normalizeArticleURL(u)] {
					addURL(u)
					newFound++
					if len(targetURLs) >= maxArticles {
						break
					}
				}
			}
			if newFound == 0 {
				break
			}
			page++
		}
	} else {
		// Query Detik Search endpoint for health articles
		searchPage := 1
		for len(targetURLs) < maxArticles && searchPage <= 25 {
			if ctx.Err() != nil {
				break
			}
			searchEndpoint := fmt.Sprintf("https://www.detik.com/search/searchall?query=%s&siteid=55&result_type=latest&page=%d", url.QueryEscape(query), searchPage)
			htmlContent, err := a.fetchHTML(ctx, searchEndpoint)
			if err != nil {
				log.Printf("[detikHealth] Warning: failed to fetch search endpoint %s: %v", searchEndpoint, err)
				break
			}
			discovered := a.DiscoverArticleURLs(htmlContent, searchEndpoint)
			if len(discovered) == 0 {
				break
			}
			newFound := 0
			for _, u := range discovered {
				addURL(u)
				newFound++
				if len(targetURLs) >= maxArticles {
					break
				}
			}
			if newFound == 0 {
				break
			}
			searchPage++
		}

		// Fallback: If search endpoint returned few/no URLs, query homepage/sections and match keywords
		if len(targetURLs) < maxArticles {
			tokens := strings.Fields(normQuery)
			htmlContent, err := a.fetchHTML(ctx, a.baseURL)
			if err == nil {
				discovered := a.DiscoverArticleURLs(htmlContent, a.baseURL)
				for _, u := range discovered {
					uLower := strings.ToLower(u)
					for _, tok := range tokens {
						if strings.Contains(uLower, tok) {
							addURL(u)
							break
						}
					}
					if len(targetURLs) >= maxArticles {
						break
					}
				}
			}
		}
	}

	if len(targetURLs) == 0 {
		return []model.Article{}, nil
	}

	if len(targetURLs) > maxArticles {
		targetURLs = targetURLs[:maxArticles]
	}

	var articles []model.Article
	for _, artURL := range targetURLs {
		if ctx.Err() != nil {
			return articles, ctx.Err()
		}

		art, err := a.FetchAndParse(ctx, artURL, "")
		if err != nil {
			log.Printf("[detikHealth] Failed extracting content\nURL: %s\nReason: %v\nstage: ParseArticleHTML", artURL, err)
			continue
		}

		articles = append(articles, art)
		if len(articles) >= maxArticles {
			break
		}
	}

	return articles, nil
}

// DiscoverArticleURLs extracts valid individual detikHealth article URLs from HTML content.
func (a *Adapter) DiscoverArticleURLs(listingHTML, baseURL string) []string {
	if strings.TrimSpace(listingHTML) == "" {
		return nil
	}

	var urls []string
	seen := make(map[string]bool)

	// 1. Extract from standard <a> href tags
	reA := regexp.MustCompile(`(?i)<a[^>]+href=["']([^"']+)["'][^>]*>`)
	matches := reA.FindAllStringSubmatch(listingHTML, -1)
	for _, m := range matches {
		raw := strings.TrimSpace(m[1])
		if raw == "" || strings.HasPrefix(raw, "javascript:") || strings.HasPrefix(raw, "#") {
			continue
		}

		resolved := a.resolveURL(raw, baseURL)
		normalized := a.normalizeArticleURL(resolved)

		if normalized != "" && a.IsValidArticleURL(normalized) && !seen[normalized] {
			seen[normalized] = true
			urls = append(urls, normalized)
		}
	}

	// 2. Extract from JSON-LD or script data-url if available
	reDataURL := regexp.MustCompile(`(?i)data-url=["']([^"']+)["']`)
	dataMatches := reDataURL.FindAllStringSubmatch(listingHTML, -1)
	for _, m := range dataMatches {
		raw := strings.TrimSpace(m[1])
		resolved := a.resolveURL(raw, baseURL)
		normalized := a.normalizeArticleURL(resolved)
		if normalized != "" && a.IsValidArticleURL(normalized) && !seen[normalized] {
			seen[normalized] = true
			urls = append(urls, normalized)
		}
	}

	return urls
}

// IsValidArticleURL verifies that a URL points to a legitimate detikHealth article.
func (a *Adapter) IsValidArticleURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}

	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}

	host := strings.ToLower(u.Hostname())
	if host != "health.detik.com" {
		return false
	}

	path := strings.ToLower(u.Path)

	// Must contain Detik article identifier pattern /d-[0-9]+
	if !articleIDReg.MatchString(path) {
		return false
	}

	// Reject non-article patterns
	if strings.HasPrefix(path, "/tag/") ||
		strings.HasPrefix(path, "/indeks/") ||
		strings.HasPrefix(path, "/search/") ||
		strings.HasPrefix(path, "/author/") ||
		strings.HasPrefix(path, "/video/") ||
		strings.Contains(path, "/komentar") ||
		strings.Contains(path, "/single/") {
		return false
	}

	return true
}

func (a *Adapter) normalizeArticleURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	u.Fragment = ""
	u.RawQuery = "" // Strip utm tracking parameters
	return u.String()
}

func (a *Adapter) resolveURL(ref, base string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return ref
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return baseURL.ResolveReference(refURL).String()
}

func (a *Adapter) fetchHTML(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "id-ID,id;q=0.9,en-US;q=0.8,en;q=0.7")

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

// FetchAndParse fetches an individual article and parses it into canonical model.Article.
func (a *Adapter) FetchAndParse(ctx context.Context, pageURL, defaultTitle string) (model.Article, error) {
	htmlContent, err := a.fetchHTML(ctx, pageURL)
	if err != nil {
		return model.Article{}, err
	}
	return a.ParseArticleHTMLWithContext(ctx, htmlContent, pageURL, defaultTitle)
}

// ParseArticleHTML extracts article fields into canonical model.Article.
func (a *Adapter) ParseArticleHTML(htmlContent, defaultTitle string) (model.Article, error) {
	return a.ParseArticleHTMLWithContext(context.Background(), htmlContent, "", defaultTitle)
}

// ParseArticleHTMLWithContext parses raw detikHealth HTML into validated model.Article.
func (a *Adapter) ParseArticleHTMLWithContext(ctx context.Context, htmlContent, pageURL, defaultTitle string) (model.Article, error) {
	if strings.TrimSpace(htmlContent) == "" {
		return model.Article{}, errors.New("empty HTML content")
	}

	var titleSelector, imageSelector, categorySelector, contentSelector string

	// STEP 7 — TITLE EXTRACTION
	title := ""
	// Priority 1: h1.detail__title or h1 inside article
	if m := h1Regex.FindStringSubmatch(htmlContent); len(m) > 0 {
		for i := 1; i < len(m); i++ {
			cand := a.cleanText(m[i])
			if cand != "" && !strings.EqualFold(cand, "detikHealth") {
				title = cand
				titleSelector = "h1.detail__title"
				break
			}
		}
	}

	// Priority 2: og:title
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

	// Priority 3: JSON-LD headline
	if title == "" {
		jsonLDHeadline := a.extractJSONLDHeadline(htmlContent)
		if jsonLDHeadline != "" {
			title = a.cleanText(jsonLDHeadline)
			titleSelector = "script[type='application/ld+json'].headline"
		}
	}

	// Priority 4: title tag
	if title == "" {
		if m := titleRegex.FindStringSubmatch(htmlContent); len(m) > 1 {
			title = a.cleanText(m[1])
			titleSelector = "title"
		}
	}

	// Priority 5: defaultTitle
	if title == "" {
		title = defaultTitle
		titleSelector = "defaultTitle"
	}

	// Trimming and whitespace / branding suffix normalization
	brandingSuffixes := []string{
		" - detikHealth", " | detikHealth", " - Detik Health", " | Detik Health",
		" - Detikcom", " | Detikcom", " - detikcom", " | detikcom",
	}
	for _, suffix := range brandingSuffixes {
		if strings.HasSuffix(title, suffix) {
			title = strings.TrimSpace(strings.TrimSuffix(title, suffix))
		}
	}

	// STEP 9 — MAIN IMAGE EXTRACTION
	imageURL := ""
	// Priority 1: Main article image figure or container
	reFigureImg := regexp.MustCompile(`(?is)<figure[^>]*class=["'][^"']*detail__media-image[^"']*["'][^>]*>[\s\S]*?<img[^>]+src=["']([^"']+)["']`)
	if m := reFigureImg.FindStringSubmatch(htmlContent); len(m) > 1 {
		cand := strings.TrimSpace(m[1])
		if a.isValidImageCandidate(cand) {
			imageURL = cand
			imageSelector = "figure.detail__media-image img[src]"
		}
	}

	if imageURL == "" {
		reMediaImg := regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*(?:detail__media|detail__media-image)[^"']*["'][^>]*>[\s\S]*?<img[^>]+src=["']([^"']+)["']`)
		if m := reMediaImg.FindStringSubmatch(htmlContent); len(m) > 1 {
			cand := strings.TrimSpace(m[1])
			if a.isValidImageCandidate(cand) {
				imageURL = cand
				imageSelector = "div.detail__media img[src]"
			}
		}
	}

	// Priority 2: og:image
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

	// Priority 3: JSON-LD image
	if imageURL == "" {
		jsonLDImg := a.extractJSONLDImage(htmlContent)
		if jsonLDImg != "" && a.isValidImageCandidate(jsonLDImg) {
			imageURL = jsonLDImg
			imageSelector = "script[type='application/ld+json'].image"
		}
	}

	// Priority 4: thumbnailUrl or twitter:image
	if imageURL == "" {
		reThumb := regexp.MustCompile(`(?i)<meta\s+(?:name|property)=["'](?:thumbnailUrl|twitter:image|dtk:thumbnailUrl)["']\s+content=["']([^"']+)["']`)
		if m := reThumb.FindStringSubmatch(htmlContent); len(m) > 1 {
			cand := strings.TrimSpace(m[1])
			if a.isValidImageCandidate(cand) {
				imageURL = cand
				imageSelector = "meta[name='thumbnailUrl']"
			}
		}
	}

	// STEP 8 — CATEGORY / TAG EXTRACTION
	var tags []string
	seenTag := make(map[string]bool)

	addTag := func(t string) {
		t = a.cleanText(t)
		t = strings.Trim(t, `"',.-`)
		if t == "" {
			return
		}
		tLower := strings.ToLower(t)
		if seenTag[tLower] ||
			strings.EqualFold(t, "detikHealth") ||
			strings.EqualFold(t, "detikcom") ||
			strings.EqualFold(t, "Home") ||
			strings.EqualFold(t, "Beranda") ||
			strings.EqualFold(t, "Berita") ||
			strings.EqualFold(t, "Infografis") ||
			strings.EqualFold(t, "Artikel") {
			return
		}
		seenTag[tLower] = true
		tags = append(tags, t)
	}

	// Priority 1: Extract from detail__body-tag nav items
	reBodyTag := regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*(?:detail__body-tag|detail__tag)[^"']*["'][^>]*>(.*?)(?:</div>\s*</div>|</div>\s*</article>|</section>|$)`)
	if m := reBodyTag.FindStringSubmatch(htmlContent); len(m) > 1 {
		reTagA := regexp.MustCompile(`(?is)<a[^>]*>(.*?)</a>`)
		for _, am := range reTagA.FindAllStringSubmatch(m[1], -1) {
			addTag(am[1])
		}
		if len(tags) > 0 {
			categorySelector = "div.detail__body-tag a"
		}
	}

	if len(tags) == 0 {
		// Try matching individual tag links across the page
		reTagLinks := regexp.MustCompile(`(?is)<a[^>]+dtr-evt=["']tag["'][^>]*dtr-ttl=["']([^"']+)["'][^>]*>|<a[^>]+href=["']https?://(?:www\.)?detik\.com/tag/[^"']+["'][^>]*>(.*?)</a>`)
		for _, m := range reTagLinks.FindAllStringSubmatch(htmlContent, -1) {
			if m[1] != "" {
				addTag(m[1])
			} else if len(m) > 2 && m[2] != "" {
				addTag(m[2])
			}
		}
		if len(tags) > 0 {
			categorySelector = "a[dtr-evt='tag']"
		}
	}

	// Priority 2: Extract from meta keywords or dtk:keywords
	if len(tags) == 0 {
		reKeywords := regexp.MustCompile(`(?i)<meta\s+(?:name|property)=["'](?:keywords|dtk:keywords)["']\s+content=["']([^"']+)["']`)
		if m := reKeywords.FindStringSubmatch(htmlContent); len(m) > 1 {
			kwParts := strings.Split(m[1], ",")
			for _, p := range kwParts {
				cand := strings.TrimSpace(p)
				if cand != "" && len(cand) <= 30 {
					addTag(cand)
				}
				if len(tags) >= 5 {
					break
				}
			}
			if len(tags) > 0 {
				categorySelector = "meta[name='keywords']"
			}
		}
	}

	// Priority 3: Extract from JSON-LD
	if len(tags) == 0 {
		jsonLDKw := a.extractJSONLDKeywords(htmlContent)
		for _, kw := range jsonLDKw {
			addTag(kw)
			if len(tags) >= 5 {
				break
			}
		}
		if len(tags) > 0 {
			categorySelector = "script[type='application/ld+json'].keywords"
		}
	}

	category := strings.Join(tags, ", ")
	if category == "" {
		category = a.classifier.Classify(title, "")
		if category == "" {
			category = classifier.CategoryGeneralHealth
		}
		categorySelector = "classifier-fallback"
	}

	// STEP 10, 11, 12 — ARTICLE CONTENT EXTRACTION & CLEANING
	//
	// DetikHealth articles use two structures:
	//
	//   A. Multi-card (paginated): Content split across multiple
	//      <section id="section-detail-N"> elements inside div.detail__body-text.
	//      The section elements contain ONLY article text — no footer/sidebar.
	//
	//   B. Single-page: Content directly in div.detail__body-text as <p> tags,
	//      without section-detail wrappers.
	//
	// NOTE: div.detail__body-text on detikHealth contains the entire page footer
	// as well, so depth-counting the full div would capture footer/navigation.
	// The section-detail-N approach cleanly limits extraction to article content.
	//
	// Strategy:
	//   1. PRIMARY: Collect all <section id="section-detail-N"> blocks.
	//      These contain only article body — no sidebar/footer pollution.
	//   2. FALLBACK: Use depth-counting on div.detail__body-text for single-page
	//      articles without section-detail wrappers (limited to ~50 nested divs).
	//   3. FALLBACK: article tag.
	//   4. LAST RESORT: raw HTML.
	bodyHTML := ""

	// Priority 1: Collect all section-detail-N blocks (covers multi-card articles).
	// These <section> elements contain the clean article text only.
	reSections := regexp.MustCompile(`(?is)<section[^>]*id=["']section-detail-\d+["'][^>]*>(.*?)</section>`)
	var sectionParts []string
	for _, m := range reSections.FindAllStringSubmatch(htmlContent, -1) {
		secHTML := m[1]
		if a.isNonArticleSection(secHTML) {
			continue
		}
		sectionParts = append(sectionParts, secHTML)
	}
	if len(sectionParts) > 0 {
		bodyHTML = strings.Join(sectionParts, "\n")
		contentSelector = "section[id^='section-detail-']"
	}

	// Priority 2: For articles without section-detail wrappers, use depth-counting
	// extraction of div.detail__body-text (single-page article format).
	// We cap the search at the first section/footer marker to avoid capturing navigation.
	if bodyHTML == "" {
		reBodyText := regexp.MustCompile(`(?i)<div[^>]*class=["'][^"']*detail__body-text[^"']*["'][^>]*>`)
		if loc := reBodyText.FindStringIndex(htmlContent); loc != nil {
			extracted := a.extractFullDivContent(htmlContent, loc[0])
			if extracted != "" {
				bodyHTML = extracted
				contentSelector = "div.detail__body-text"
			}
		}
	}

	if bodyHTML == "" {
		reArt := regexp.MustCompile(`(?is)<article[^>]*>(.*?)</article>`)
		if m := reArt.FindStringSubmatch(htmlContent); len(m) > 1 {
			bodyHTML = m[1]
			contentSelector = "article"
		} else {
			bodyHTML = htmlContent
			contentSelector = "raw"
		}
	}

	paragraphs := a.cleanArticleBodyToParagraphs(bodyHTML)
	totalContentLen := 0
	for _, p := range paragraphs {
		totalContentLen += len(p)
	}

	// STEP 16, 17 — VALIDATION & WRONG DATA PREVENTION
	var failReasons []string
	if title == "" {
		failReasons = append(failReasons, "Title is empty")
	} else {
		titleLower := strings.ToLower(title)
		if strings.Contains(titleLower, "berita artikel kesehatan, diet, seks") ||
			strings.Contains(titleLower, "indeks berita") {
			failReasons = append(failReasons, "Title belongs to homepage/index, not an article")
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

	if len(paragraphs) == 0 || totalContentLen < 60 {
		failReasons = append(failReasons, fmt.Sprintf("Content too short (%d chars), likely not main article body", totalContentLen))
	}

	if pageURL != "" && !a.isValidURL(pageURL) {
		failReasons = append(failReasons, "Source URL is invalid")
	}

	validationResult := "PASS"
	if len(failReasons) > 0 {
		validationResult = "FAIL"
	}

	logMsg := fmt.Sprintf(
		"[detikHealth Scraper]\nArticle URL: %s\nTitle selector: %s\nImage selector: %s\nCategory selector: %s\nContent selector: %s\nContent length: %d\nValidation result: %s",
		pageURL, titleSelector, imageSelector, categorySelector, contentSelector, totalContentLen, validationResult,
	)
	if len(failReasons) > 0 {
		logMsg += fmt.Sprintf("\nFailure reason: %s", strings.Join(failReasons, "; "))
		log.Println(logMsg)
		return model.Article{}, fmt.Errorf("article validation failed: %s", strings.Join(failReasons, "; "))
	}
	log.Println(logMsg)

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

func (a *Adapter) isNonArticleSection(secHTML string) bool {
	lower := strings.ToLower(secHTML)
	if strings.Contains(lower, "class=\"aevp") ||
		strings.Contains(lower, "class='aevp") ||
		strings.Contains(lower, "class=\"pip-vid") ||
		strings.Contains(lower, "class='pip-vid") ||
		strings.Contains(lower, "class=\"sisip_video") ||
		strings.Contains(lower, "class='sisip_video") ||
		strings.Contains(lower, "class=\"cb-berita-terkait") ||
		strings.Contains(lower, "class='cb-berita-terkait") ||
		strings.Contains(lower, "class=\"cb-artikel-lainnya") ||
		strings.Contains(lower, "class='cb-artikel-lainnya") {
		return true
	}
	return false
}

func (a *Adapter) removeNonArticleNodes(bodyHTML string) string {
	if strings.TrimSpace(bodyHTML) == "" {
		return bodyHTML
	}

	doc, err := xhtml.Parse(strings.NewReader(bodyHTML))
	if err != nil {
		return bodyHTML
	}

	var toRemove []*xhtml.Node

	var traverse func(*xhtml.Node)
	traverse = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			tag := strings.ToLower(n.Data)
			if tag == "script" || tag == "style" || tag == "iframe" || tag == "social-actions" || tag == "cb-rekomendit" || tag == "svg" {
				toRemove = append(toRemove, n)
				return
			}

			for _, attr := range n.Attr {
				val := strings.ToLower(attr.Val)
				if attr.Key == "class" || attr.Key == "id" {
					if strings.Contains(val, "aevp") ||
						strings.Contains(val, "pip-vid") ||
						strings.Contains(val, "sisip_video") ||
						strings.Contains(val, "detail__media-video") ||
						strings.Contains(val, "detail__video") ||
						strings.Contains(val, "video-container") ||
						strings.Contains(val, "video20detik") ||
						strings.Contains(val, "detail__video-transcript") ||
						strings.Contains(val, "collapsible") ||
						strings.Contains(val, "itp__toc_title") ||
						strings.Contains(val, "collapsible__top") ||
						strings.Contains(val, "collapsible__content") ||
						strings.Contains(val, "toc-item") ||
						strings.Contains(val, "table-of-contents") ||
						strings.Contains(val, "table-of-content") ||
						strings.Contains(val, "toc-wrapper") ||
						strings.Contains(val, "article-toc") ||
						strings.Contains(val, "daftar-isi") ||
						val == "toc" ||
						strings.Contains(val, "cb-berita-terkait") ||
						strings.Contains(val, "cb-artikel-lainnya") ||
						strings.Contains(val, "cb-infografis-lainnya") ||
						strings.Contains(val, "cb-rekomendit") ||
						strings.Contains(val, "koleksi-wrap") ||
						strings.Contains(val, "newstag") ||
						strings.Contains(val, "detail__body-tag") ||
						strings.Contains(val, "detail__tag") ||
						strings.Contains(val, "parallaxindetail") ||
						strings.Contains(val, "staticdetail_container") ||
						strings.Contains(val, "advertisement") ||
						strings.Contains(val, "noncontent") ||
						strings.Contains(val, "linksisip") ||
						strings.Contains(val, "lihatjg") ||
						strings.Contains(val, "pic_artikel_sisip") ||
						strings.Contains(val, "sticky-share") ||
						strings.Contains(val, "wrap-toast") ||
						strings.Contains(val, "toast") {
						toRemove = append(toRemove, n)
						return
					}
				}
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

func (a *Adapter) cleanArticleBodyToParagraphs(bodyHTML string) []string {
	if strings.TrimSpace(bodyHTML) == "" {
		return nil
	}

	// 1. Strip non-article nodes using DOM parsing
	bodyHTML = a.removeNonArticleNodes(bodyHTML)

	// 2. Pre-process loose text before the first block element and convert <br><br> to paragraphs
	bodyHTML = regexp.MustCompile(`(?i)<br\s*/?>\s*<br\s*/?>`).ReplaceAllLiteralString(bodyHTML, "</p><p>")
	trimmed := strings.TrimSpace(bodyHTML)
	if !strings.HasPrefix(trimmed, "<p") && !strings.HasPrefix(trimmed, "<h") && !strings.HasPrefix(trimmed, "<li") && !strings.HasPrefix(trimmed, "<div") && !strings.HasPrefix(trimmed, "<table") {
		idxCloseP := strings.Index(trimmed, "</p>")
		idxOpenP := strings.Index(trimmed, "<p")
		if idxCloseP != -1 && (idxOpenP == -1 || idxCloseP < idxOpenP) {
			trimmed = "<p>" + trimmed
		} else if idxOpenP != -1 {
			trimmed = "<p>" + trimmed[:idxOpenP] + "</p>" + trimmed[idxOpenP:]
		}
		bodyHTML = trimmed
	}

	// 3. Extract block elements in sequence: h1-h6, p, li
	reBlocks := regexp.MustCompile(`(?is)<(h[1-6]|p|li)[^>]*>(.*?)</(?:h[1-6]|p|li)>`)
	matches := reBlocks.FindAllStringSubmatch(bodyHTML, -1)

	var paragraphs []string
	seenParas := make(map[string]bool)

	// If no standard block tags matched, split by <br> or double newlines
	if len(matches) == 0 {
		cleanRaw := a.cleanText(bodyHTML)
		cleanRaw = a.cleanAuthorInitials(cleanRaw)
		if cleanRaw != "" && len(cleanRaw) >= 30 && !a.isBoilerplateLine(cleanRaw) {
			paragraphs = append(paragraphs, cleanRaw)
		}
		return paragraphs
	}

	for _, m := range matches {
		tag := strings.ToLower(m[1])
		rawText := a.cleanText(m[2])
		if rawText == "" {
			continue
		}

		// Strip author signoffs / initials like (suc/up) or [Gambas:Video]
		rawText = a.cleanAuthorInitials(rawText)
		if rawText == "" {
			continue
		}

		// Filter out boilerplate, ads, caption lines, etc.
		if a.isBoilerplateLine(rawText) {
			continue
		}

		// Deduplicate identical consecutive paragraphs
		normPara := strings.ToLower(rawText)
		if seenParas[normPara] && len(paragraphs) > 0 && strings.EqualFold(paragraphs[len(paragraphs)-1], rawText) {
			continue
		}
		seenParas[normPara] = true

		if tag == "li" {
			paragraphs = append(paragraphs, "• "+rawText)
		} else {
			paragraphs = append(paragraphs, rawText)
		}
	}

	// Also check if the very beginning of the body had text outside <p> before the first <p>
	// e.g. "<strong>Jakarta</strong> - Harga masker mendadak melonjak..."
	if len(paragraphs) > 0 {
		// Clean leading location dashes if needed
		paragraphs[0] = strings.TrimSpace(paragraphs[0])
	}

	return paragraphs
}

// extractFullDivContent extracts the complete innerHTML of the div starting at startIdx
// using depth-counting to correctly handle arbitrarily-nested child elements.
// This avoids the truncation caused by lazy-regex matching that stops at the first </div>.
func (a *Adapter) extractFullDivContent(html string, startIdx int) string {
	// Find the position of the opening ">" that ends the opening tag
	openTagEnd := strings.Index(html[startIdx:], ">")
	if openTagEnd == -1 {
		return ""
	}
	openTagEnd += startIdx + 1 // position just after the ">"

	depth := 1
	pos := openTagEnd
	htmlLen := len(html)

	for depth > 0 && pos < htmlLen {
		nextOpen := strings.Index(html[pos:], "<div")
		nextClose := strings.Index(html[pos:], "</div>")

		if nextClose == -1 {
			// No closing tag found — malformed HTML, return what we have
			return html[openTagEnd:pos]
		}

		if nextOpen != -1 && nextOpen < nextClose {
			depth++
			pos += nextOpen + 4 // skip past "<div"
		} else {
			depth--
			if depth == 0 {
				// Return the content between the opening and closing tags
				return html[openTagEnd : pos+nextClose]
			}
			pos += nextClose + 6 // skip past "</div>"
		}
	}

	return ""
}

func (a *Adapter) cleanAuthorInitials(text string) string {
	// Matches: (suc/up), (kna/kna), (up/up), (suc/fds/up), etc. at the end
	reInitial := regexp.MustCompile(`\([a-zA-Z]{2,4}/[a-zA-Z]{2,4}(?:/[a-zA-Z]{2,4})?\)\s*$`)
	text = reInitial.ReplaceAllString(text, "")

	// Matches: [Gambas:Video 20detik], [Gambas:Instagram], etc.
	reGambas := regexp.MustCompile(`(?i)\[Gambas:[^\]]+\]`)
	text = reGambas.ReplaceAllString(text, "")

	return strings.TrimSpace(text)
}

func (a *Adapter) isBoilerplateLine(text string) bool {
	norm := strings.ToLower(strings.TrimSpace(text))

	if len(norm) < 4 {
		return true
	}

	// Exact matches or prefix matches for boilerplate
	if strings.HasPrefix(norm, "baca juga:") ||
		strings.HasPrefix(norm, "baca juga :") ||
		strings.HasPrefix(norm, "simak juga video:") ||
		strings.HasPrefix(norm, "simak video:") ||
		strings.HasPrefix(norm, "foto:") ||
		strings.HasPrefix(norm, "foto :") ||
		strings.HasPrefix(norm, "tonton video:") ||
		strings.HasPrefix(norm, "saksikan video:") ||
		strings.HasPrefix(norm, "scroll to continue with content") ||
		strings.HasPrefix(norm, "advertisement") ||
		strings.HasPrefix(norm, "pilihan editor:") ||
		strings.HasPrefix(norm, "infografis:") && len(norm) < 30 ||
		strings.HasPrefix(norm, "konten selanjutnya") ||
		strings.HasPrefix(norm, "berita terkait") ||
		strings.HasPrefix(norm, "tautan telah disalin") ||
		strings.Contains(norm, "anda menyukai artikel ini") ||
		strings.Contains(norm, "artikel disimpan") {
		return true
	}

	// Photo captions slipping through
	if strings.Contains(norm, "foto: ") && strings.Contains(norm, "detikhealth") && len(norm) < 120 {
		return true
	}

	return false
}

func (a *Adapter) extractJSONLDHeadline(htmlContent string) string {
	reScript := regexp.MustCompile(`(?is)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	for _, m := range reScript.FindAllStringSubmatch(htmlContent, -1) {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(m[1]), &obj); err == nil {
			if hl, ok := obj["headline"].(string); ok && hl != "" {
				return hl
			}
			if name, ok := obj["name"].(string); ok && name != "" {
				return name
			}
		}
	}
	return ""
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

func (a *Adapter) extractJSONLDKeywords(htmlContent string) []string {
	var results []string
	reScript := regexp.MustCompile(`(?is)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	for _, m := range reScript.FindAllStringSubmatch(htmlContent, -1) {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(m[1]), &obj); err == nil {
			if kwStr, ok := obj["keywords"].(string); ok && kwStr != "" {
				for _, p := range strings.Split(kwStr, ",") {
					t := strings.TrimSpace(p)
					if t != "" {
						results = append(results, t)
					}
				}
			}
		}
	}
	return results
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
	if strings.Contains(lower, "logo-detikhealth") ||
		strings.Contains(lower, "logodetikhealth") ||
		strings.Contains(lower, "logo.jpg") ||
		strings.Contains(lower, "logo.png") ||
		strings.Contains(lower, "favicon") ||
		strings.Contains(lower, "icon") ||
		strings.Contains(lower, "banner") ||
		strings.Contains(lower, "pixel") ||
		strings.Contains(lower, "default-169.gif") ||
		strings.Contains(lower, "play-arrow") {
		return false
	}
	return true
}

func (a *Adapter) isValidURL(raw string) bool {
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

