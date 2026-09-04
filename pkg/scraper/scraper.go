package scraper

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"maps-scraper/pkg/database"
	"maps-scraper/pkg/model"
	"maps-scraper/pkg/normalizer"
	"maps-scraper/pkg/writer"

	"github.com/mxschmitt/playwright-go"
)

var (
	latLonRegex = regexp.MustCompile(`!3d(-?[\d\.]+)!4d(-?[\d\.]+)`)
	urlCoordReg = regexp.MustCompile(`@(-?[\d\.]+),(-?[\d\.]+)`)
	hexIdRegex  = regexp.MustCompile(`0x[0-9a-fA-F]+:0x[0-9a-fA-F]+`)
)

type Config struct {
	Queries    []string
	MaxPlaces  int
	Headless   bool
	TimeoutMs  float64
	OutputPath string
}

type MapsScraper struct {
	cfg Config
	pw  *playwright.Playwright
	br  playwright.Browser
	mu  sync.Mutex
}

func NewMapsScraper(cfg Config) *MapsScraper {
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 40000
	}
	if len(cfg.Queries) == 0 {
		cfg.Queries = generateExhaustiveSurabayaQueries()
	}
	return &MapsScraper{cfg: cfg}
}

func generateExhaustiveSurabayaQueries() []string {
	var queries []string

	// General building & cement store prefixes
	prefixes := []string{
		"toko bangunan",
		"toko semen",
		"toko bahan bangunan",
		"distributor semen",
		"supplier semen",
		"toko material",
		"toko besi dan bangunan",
		"toko semen mortar",
		"grosir semen",
		"depo bangunan",
		"agen semen gresik",
		"agen semen tiga roda",
		"agen semen dynamix",
		"agen semen conch",
		"agen semen singa merah",
		"toko baja ringan dan genteng",
		"toko bata ringan hebel",
		"toko keramik dan semen",
	}

	for _, p := range prefixes {
		queries = append(queries, fmt.Sprintf("%s surabaya", p))
	}

	// Multi-signal variations across all 31 official Districts (Kecamatan) of Surabaya
	for _, kec := range normalizer.SurabayaKecamatans {
		queries = append(queries, fmt.Sprintf("toko bangunan %s surabaya", kec))
		queries = append(queries, fmt.Sprintf("toko bahan bangunan %s surabaya", kec))
		queries = append(queries, fmt.Sprintf("toko semen %s surabaya", kec))
		queries = append(queries, fmt.Sprintf("toko material %s surabaya", kec))
		queries = append(queries, fmt.Sprintf("distributor semen %s surabaya", kec))
		queries = append(queries, fmt.Sprintf("toko bata ringan %s surabaya", kec))
		queries = append(queries, fmt.Sprintf("toko besi bangunan %s surabaya", kec))
	}

	return queries
}

func (s *MapsScraper) Init() error {
	log.Println("[INIT] Launching Playwright browser engine for large scale scraping...")
	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("could not start playwright: %w", err)
	}
	s.pw = pw

	br, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(s.cfg.Headless),
		Args: []string{
			"--disable-blink-features=AutomationControlled",
			"--no-sandbox",
			"--disable-setuid-sandbox",
			"--disable-infobars",
			"--window-size=1366,850",
			"--lang=id-ID,id",
		},
	})
	if err != nil {
		return fmt.Errorf("could not launch chromium browser: %w", err)
	}
	s.br = br
	return nil
}

func (s *MapsScraper) Close() {
	if s.br != nil {
		s.br.Close()
	}
	if s.pw != nil {
		s.pw.Stop()
	}
}

