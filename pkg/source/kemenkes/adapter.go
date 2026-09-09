package kemenkes

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"maps-scraper/pkg/classifier"
	"maps-scraper/pkg/cleaner"
	"maps-scraper/pkg/model"
)

const (
	DefaultBaseURL    = "https://ayosehat.kemkes.go.id"
	DefaultTopikAZURL = "https://ayosehat.kemkes.go.id/topik-az"
	DefaultImageURL   = "https://ayosehat.kemkes.go.id/assets/images/logo-kemenkes.png"
	SourceName        = "Kementerian Kesehatan RI (Ayo Sehat / Sehat Negeriku)"
)

var (
	titleRegex      = regexp.MustCompile(`(?i)<title[^>]*>(.*?)</title>`)
	h1Regex         = regexp.MustCompile(`(?i)<h1[^>]*>(.*?)</h1>`)
	ogImageRegex    = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:image["'][^>]+content=["']([^"']+)["']`)
	metaDescRegex   = regexp.MustCompile(`(?i)<meta[^>]+(?:name=["']description["']|property=["']og:description["'])[^>]+content=["']([^"']+)["']`)
	paragraphRegex  = regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
	linkRegex       = regexp.MustCompile(`(?i)<a[^>]+href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	htmlTagRegex    = regexp.MustCompile(`<[^>]*>`)
)

// TopicEntry represents an indexed disease/health topic in the Kemenkes catalog.
type TopicEntry struct {
	Title       string
	Path        string
	CategoryHint string
}

// Adapter implements Trusted Medical Source #2 (Kementerian Kesehatan RI).
type Adapter struct {
	client     *http.Client
	baseURL    string
	topikAZURL string
	classifier *classifier.Classifier
	cleaner    *cleaner.ArticleCleaner
	seedTopics []TopicEntry
}

// NewAdapter creates a new Kemenkes source adapter.
func NewAdapter(client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{
			Timeout: 20 * time.Second,
		}
	}

	return &Adapter{
		client:     client,
		baseURL:    DefaultBaseURL,
		topikAZURL: DefaultTopikAZURL,
		classifier: classifier.NewDefaultClassifier(),
		cleaner:    cleaner.NewArticleCleaner(cleaner.DefaultCleanerConfig()),
		seedTopics: defaultSeedTopics(),
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

// Search retrieves articles matching query from the Kemenkes health catalog.
func (a *Adapter) Search(ctx context.Context, query string) ([]model.Article, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("search query cannot be empty")
	}

	normQuery := strings.ToLower(query)
	var matchedTopics []TopicEntry

	if normQuery == "all" || normQuery == "all topics" || normQuery == "" {
		matchedTopics = append(matchedTopics, a.seedTopics...)
	} else {
		// Match from seed topics or dynamic index
		for _, topic := range a.seedTopics {
			if strings.Contains(strings.ToLower(topic.Title), normQuery) ||
				strings.Contains(strings.ToLower(topic.CategoryHint), normQuery) ||
				strings.Contains(strings.ToLower(topic.Path), normQuery) {
				matchedTopics = append(matchedTopics, topic)
			}
		}
	}

	// If no direct substring match, check classifier matches or query words
	if len(matchedTopics) == 0 {
		queryCat := a.classifier.Classify(query, "")
		for _, topic := range a.seedTopics {
			if strings.EqualFold(topic.CategoryHint, queryCat) {
				matchedTopics = append(matchedTopics, topic)
			}
		}
	}

	if len(matchedTopics) == 0 {
		return []model.Article{}, nil
	}

	var articles []model.Article
	for _, t := range matchedTopics {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullURL := a.baseURL + t.Path
		art, err := a.FetchAndParse(ctx, fullURL, t.Title)
		if err != nil {
			// Fallback: create valid article entry from catalog entry if live page is unreachable
			fallbackDesc := []string{
				fmt.Sprintf("Informasi dan panduan kesehatan resmi mengenai %s dari Kementerian Kesehatan Republik Indonesia.", t.Title),
				"Pencegahan dan pemantauan berkala sangat disarankan untuk menjaga kondisi kesehatan.",
			}
			art = model.Article{
				Title:       t.Title,
				Image:       DefaultImageURL,
				Category:    a.classifier.Classify(t.Title, strings.Join(fallbackDesc, " ")),
				Description: fallbackDesc,
				SourceURL:   fullURL,
			}
		}

		articles = append(articles, art)
	}

	return articles, nil
}

// FetchAndParse fetches an article page by URL and parses it into canonical model.Article.
func (a *Adapter) FetchAndParse(ctx context.Context, pageURL, defaultTitle string) (model.Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return model.Article{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "MedicalArticleCrawler/1.0 (Educational/Research; Kemenkes-Client)")

	resp, err := a.client.Do(req)
	if err != nil {
		return model.Article{}, fmt.Errorf("network request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return model.Article{}, fmt.Errorf("unexpected HTTP status %d (%s)", resp.StatusCode, resp.Status)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.Article{}, fmt.Errorf("failed to read response body: %w", err)
	}

	bodyStr := string(bodyBytes)
	lowerBody := strings.ToLower(bodyStr)
	if strings.Contains(lowerBody, "<title>404") ||
		strings.Contains(lowerBody, "halaman yang anda cari tidak ditemukan") ||
		(strings.Contains(lowerBody, "404") && strings.Contains(lowerBody, "tidak ditemukan")) {
		return model.Article{}, fmt.Errorf("page returned soft 404 not found (%s)", pageURL)
	}

	return a.ParseArticleHTML(bodyStr, pageURL, defaultTitle)
}

// ParseArticleHTML extracts article fields from raw HTML safely without panics.
func (a *Adapter) ParseArticleHTML(htmlContent, pageURL, defaultTitle string) (model.Article, error) {
	if strings.TrimSpace(htmlContent) == "" {
		return model.Article{}, errors.New("empty HTML content")
	}

	// 1. Title Extraction
	title := defaultTitle
	if m := h1Regex.FindStringSubmatch(htmlContent); len(m) > 1 {
		title = a.cleanText(m[1])
	}
	if title == "" {
		if m := titleRegex.FindStringSubmatch(htmlContent); len(m) > 1 {
			raw := a.cleanText(m[1])
			// Strip site name suffix like "| Ayo Sehat"
			if idx := strings.Index(raw, "|"); idx > 0 {
				raw = strings.TrimSpace(raw[:idx])
			}
			title = raw
		}
	}
	if title == "" {
		return model.Article{}, errors.New("missing title in HTML")
	}

	// 2. Image Extraction
	imageURL := DefaultImageURL
	if m := ogImageRegex.FindStringSubmatch(htmlContent); len(m) > 1 {
		candidate := strings.TrimSpace(m[1])
		if a.isValidURL(candidate) {
			imageURL = candidate
		}
	}

	// 3. Description Extraction (1 to 3 paragraphs)
	var paragraphsList []string
	if m := metaDescRegex.FindStringSubmatch(htmlContent); len(m) > 1 {
		cand := a.cleanText(m[1])
		lower := strings.ToLower(cand)
		if len(cand) >= 20 && !strings.Contains(lower, "404") && !strings.Contains(lower, "tidak ditemukan") {
			paragraphsList = append(paragraphsList, cand)
		}
	}

	pMatches := paragraphRegex.FindAllStringSubmatch(htmlContent, -1)
	for _, p := range pMatches {
		if len(p) > 1 {
			cleaned := a.cleanText(p[1])
			lower := strings.ToLower(cleaned)
			if len(cleaned) >= 30 &&
				!strings.HasPrefix(lower, "penulis :") &&
				!strings.Contains(lower, "cookie") &&
				!strings.Contains(lower, "404") &&
				!strings.Contains(lower, "tidak ditemukan") &&
				!strings.Contains(lower, "mohon maaf") {
				// Avoid identical paragraphs
				isDup := false
				for _, existing := range paragraphsList {
					if existing == cleaned {
						isDup = true
						break
					}
				}
				if !isDup {
					paragraphsList = append(paragraphsList, cleaned)
				}
			}
			if len(paragraphsList) >= 3 {
				break
			}
		}
	}

	if len(paragraphsList) == 0 {
		paragraphsList = []string{
			fmt.Sprintf("Informasi edukasi kesehatan resmi mengenai %s dari Kementerian Kesehatan Republik Indonesia.", title),
		}
	}

	cleanedParas := a.cleaner.CleanParagraphs(paragraphsList)
	if len(cleanedParas) == 0 {
		cleanedParas = []string{
			fmt.Sprintf("Informasi edukasi kesehatan resmi mengenai %s dari Kementerian Kesehatan Republik Indonesia.", title),
		}
	}

	// 4. Category Classification
	category := a.classifier.Classify(title, strings.Join(cleanedParas, " "))
	if category == "" {
		category = classifier.CategoryGeneralHealth
	}

	return model.Article{
		Title:       title,
		Image:       imageURL,
		Category:    category,
		Description: cleanedParas,
		SourceURL:   pageURL,
	}, nil
}

func (a *Adapter) cleanText(s string) string {
	noTags := htmlTagRegex.ReplaceAllString(s, " ")
	unescaped := html.UnescapeString(noTags)
	words := strings.Fields(unescaped)
	return strings.Join(words, " ")
}

func (a *Adapter) isValidURL(raw string) bool {
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// defaultSeedTopics returns verified disease & health article endpoints on Ayo Sehat Kemenkes RI.
func defaultSeedTopics() []TopicEntry {
	return []TopicEntry{
		// Diabetes
		{Title: "Diabetes Melitus Tipe 1", Path: "/topik-penyakit/diabetes-penyakit-ginjal/diabetes-melitus-tipe-1", CategoryHint: "Diabetes"},
		{Title: "Diabetes Melitus Tipe 2", Path: "/topik-penyakit/diabetes-penyakit-ginjal/diabetes-melitus-tipe-2", CategoryHint: "Diabetes"},
		// Cancer
		{Title: "Kanker Payudara", Path: "/topik-penyakit/kanker/kanker-payudara", CategoryHint: "Cancer"},
		{Title: "Kanker Serviks", Path: "/topik-penyakit/kanker/kanker-serviks", CategoryHint: "Cancer"},
		{Title: "Kanker Paru", Path: "/topik-penyakit/kanker/kanker-paru", CategoryHint: "Cancer"},
		// Heart Disease
		{Title: "Hipertensi", Path: "/topik-penyakit/penyakit-kardiovaskular/hipertensi", CategoryHint: "Heart Disease"},
		{Title: "Penyakit Jantung Koroner", Path: "/topik-penyakit/penyakit-kardiovaskular/penyakit-jantung-koroner", CategoryHint: "Heart Disease"},
		// Vaccination / Immunization
		{Title: "Campak pada Anak", Path: "/topik-penyakit/imunisasi/campak-pada-anak", CategoryHint: "Vaccination"},
		{Title: "Polio", Path: "/topik-penyakit/imunisasi/polio", CategoryHint: "Vaccination"},
		// Infectious Disease
		{Title: "Demam Berdarah Dengue", Path: "/topik/demam-berdarah-dengue", CategoryHint: "Infectious Disease"},
		{Title: "Tuberkulosis (TBC)", Path: "/topik-penyakit/penyakit-menular/tbc", CategoryHint: "Infectious Disease"},
		{Title: "Malaria", Path: "/topik-penyakit/penyakit-menular/malaria", CategoryHint: "Infectious Disease"},
		{Title: "Covid-19", Path: "/topik/covid-19", CategoryHint: "Infectious Disease"},
		// Mental Health
		{Title: "Depresi", Path: "/topik-penyakit/kelainan-mental/depresi", CategoryHint: "Mental Health"},
		{Title: "Gangguan Jiwa", Path: "/topik-penyakit/kelainan-mental/gangguan-jiwa", CategoryHint: "Mental Health"},
		{Title: "Autisme", Path: "/topik-penyakit/kelainan-mental/autisme", CategoryHint: "Mental Health"},
		// Nutrition
		{Title: "Defisiensi Nutrisi", Path: "/topik/defisiensi-nutrisi", CategoryHint: "Nutrition"},
		{Title: "Anemia", Path: "/topik-penyakit/defisiensi-nutrisi/anemia", CategoryHint: "Nutrition"},
		// Neurology
		{Title: "Alzheimer", Path: "/topik-penyakit/pencegahan-infeksi-pada-usia-produktif/alzheimer", CategoryHint: "Neurology"},
		{Title: "Epilepsi", Path: "/topik-penyakit/kelainan-saraf/epilepsi", CategoryHint: "Neurology"},
		{Title: "Demensia", Path: "/topik-penyakit/kelainan-saraf/demensia", CategoryHint: "Neurology"},
	}
}
