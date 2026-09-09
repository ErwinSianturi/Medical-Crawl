package cleaner

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"maps-scraper/pkg/model"
)

var (
	// HTML and markup patterns
	scriptRegex    = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRegex     = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	commentRegex   = regexp.MustCompile(`(?is)<!--.*?-->`)
	htmlTagRegex   = regexp.MustCompile(`<[^>]*>`)
	multiSpaceReg    = regexp.MustCompile(`\s+`)
	punctSpaceReg    = regexp.MustCompile(`\s+([.,;:!?])`)
	breadcrumbReg    = regexp.MustCompile(`(?i)\b(?:home|health topics|news|articles)\s*(?:[>/»|]\s*)+[^.\n!]+(?:[.!\n]|$)`)
	navigationReg    = regexp.MustCompile(`(?i)\b(?:skip to (?:main )?content|back to top|click here to read more|read (?:full )?article|continue reading|table of contents)\b[:\s\-.]*`)
	advertisementReg = regexp.MustCompile(`(?i)\b(?:advertisement|sponsored content|promoted story|advertorial)\b[:\s\-.]*[^.!\n]*(?:[.!\n]|$)`)
	cookieReg        = regexp.MustCompile(`(?i)\b(?:this website uses cookies|we use cookies to|accept (?:all )?cookies|cookie policy|manage consent|manage cookies|consent preferences)[^.!\n]*(?:[.!\n]|$)`)
	datelineRegex    = regexp.MustCompile(`(?i)^(?:[a-zA-Z\s.,]+,\s*)?(?:kompas\.com|kompas|detikhealth|detikcom|detik\.com|ayosehat|kemenkes|antaranews|antara)(?:,\s*[a-zA-Z\s]+)?\s*[-–—:]\s*`)
	donationRegex    = regexp.MustCompile(`(?i)\b(?:mengulurkan tangan untuk membantu|kirim bantuan anda|salurkan bantuan anda|meringankan beban yang sedang mereka hadapi|tengah dirundung duka|bantuwarga|kitabisa\.com|bit\.ly/(?:bantu|peduli|donasi)|dapatkan update berita pilihan|gabung kompas\.com plus)\b`)

	ErrEmptyTitle       = errors.New("article title cannot be empty")
	ErrTitleTooShort    = errors.New("article title is too short")
	ErrEmptyDescription = errors.New("article description cannot be empty")
	ErrDescTooShort     = errors.New("article description is too short")
	ErrEmptyCategory    = errors.New("article category cannot be empty")
	ErrEmptyImage       = errors.New("article image URL cannot be empty")
	ErrInvalidImageURL  = errors.New("article image URL is invalid or malformed")
)

// CleanerConfig defines parameters for article text sanitization and validation.
type CleanerConfig struct {
	MinTitleLen int `json:"min_title_len"`
	MinDescLen  int `json:"min_desc_len"`
}

// DefaultCleanerConfig provides production standard cleaning thresholds.
func DefaultCleanerConfig() CleanerConfig {
	return CleanerConfig{
		MinTitleLen: 5,
		MinDescLen:  10,
	}
}

// ArticleCleaner handles deterministic text cleaning, HTML stripping, encoding correction, and validation.
type ArticleCleaner struct {
	cfg CleanerConfig
}

// NewArticleCleaner creates an initialized ArticleCleaner.
func NewArticleCleaner(cfg CleanerConfig) *ArticleCleaner {
	if cfg.MinTitleLen <= 0 {
		cfg.MinTitleLen = 5
	}
	if cfg.MinDescLen <= 0 {
		cfg.MinDescLen = 10
	}
	return &ArticleCleaner{cfg: cfg}
}

// CleanAndValidate sanitizes all fields of raw Article and verifies required constraints.
func (c *ArticleCleaner) CleanAndValidate(raw *model.Article) (*model.Article, error) {
	if raw == nil {
		return nil, errors.New("nil article provided")
	}

	cleanTitle := c.CleanTitle(raw.Title)
	if cleanTitle == "" {
		return nil, ErrEmptyTitle
	}
	if len(cleanTitle) < c.cfg.MinTitleLen {
		return nil, fmt.Errorf("%w (min %d chars, got %d)", ErrTitleTooShort, c.cfg.MinTitleLen, len(cleanTitle))
	}

	cleanDesc := c.CleanParagraphs(raw.Description)
	if len(cleanDesc) == 0 {
		return nil, ErrEmptyDescription
	}

	totalDescLen := 0
	for _, p := range cleanDesc {
		totalDescLen += len(p)
	}
	if totalDescLen < c.cfg.MinDescLen {
		return nil, fmt.Errorf("%w (min %d chars, got %d)", ErrDescTooShort, c.cfg.MinDescLen, totalDescLen)
	}

	cleanImage := c.CleanImageURL(raw.Image)
	if cleanImage == "" {
		cleanImage = "-"
	} else if cleanImage != "-" {
		if err := c.ValidateImageURL(cleanImage); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidImageURL, err)
		}
	}

	cleanCategory := c.CleanCategory(raw.Category)

	return &model.Article{
		ID:           raw.ID,
		Title:        cleanTitle,
		Image:        cleanImage,
		Category:     cleanCategory,
		Description:  cleanDesc,
		SourceURL:    raw.SourceURL,
		ScrapedAt:    raw.ScrapedAt,
		CrawlSession: raw.CrawlSession,
	}, nil
}

