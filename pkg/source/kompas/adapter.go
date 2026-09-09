package kompas

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

	xhtml "golang.org/x/net/html"

	"maps-scraper/pkg/classifier"
	"maps-scraper/pkg/cleaner"
	"maps-scraper/pkg/model"
)

const (
	DefaultBaseURL = "https://health.kompas.com"
	SourceName     = "Kompas Health"
)

var (
	h1Regex       = regexp.MustCompile(`(?is)<h1[^>]*class=["']read__title["'][^>]*>(.*?)</h1>|<h1[^>]*>(.*?)</h1>`)
	keywordsRegex = regexp.MustCompile(`(?i)<meta\s+name=["']keywords["']\s+content=["']([^"']+)["']`)
	ogImageRegex  = regexp.MustCompile(`(?i)<meta\s+(?:property|name)=["']og:image["']\s+content=["']([^"']+)["']`)
	htmlTagRegex  = regexp.MustCompile(`<[^>]*>`)
)

type Adapter struct {
	client     *http.Client
	baseURL    string
	sourceURL  string
	classifier *classifier.Classifier
	cleaner    *cleaner.ArticleCleaner
}

func NewAdapter(client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{
			Timeout: 60 * time.Second,
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

func (a *Adapter) Name() string {
	return SourceName
}

func (a *Adapter) BaseURL() string {
	return a.baseURL
}

func (a *Adapter) SetSourceURL(sourceURL string) {
	if sourceURL != "" {
		a.sourceURL = sourceURL
		if u, err := url.Parse(sourceURL); err == nil && u.Host != "" {
			a.baseURL = u.Scheme + "://" + u.Host
		}
	}
}

func (a *Adapter) Search(ctx context.Context, query string) ([]model.Article, error) {
	return a.SearchWithLimit(ctx, query, 100) // Default arbitrary limit if not specified
}

func (a *Adapter) SearchWithLimit(ctx context.Context, query string, maxArticles int) ([]model.Article, error) {
	if query == "" {
		return nil, errors.New("search query cannot be empty")
	}
	
	if strings.ToLower(query) == "all" || strings.ToLower(query) == "all topics" {
		query = "kesehatan" // Fallback to general health search
	}

	searchPage := 1
	var articles []model.Article
	seenURLs := make(map[string]bool)

	for len(articles) < maxArticles {
		select {
		case <-ctx.Done():
			return articles, ctx.Err()
		default:
		}

		searchEndpoint := fmt.Sprintf("https://health.kompas.com/search?q=%s&page=%d", url.QueryEscape(query), searchPage)
		htmlContent, err := a.fetchHTML(ctx, searchEndpoint)
		if err != nil {
			break
		}

		urls := a.DiscoverArticleURLs(htmlContent, a.baseURL)
		if len(urls) == 0 {
			break // No more results
		}

		addedOnPage := 0
		for _, articleURL := range urls {
			if len(articles) >= maxArticles {
				break
			}

			if seenURLs[articleURL] {
				continue
			}
			seenURLs[articleURL] = true

			art, err := a.FetchAndParse(ctx, articleURL, "")
			if err == nil && art.Title != "" {
				articles = append(articles, art)
				addedOnPage++
			}
		}

		if addedOnPage == 0 {
			break
		}
		searchPage++
	}

	return articles, nil
}

func (a *Adapter) DiscoverArticleURLs(htmlContent, baseURL string) []string {
	var urls []string
	
	// Kompas search results usually have class="article__link"
	linkRegex := regexp.MustCompile(`(?i)<a[^>]*class=["'][^"']*article__link[^"']*["'][^>]*href=["']([^"']+)["']`)
	matches := linkRegex.FindAllStringSubmatch(htmlContent, -1)
	
	for _, m := range matches {
		if len(m) > 1 {
			u := m[1]
			if strings.Contains(u, "health.kompas.com/read") {
				// Normalize URL to remove tracking parameters
				parsed, err := url.Parse(u)
				if err == nil {
					parsed.RawQuery = ""
					urls = append(urls, parsed.String())
				}
			}
		}
	}
	
	return urls
}

func (a *Adapter) fetchHTML(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func (a *Adapter) FetchAndParse(ctx context.Context, pageURL, defaultTitle string) (model.Article, error) {
	// Append ?page=all to get the full article
	fullPageURL := pageURL
	if !strings.Contains(fullPageURL, "page=all") {
		if strings.Contains(fullPageURL, "?") {
			fullPageURL += "&page=all"
		} else {
			fullPageURL += "?page=all"
		}
	}
	
	htmlContent, err := a.fetchHTML(ctx, fullPageURL)
	if err != nil {
		return model.Article{}, err
	}

	return a.ParseArticleHTML(htmlContent, pageURL, defaultTitle)
}

func (a *Adapter) ParseArticleHTML(htmlContent, pageURL, defaultTitle string) (model.Article, error) {
	var art model.Article
	art.SourceURL = pageURL
	art.ScrapedAt = time.Now().UTC().Format(time.RFC3339)

	// Extract Title
	if m := h1Regex.FindStringSubmatch(htmlContent); len(m) > 0 {
		if m[1] != "" {
			art.Title = html.UnescapeString(m[1])
		} else if len(m) > 2 && m[2] != "" {
			art.Title = html.UnescapeString(m[2])
		}
	}
	if art.Title == "" {
		art.Title = defaultTitle
	}
	art.Title = a.cleanText(art.Title)

	// Extract Image
	if m := ogImageRegex.FindStringSubmatch(htmlContent); len(m) > 1 {
		art.Image = html.UnescapeString(m[1])
	}

	// Extract Category from keywords
	if m := keywordsRegex.FindStringSubmatch(htmlContent); len(m) > 1 {
		art.Category = html.UnescapeString(m[1])
	}
	
	if art.Category == "" {
		art.Category = "Health"
	}

	// Extract Content
	bodyHTML := a.extractArticleBodyHTML(htmlContent)
	bodyHTML = a.removeNonArticleNodes(bodyHTML)
	paras := a.cleanArticleBodyToParagraphs(bodyHTML)
	
	art.Description = paras
	
	// Generate deterministic ID
	art.ID = model.GenerateArticleID(art.Title, pageURL)

	return art, nil
}

func (a *Adapter) extractArticleBodyHTML(htmlContent string) string {
	doc, err := xhtml.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return ""
	}
	
	var bodyNode *xhtml.Node
	var findBody func(*xhtml.Node)
	findBody = func(n *xhtml.Node) {
		if bodyNode != nil {
			return
		}
		if n.Type == xhtml.ElementNode && n.Data == "div" {
			for _, attr := range n.Attr {
				if attr.Key == "class" && strings.Contains(attr.Val, "read__content") {
					bodyNode = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findBody(c)
		}
	}
	findBody(doc)
	
	if bodyNode != nil {
		var buf strings.Builder
		xhtml.Render(&buf, bodyNode)
		return buf.String()
	}
	return ""
}

func (a *Adapter) removeNonArticleNodes(bodyHTML string) string {
	doc, err := xhtml.Parse(strings.NewReader(bodyHTML))
	if err != nil {
		return bodyHTML
	}
	
	var clean func(*xhtml.Node)
	clean = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			// Remove scripts, iframes
			if n.Data == "script" || n.Data == "iframe" || n.Data == "style" {
				n.Data = "span" // neutral element
				n.FirstChild = nil
			}
			
			// Remove ads, recommendations, and "Baca juga"
			for _, attr := range n.Attr {
				if attr.Key == "class" {
					classes := attr.Val
					if strings.Contains(classes, "ads-on-body") || 
					   strings.Contains(classes, "kompasidRec") ||
					   strings.Contains(classes, "inner-link-baca-juga") {
						n.Data = "span"
						n.FirstChild = nil
					}
				}
			}
			
			// Handle strong nodes containing Baca juga
			if n.Data == "strong" || n.Data == "b" {
				text := a.getNodeText(n)
				if strings.Contains(strings.ToLower(text), "baca juga") || strings.Contains(strings.ToLower(text), "baca juga:") {
					n.Data = "span"
					n.FirstChild = nil
				}
			}
			
			// Same for paragraphs that just say "Baca juga:" or "Simak Video"
			if n.Data == "p" {
				text := a.getNodeText(n)
				lower := strings.ToLower(text)
				if strings.HasPrefix(lower, "baca juga") || strings.HasPrefix(lower, "simak video") {
					n.Data = "span"
					n.FirstChild = nil
				}
			}
		}
		
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			clean(c)
		}
	}
	
	clean(doc)
	
	var buf strings.Builder
	xhtml.Render(&buf, doc)
	return buf.String()
}

func (a *Adapter) cleanArticleBodyToParagraphs(bodyHTML string) []string {
	doc, err := xhtml.Parse(strings.NewReader(bodyHTML))
	if err != nil {
		return nil
	}
	
	var paras []string
	
	var traverse func(*xhtml.Node)
	traverse = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			if n.Data == "p" || n.Data == "h2" || n.Data == "h3" || n.Data == "blockquote" || n.Data == "li" {
				text := a.cleanText(a.getNodeText(n))
				if text != "" && !strings.HasPrefix(strings.ToLower(text), "baca juga") {
					paras = append(paras, text)
				}
				return // do not traverse children of these block elements to avoid duplication
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}
	
	traverse(doc)
	return paras
}

func (a *Adapter) getNodeText(n *xhtml.Node) string {
	if n.Type == xhtml.TextNode {
		return n.Data
	}
	if n.Type == xhtml.ElementNode && (n.Data == "script" || n.Data == "style") {
		return ""
	}
	var buf strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		buf.WriteString(a.getNodeText(c))
	}
	return buf.String()
}

func (a *Adapter) cleanText(s string) string {
	s = html.UnescapeString(s)
	s = htmlTagRegex.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\u00a0", " ") // non-breaking space
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