// Scrape executes systematic multi-query extraction to reach target 1200+ records
func (s *MapsScraper) Scrape(ctx context.Context) ([]model.StoreRecord, error) {
	var records []model.StoreRecord
	seenPlaceIDs := make(map[string]bool)
	seenUrls := make(map[string]bool)

	csvWriter := writer.NewCSVWriter(s.cfg.OutputPath)

	detailPage, err := s.br.NewPage(playwright.BrowserNewPageOptions{
		Locale:    playwright.String("id-ID"),
		UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open detail page: %w", err)
	}
	defer detailPage.Close()

	for qIdx, query := range s.cfg.Queries {
		if len(records) >= s.cfg.MaxPlaces {
			log.Printf("[TARGET ACHIEVED] Collected %d stores, stopping search.\n", len(records))
			break
		}

		log.Printf("\n[QUERY %d/%d] Searching: '%s' (Current count: %d/%d)...\n",
			qIdx+1, len(s.cfg.Queries), query, len(records), s.cfg.MaxPlaces)

		searchPage, err := s.br.NewPage(playwright.BrowserNewPageOptions{
			Locale:    playwright.String("id-ID"),
			UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"),
		})
		if err != nil {
			log.Printf("[ERROR] Search page creation failed: %v\n", err)
			continue
		}

		searchURL := fmt.Sprintf("https://www.google.com/maps/search/%s?hl=id", url.QueryEscape(query))
		if _, err := searchPage.Goto(searchURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			Timeout:   playwright.Float(s.cfg.TimeoutMs),
		}); err != nil {
			log.Printf("[ERROR] Navigation failed for query '%s': %v\n", query, err)
			searchPage.Close()
			continue
		}

		time.Sleep(2500 * time.Millisecond)
		feedSelector := "div[role='feed']"
		_, _ = searchPage.WaitForSelector(feedSelector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(8000)})

		// Scroll to discover more items in this area
		for scroll := 0; scroll < 12; scroll++ {
			endEl, _ := searchPage.QuerySelector("span.HlvSq")
			if endEl != nil {
				break
			}
			_, _ = searchPage.Evaluate(`() => {
				const feed = document.querySelector('div[role="feed"]');
				if (feed) { feed.scrollTop += 3000; }
			}`)
			time.Sleep(1500 * time.Millisecond)
		}

		candidateElements, _ := searchPage.QuerySelectorAll("a[href*='/maps/place/']")
		var detailLinks []string
		for _, el := range candidateElements {
			href, err := el.GetAttribute("href")
			if err == nil && href != "" && strings.Contains(href, "/maps/place/") {
				if !seenUrls[href] {
					seenUrls[href] = true
					detailLinks = append(detailLinks, href)
				}
			}
		}
		searchPage.Close()

		log.Printf("[DISCOVERY] Found %d new candidates for '%s'\n", len(detailLinks), query)

		for _, link := range detailLinks {
			if len(records) >= s.cfg.MaxPlaces {
				break
			}

			rec, err := s.extractPlaceDetail(detailPage, link)
			if err != nil {
				continue
			}

			if rec.Name == "" {
				continue
			}

			// Validate Surabaya boundary
			// if !normalizer.IsInSurabaya(rec.Address) {
			// 	continue
			// }

			// Strict cement/building store validation
			if !normalizer.IsCementStoreRelevance(rec.Name, rec.Address, rec.Types) {
				continue
			}

			// Multi-level Deduplication
			// Level 1: Place ID
			if rec.PlaceID != "" {
				if seenPlaceIDs[rec.PlaceID] {
					continue
				}
				seenPlaceIDs[rec.PlaceID] = true
			}

			// Level 2: Name + Address
			nameAddrKey := strings.ToLower(rec.Name) + "||" + strings.ToLower(rec.Address)
			if seenUrls[nameAddrKey] {
				continue
			}
			seenUrls[nameAddrKey] = true

			// Ensure mandatory fields are present
			if rec.Name == "" || rec.Address == "" || rec.City == "" || rec.Kecamatan == "" || rec.Latitude == "" || rec.Longitude == "" {
				continue
			}

			records = append(records, *rec)
			
			if database.DB != nil {
				_ = database.SaveRecord(rec)
			}

			log.Printf("[STORE %4d/%d] %s | %s | %s\n",
				len(records), s.cfg.MaxPlaces, rec.Name, rec.Kecamatan, rec.Phone)

			// Progressively flush to CSV every 10 stores
			if len(records)%10 == 0 || len(records) == 1 {
				_ = csvWriter.WriteRecords(records)
			}
		}
	}

	// Final write
	_ = csvWriter.WriteRecords(records)
	log.Printf("[SUMMARY] Successfully scraped and saved %d verified stores to %s\n", len(records), s.cfg.OutputPath)
	return records, nil
}