// CleanTitle normalizes article titles by stripping tags, breadcrumbs, and extra spacing.
func (c *ArticleCleaner) CleanTitle(title string) string {
	text := c.stripHTML(title)
	text = c.fixEncodingAndPunctuation(text)
	text = breadcrumbReg.ReplaceAllString(text, "")
	text = navigationReg.ReplaceAllString(text, "")
	text = advertisementReg.ReplaceAllString(text, "")
	text = c.collapseWhitespace(text)
	return strings.Trim(text, " -|/:\t\r\n")
}

// CleanDescription normalizes a single description paragraph, strips boilerplate/ads/cookies, and removes duplicated text.
func (c *ArticleCleaner) CleanDescription(desc string) string {
	text := c.stripHTML(desc)
	text = c.fixEncodingAndPunctuation(text)
	text = breadcrumbReg.ReplaceAllString(text, "")
	text = navigationReg.ReplaceAllString(text, "")
	text = advertisementReg.ReplaceAllString(text, "")
	text = cookieReg.ReplaceAllString(text, "")
	norm := strings.ToLower(text)
	if regexp.MustCompile(`(?i)\bdiakses pada \d{4}\b`).MatchString(norm) ||
		donationRegex.MatchString(norm) ||
		(strings.Contains(norm, "bantuan") && (strings.Contains(norm, "bit.ly/") || strings.Contains(norm, "tautan") || strings.Contains(norm, "rekening"))) ||
		(strings.Contains(norm, "donasi") && (strings.Contains(norm, "bit.ly/") || strings.Contains(norm, "tautan") || strings.Contains(norm, "rekening"))) ||
		(strings.Contains(norm, "saudara-saudara kita") && strings.Contains(norm, "duka")) ||
		strings.Contains(norm, "jadwalkan sesi konsultasi") ||
		strings.Contains(norm, "download aplikasi halodoc") ||
		strings.Contains(norm, "kenapa harus chat dokter") ||
		strings.Contains(norm, "kenapa beli obat di halodoc") ||
		strings.Contains(norm, "toko kesehatan halodoc produknya") ||
		strings.Contains(norm, "official whatsapp halodoc") ||
		strings.Contains(norm, "beli obat tinggal chat di whatsapp") ||
		strings.Contains(norm, "pharmacy delivery halodoc") ||
		strings.Contains(norm, "tebus resep. melalui fitur tebus resep") ||
		strings.Contains(norm, "surat izin apotek") ||
		strings.Contains(norm, "dokter tersebut tersedia selama 24 jam di halodoc") ||
		strings.Contains(norm, "tanya hilda") ||
		strings.Contains(norm, "hilda (halodoc intelligent") ||
		strings.Contains(norm, "kenalin hilda") ||
		strings.Contains(norm, "hilda bukan cuma asisten biasa") {
		return ""
	}
	cleanTrim := strings.Trim(norm, " :*#\t\r\n")
	if cleanTrim == "daftar isi" || cleanTrim == "table of contents" || cleanTrim == "table of content" || cleanTrim == "toc" ||
		strings.HasPrefix(cleanTrim, "daftar isi ") || strings.HasPrefix(cleanTrim, "table of contents ") {
		return ""
	}
	text = datelineRegex.ReplaceAllString(text, "")
	text = c.collapseWhitespace(text)
	text = c.deduplicateConsecutiveText(text)
	return strings.TrimSpace(text)
}

