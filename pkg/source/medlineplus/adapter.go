package medlineplus

import (
	"context"
	"encoding/xml"
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
	"maps-scraper/pkg/model"
)

const (
	DefaultBaseURL     = "https://wsearch.nlm.nih.gov/ws/query"
	DefaultSiteURL     = "https://medlineplus.gov"
	DefaultImageURL    = "https://medlineplus.gov/images/medlineplus-share.jpg"
	SourceName         = "NIH MedlinePlus"
)

var (
	htmlTagRegex  = regexp.MustCompile(`<[^>]*>`)
	multiSpaceReg = regexp.MustCompile(`\s+`)
)

// XML Schema structures for MedlinePlus Web Service
type nlmSearchResult struct {
	XMLName xml.Name `xml:"nlmSearchResult"`
	Term    string   `xml:"term"`
	Count   int      `xml:"count"`
	List    listNode `xml:"list"`
}

type listNode struct {
	Documents []documentNode `xml:"document"`
}

type documentNode struct {
	URL      string        `xml:"url,attr"`
	Contents []contentNode `xml:"content"`
}

type contentNode struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",innerxml"`
}

// Adapter implements Trusted Medical Source #1 (NIH MedlinePlus).
type Adapter struct {
	client     *http.Client
	baseURL    string
	classifier *classifier.Classifier
}

// NewAdapter creates a new MedlinePlus source adapter.
func NewAdapter(client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{
			Timeout: 20 * time.Second,
		}
	}
	return &Adapter{
		client:     client,
		baseURL:    DefaultBaseURL,
		classifier: classifier.NewDefaultClassifier(),
	}
}

// Name returns the display name of the trusted medical source.
func (a *Adapter) Name() string {
	return SourceName
}

// BaseURL returns the primary domain URL of the source.
func (a *Adapter) BaseURL() string {
	return DefaultSiteURL
}

// Search queries the MedlinePlus Web Service API for articles on the given medical topic.
// It extracts and normalizes the results into canonical model.Article objects.
func (a *Adapter) Search(ctx context.Context, query string) ([]model.Article, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("search query cannot be empty")
	}

	searchURL := fmt.Sprintf("%s?db=healthTopics&term=%s", a.baseURL, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "MedicalArticleCrawler/1.0 (Educational/Research; Go-HTTP-Client)")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d (%s)", resp.StatusCode, resp.Status)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return a.ParseResponse(bodyBytes)
}

// ParseResponse parses the raw XML response from MedlinePlus into canonical Article models.
func (a *Adapter) ParseResponse(rawXML []byte) ([]model.Article, error) {
	var result nlmSearchResult
	if err := xml.Unmarshal(rawXML, &result); err != nil {
		return nil, fmt.Errorf("malformed XML response: %w", err)
	}

	articles := make([]model.Article, 0, len(result.List.Documents))

	for _, doc := range result.List.Documents {
		art, err := a.extractArticle(doc)
		if err != nil {
			continue // Skip invalid or unparseable items
		}
		articles = append(articles, art)
	}

	return articles, nil
}

func (a *Adapter) extractArticle(doc documentNode) (model.Article, error) {
	var (
		rawTitle    string
		rawSnippet  string
		rawSummary  string
		categories  []string
		imageURL    string
	)

	for _, c := range doc.Contents {
		switch strings.ToLower(c.Name) {
		case "title":
			rawTitle = c.Value
		case "snippet":
			rawSnippet = c.Value
		case "fullsummary":
			rawSummary = c.Value
		case "groupname":
			cleanCat := a.cleanText(c.Value)
			if cleanCat != "" && !contains(categories, cleanCat) {
				categories = append(categories, cleanCat)
			}
		case "mesh":
			cleanMesh := a.cleanText(c.Value)
			if cleanMesh != "" && !contains(categories, cleanMesh) {
				categories = append(categories, cleanMesh)
			}
		case "image", "thumbnail":
			imageURL = strings.TrimSpace(a.cleanText(c.Value))
		}
	}

	// 1. Title Extraction
	title := a.cleanText(rawTitle)
	if title == "" {
		return model.Article{}, errors.New("missing title")
	}

	// 2. Description Extraction (1 to 3 paragraphs)
	var paras []string
	snippetClean := a.cleanText(rawSnippet)
	summaryClean := a.cleanText(rawSummary)

	if snippetClean != "" && len(snippetClean) >= 20 {
		paras = append(paras, snippetClean)
	}

	if summaryClean != "" && len(summaryClean) >= 20 {
		if len(paras) == 0 || paras[0] != summaryClean {
			paras = append(paras, summaryClean)
		}
	}

	if len(paras) == 0 {
		paras = []string{"Comprehensive medical topic overview and patient education from MedlinePlus."}
	}

	if len(paras) > 3 {
		paras = paras[:3]
	}
	for i, p := range paras {
		if len(p) > 500 {
			cutoff := strings.LastIndexAny(p[:500], ".!?")
			if cutoff > 100 {
				paras[i] = p[:cutoff+1]
			} else {
				paras[i] = strings.TrimSpace(p[:500]) + "..."
			}
		}
	}

	// 3. Category Detection using deterministic classifier
	category := a.classifier.Classify(title, strings.Join(paras, " "))
	if category == "" || category == classifier.CategoryGeneralHealth {
		// If title/desc yield General Health, inspect raw categories/MeSH terms
		if len(categories) > 0 {
			if meshCat := a.classifier.Classify(categories[0], ""); meshCat != classifier.CategoryGeneralHealth {
				category = meshCat
			} else if category == "" {
				category = categories[0]
			}
		}
	}
	if category == "" {
		category = classifier.CategoryGeneralHealth
	}

	// 4. Safe Image Handling
	if imageURL == "" {
		imageURL = DefaultImageURL
	}

	return model.Article{
		Title:       title,
		Image:       imageURL,
		Category:    category,
		Description: paras,
	}, nil
}

// cleanText strips XML/HTML markup, unescapes entities, and collapses spaces.
func (a *Adapter) cleanText(raw string) string {
	// Unescape HTML entities first so escaped markup like &lt;span&gt; becomes <span>
	unescaped := html.UnescapeString(raw)
	stripped := htmlTagRegex.ReplaceAllString(unescaped, " ")
	// Second unescape in case of double-escaped entities
	unescaped2 := html.UnescapeString(stripped)
	collapsed := multiSpaceReg.ReplaceAllString(unescaped2, " ")
	return strings.TrimSpace(collapsed)
}

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, val) {
			return true
		}
	}
	return false
}