func (s *MapsScraper) extractPlaceDetail(page playwright.Page, urlStr string) (*model.StoreRecord, error) {
	if _, err := page.Goto(urlStr, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(s.cfg.TimeoutMs),
	}); err != nil {
		return nil, fmt.Errorf("detail navigation failed: %w", err)
	}

	time.Sleep(2 * time.Second)

	name := ""
	nameEl, _ := page.QuerySelector("h1")
	if nameEl != nil {
		name, _ = nameEl.InnerText()
		name = strings.TrimSpace(name)
	}

	currentURL := page.URL()
	lat := 0.0
	lon := 0.0

	if matches := latLonRegex.FindStringSubmatch(currentURL); len(matches) == 3 {
		lat, _ = strconv.ParseFloat(matches[1], 64)
		lon, _ = strconv.ParseFloat(matches[2], 64)
	} else if matches := urlCoordReg.FindStringSubmatch(currentURL); len(matches) == 3 {
		lat, _ = strconv.ParseFloat(matches[1], 64)
		lon, _ = strconv.ParseFloat(matches[2], 64)
	}

	rating := ""
	ratingEl, _ := page.QuerySelector("div.F7nice span[aria-hidden='true']")
	if ratingEl != nil {
		txt, _ := ratingEl.InnerText()
		txt = strings.TrimSpace(strings.ReplaceAll(txt, ",", "."))
		if rVal, err := strconv.ParseFloat(txt, 64); err == nil {
			rating = normalizer.FormatRating(rVal)
		}
	}

	category := ""
	catEl, _ := page.QuerySelector("button.DkEaL, button[jsaction*='pane.rating.category']")
	if catEl != nil {
		category, _ = catEl.InnerText()
		category = strings.TrimSpace(category)
	}

	address := ""
	var extractedPhoneFromAddress string
	addrEl, _ := page.QuerySelector("button[data-item-id='address'] div.fontBodyMedium, button[data-item-id='address'] div.Io6YTe, div[data-item-id='address']")
	if addrEl != nil {
		address, _ = addrEl.InnerText()
		address, extractedPhoneFromAddress = normalizer.CleanAddress(address)
	}

	phone := extractedPhoneFromAddress
	phoneEl, _ := page.QuerySelector("button[data-item-id*='phone:tel:'] div.fontBodyMedium, button[data-item-id*='phone:tel:'] div.Io6YTe")
	if phoneEl != nil {
		phone, _ = phoneEl.InnerText()
		phone = normalizer.FormatPhone(phone)
	}

	website := ""
	webEl, _ := page.QuerySelector("a[data-item-id='authority']")
	if webEl != nil {
		website, _ = webEl.GetAttribute("href")
	}

	photoURL := ""
	imgEl, _ := page.QuerySelector("button[aria-label*='Foto'] img, img[src*='googleusercontent.com']")
	if imgEl != nil {
		photoURL, _ = imgEl.GetAttribute("src")
	}

	openingHours := s.extractOpeningHours(page)

	types := "store, point_of_interest, establishment"
	if category != "" {
		types = strings.ToLower(category) + ", " + types
	}
	if rating == "" {
		types = "tidak ada ulasan, " + types
	}

	status := "Buka"
	if strings.Contains(openingHours, "Tutup Permanen") || strings.Contains(name, "Permanen") {
		status = "Tutup Permanen"
	}

	city := normalizer.ExtractCity(address, "Surabaya") // Changed from hardcoded "KOTA SURABAYA" to standard extraction
	kecamatan := normalizer.ExtractKecamatan(address, city)

	latFormatted := ""
	lonFormatted := ""
	dotLink := ""
	commaLink := ""
	if lat != 0 && lon != 0 {
		latFormatted = normalizer.FormatCoordinate(lat)
		lonFormatted = normalizer.FormatCoordinate(lon)
		dotLink, commaLink = normalizer.GenerateMapsQueryLinks(lat, lon)
	}

	semenBrand := normalizer.DetectSemenBrand(name, address)

	placeID := ""
	content, _ := page.Content()
	if hexMatches := hexIdRegex.FindAllString(content, -1); len(hexMatches) > 0 {
		placeID = hexMatches[0]
	}

	rec := &model.StoreRecord{
		PlaceID:           placeID,
		Name:              name,
		Address:           address,
		City:              city,
		Kecamatan:         kecamatan,
		Latitude:          latFormatted,
		Longitude:         lonFormatted,
		Types:             types,
		Rating:            rating,
		Phone:             phone,
		Status:            status,
		OpeningHours:      openingHours,
		PhotoURL:          photoURL,
		WebsiteLinks:      website,
		SemenYangDijual:   semenBrand,
		LinkSetinganTitik: dotLink,
		LinkSetinganKoma:  commaLink,
	}

	return rec, nil
}