// CleanParagraphs cleans a slice of description paragraphs.
// It retains all valid, substantive paragraphs while filtering boilerplate and TOC remnants.
func (c *ArticleCleaner) CleanParagraphs(paras []string) []string {
	var cleanedList []string
	for _, p := range paras {
		cleaned := c.CleanDescription(p)
		if len(cleaned) >= 15 {
			cleanedList = append(cleanedList, cleaned)
		}
	}

	// Remove leading TOC bullet points if they match headings in the rest of the text
	cleanedList = c.removeLeadingTOCRemnants(cleanedList)

	// Remove trailing video recommendation widgets or duplicate video titles
	cleanedList = c.removeTrailingVideoRecommendations(cleanedList)

	// If no paragraphs reached threshold but input had some text, salvage the cleaned text
	if len(cleanedList) == 0 && len(paras) > 0 {
		for _, p := range paras {
			cleaned := c.CleanDescription(p)
			if cleaned != "" {
				cleanedList = append(cleanedList, cleaned)
				break
			}
		}
	}

	return cleanedList
}

func (c *ArticleCleaner) removeTrailingVideoRecommendations(paras []string) []string {
	if len(paras) <= 1 {
		return paras
	}

	// 1. Remove trailing duplicate paragraphs at the end of article
	for len(paras) >= 2 {
		lastIdx := len(paras) - 1
		prevIdx := len(paras) - 2
		if strings.EqualFold(paras[lastIdx], paras[prevIdx]) && strings.HasPrefix(strings.ToLower(paras[lastIdx]), "video ") {
			paras = paras[:lastIdx]
		} else {
			break
		}
	}

	// 2. Remove isolated trailing "Video ..." recommendation headers right after signoff paragraphs ("pungkas...", "jelas...", etc.)
	if len(paras) >= 2 {
		lastIdx := len(paras) - 1
		lastLower := strings.ToLower(paras[lastIdx])
		if strings.HasPrefix(lastLower, "video ") && !strings.Contains(lastLower, ".") {
			// Check if previous paragraph ends with a signoff like "pungkas...", "jelas...", "katanya."
			prevLower := strings.ToLower(paras[lastIdx-1])
			if strings.Contains(prevLower, "pungkas") || strings.Contains(prevLower, "ujarnya") ||
				strings.Contains(prevLower, "tuturnya") || strings.Contains(prevLower, "tutupnya") ||
				strings.Contains(prevLower, "katanya") || strings.Contains(prevLower, "jelasnya") {
				paras = paras[:lastIdx]
			}
		}
	}

	return paras
}

func (c *ArticleCleaner) removeLeadingTOCRemnants(paras []string) []string {
	if len(paras) == 0 {
		return paras
	}

	bulletCount := 0
	for i := 0; i < len(paras) && i < 15; i++ {
		if strings.HasPrefix(paras[i], "•") {
			bulletCount++
		} else {
			break
		}
	}

	if bulletCount >= 1 {
		foundLater := false
		for i := 0; i < bulletCount; i++ {
			bText := strings.TrimPrefix(paras[i], "• ")
			bText = strings.TrimSpace(bText)
			if len(bText) > 4 {
				for j := bulletCount; j < len(paras); j++ {
					if strings.Contains(strings.ToLower(paras[j]), strings.ToLower(bText)) {
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

// SplitAndCleanDescription takes raw text (possibly containing multiple paragraphs or HTML breaks)
// and returns a normalized slice of cleaned paragraphs.
func (c *ArticleCleaner) SplitAndCleanDescription(raw string) []string {
	// Replace paragraph and break tags with double newlines
	s := strings.ReplaceAll(raw, "</p>", "\n\n")
	s = strings.ReplaceAll(s, "<br>", "\n\n")
	s = strings.ReplaceAll(s, "<br/>", "\n\n")
	s = strings.ReplaceAll(s, "<br />", "\n\n")

	parts := strings.Split(s, "\n\n")
	var rawParas []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			rawParas = append(rawParas, trimmed)
		}
	}

	if len(rawParas) == 0 && strings.TrimSpace(raw) != "" {
		rawParas = []string{raw}
	}

	return c.CleanParagraphs(rawParas)
}

// CleanCategory standardizes category naming while preserving taxonomy and acronyms.
func (c *ArticleCleaner) CleanCategory(category string) string {
	clean := c.CleanTitle(category)
	if clean == "" {
		return ""
	}
	parts := strings.Split(clean, ",")
	var cleanedParts []string
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p != "" {
			// If all lowercase, title-case it
			if p == strings.ToLower(p) {
				p = strings.Title(p)
			}
			cleanedParts = append(cleanedParts, p)
		}
	}
	if len(cleanedParts) > 1 {
		return strings.Join(cleanedParts, " , ")
	}
	if len(cleanedParts) == 1 {
		return cleanedParts[0]
	}
	return clean
}

// CleanImageURL normalizes image URLs and strips invalid/unsupported trailing .webp suffixes from CDN media URLs.
func (c *ArticleCleaner) CleanImageURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" || trimmed == "-" {
		return trimmed
	}

	lower := strings.ToLower(trimmed)
	if strings.HasSuffix(lower, ".webp") {
		if strings.HasSuffix(lower, ".jpg.webp") ||
			strings.HasSuffix(lower, ".jpeg.webp") ||
			strings.HasSuffix(lower, ".png.webp") ||
			strings.Contains(lower, "cloudfront.net") ||
			strings.Contains(lower, "halodoc.com") {
			return strings.TrimSuffix(trimmed, ".webp")
		}
	}

	return trimmed
}

// ValidateImageURL checks if an image URL is well-formed with a valid HTTP/HTTPS scheme and host.
// It explicitly accepts "-" as an indicator that no image is available.
func (c *ArticleCleaner) ValidateImageURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return errors.New("empty URL")
	}
	if trimmed == "-" {
		return nil
	}
	if strings.HasPrefix(trimmed, "gambar/") {
		return nil
	}

	u, err := url.ParseRequestURI(trimmed)
	if err != nil {
		return err
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme: %q", scheme)
	}

	host := strings.ToLower(u.Hostname())
	if host == "" || !strings.Contains(host, ".") || host == "localhost" {
		return fmt.Errorf("invalid host: %q", host)
	}

	return nil
}

// stripHTML removes script, style, comments, tags, and decodes HTML entities.
func (c *ArticleCleaner) stripHTML(s string) string {
	// 1. Remove script and style blocks
	s = scriptRegex.ReplaceAllString(s, " ")
	s = styleRegex.ReplaceAllString(s, " ")
	s = commentRegex.ReplaceAllString(s, " ")

	// 2. Decode HTML entities (unescape twice to resolve double-escaped entities like &amp;lt;)
	s = html.UnescapeString(s)
	if strings.Contains(s, "&") {
		s = html.UnescapeString(s)
	}

	// 3. Strip residual HTML tags
	s = htmlTagRegex.ReplaceAllString(s, " ")
	return s
}

// fixEncodingAndPunctuation cleans unicode artifacts, replacement characters, and smart punctuation.
func (c *ArticleCleaner) fixEncodingAndPunctuation(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		switch r {
		case '\uFFFD': // Unicode replacement character
			continue
		case '\u200B', '\u200C', '\u200D', '\uFEFF', '\u00AD': // Zero-width spaces and soft hyphens
			continue
		case '*': // Markdown emphasis / bullet artifact
			continue
		case '“', '”', '„', '«', '»': // Smart double quotes
			b.WriteRune('"')
		case '‘', '’', '‚': // Smart single quotes / apostrophes
			b.WriteRune('\'')
		case '—', '–': // Em dash and En dash
			b.WriteRune('-')
		case '…': // Ellipsis
			b.WriteString("...")
		default:
			if unicode.IsPrint(r) || unicode.IsSpace(r) {
				b.WriteRune(r)
			}
		}
	}

	return b.String()
}

// collapseWhitespace condenses all consecutive spaces, tabs, and newlines into a single space and cleans punctuation spacing.
func (c *ArticleCleaner) collapseWhitespace(s string) string {
	collapsed := multiSpaceReg.ReplaceAllString(s, " ")
	return punctSpaceReg.ReplaceAllString(collapsed, "$1")
}

// deduplicateConsecutiveText eliminates repeated sentences or phrases within the text.
func (c *ArticleCleaner) deduplicateConsecutiveText(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 20 {
		return trimmed
	}

	// 1. Check if the entire string is an exact double repetition (e.g. "Text. Text.")
	halfLen := len(trimmed) / 2
	firstHalf := strings.TrimSpace(trimmed[:halfLen])
	secondHalf := strings.TrimSpace(trimmed[halfLen:])
	if firstHalf == secondHalf && len(firstHalf) > 10 {
		return firstHalf
	}

	// 2. Split by sentence terminators and deduplicate consecutive identical sentences
	rawParts := strings.Split(trimmed, ". ")
	if len(rawParts) <= 1 {
		return trimmed
	}

	var deduped []string
	var lastPart string

	for _, p := range rawParts {
		curr := strings.TrimSpace(p)
		if curr == "" {
			continue
		}
		currNorm := strings.ToLower(curr)
		if currNorm == lastPart {
			continue
		}
		deduped = append(deduped, curr)
		lastPart = currNorm
	}

	result := strings.Join(deduped, ". ")
	if !strings.HasSuffix(result, ".") && strings.HasSuffix(trimmed, ".") {
		result += "."
	}

	return result
}