func (s *MapsScraper) extractOpeningHours(page playwright.Page) string {
	// Retry mechanism: Sometimes the UI isn't fully initialized, so click does nothing.
    for i := 0; i < 3; i++ {
        _, _ = page.Evaluate(`() => {
            const ohDiv = document.querySelector('div[data-item-id^="oh"]');
            if (ohDiv) {
                const btn = ohDiv.querySelector('[aria-expanded]');
                if (btn && btn.getAttribute('aria-expanded') === 'false') {
                    btn.click();
                } else if (!btn) {
                    const b = ohDiv.querySelector('button');
                    if (b) b.click();
                }
            }
            
            // Fallback for search result panels that don't have ohDiv but use div[role="button"]
            const btns = Array.from(document.querySelectorAll('button, div[role="button"], div[aria-expanded]'));
            for (const b of btns) {
                if (b.innerText && (b.innerText.includes('Buka') || b.innerText.includes('Tutup') || b.innerText.includes('Open') || b.innerText.includes('Closed'))) {
                    if (b.getAttribute('aria-expanded') === 'false') {
                        b.click();
                        break;
                    }
                }
            }
        }`)
        time.Sleep(1500 * time.Millisecond)

        // Check if table expanded (more than 2 rows)
        rowCount, _ := page.Evaluate(`() => {
            const trs = Array.from(document.querySelectorAll('table tr, div.t392fc tr, div[role="dialog"] tr'));
            return trs.length;
        }`)
        if count, ok := rowCount.(int); ok && count >= 5 {
            break // Table expanded successfully
        }
    }

	tableText, _ := page.Evaluate(`() => {
		const trs = Array.from(document.querySelectorAll('table tr, div.t392fc tr, div[role="dialog"] tr'));
		return trs.map(t => t.innerText).join('\n');
	}`)
	if str, ok := tableText.(string); ok && str != "" {
		normalized := normalizer.NormalizeOpeningHours(str)
		if normalized != "" {
			return normalized
		}
	}

	dayInfo, _ := page.QuerySelector("div[data-item-id*='oh']")
	if dayInfo != nil {
		txt, _ := dayInfo.InnerText()
		if strings.Contains(txt, "24 hours") || strings.Contains(txt, "24 jam") {
			return "Senin,Buka 24 jam | Selasa,Buka 24 jam | Rabu,Buka 24 jam | Kamis,Buka 24 jam | Jumat,Buka 24 jam | Sabtu,Buka 24 jam | Minggu,Buka 24 jam"
		}
	}

	return ""
}
