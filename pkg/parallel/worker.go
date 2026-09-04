package parallel

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"maps-scraper/pkg/model"
	"maps-scraper/pkg/normalizer"

	"github.com/mxschmitt/playwright-go"
)

var (
	latLonRegex = regexp.MustCompile(`!3d(-?[\d\.]+)!4d(-?[\d\.]+)`)
	urlCoordReg = regexp.MustCompile(`@(-?[\d\.]+),(-?[\d\.]+)`)
	hexIdRegex  = regexp.MustCompile(`0x[0-9a-fA-F]+:0x[0-9a-fA-F]+`)
	placeIdRegex = regexp.MustCompile(`ChIJ[a-zA-Z0-9_-]+`)
)

type WorkerConfig struct {
	WorkerID       int
	Headless       bool
	MinDelay       time.Duration
	MaxDelay       time.Duration
	Timeout        time.Duration
	Logger         *log.Logger
	JobConfig      model.JobConfig
	OnItemFound    func(workerID int, rec model.StoreRecord)
	OnWorkerUpdate func(wState model.WorkerState)
}



type Worker struct {
	cfg     WorkerConfig
	pw      *playwright.Playwright
	browser playwright.Browser
	context playwright.BrowserContext
	storage *StorageManager
}

func NewWorker(cfg WorkerConfig, storage *StorageManager) *Worker {
	return &Worker{
		cfg:     cfg,
		storage: storage,
	}
}

func (w *Worker) Init() error {
	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("[Worker %d] Failed to start Playwright: %w", w.cfg.WorkerID, err)
	}
	w.pw = pw

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(w.cfg.Headless),
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
		_ = pw.Stop()
		return fmt.Errorf("[Worker %d] Failed to launch Chromium: %w", w.cfg.WorkerID, err)
	}
	w.browser = browser

	browserContext, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Locale:    playwright.String("id-ID"),
		UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"),
	})
	if err != nil {
		_ = browser.Close()
		_ = pw.Stop()
		return fmt.Errorf("[Worker %d] Failed to create context: %w", w.cfg.WorkerID, err)
	}
	w.context = browserContext

	return nil
}

func (w *Worker) ProcessQuery(ctx context.Context, queryID int, queryString string) (int, error) {
	w.cfg.Logger.Printf("[Worker %02d] STARTING query=%d ('%s')\n", w.cfg.WorkerID, queryID, queryString)

	searchPage, err := w.context.NewPage()
	if err != nil {
		return 0, fmt.Errorf("failed to open search page: %w", err)
	}
	defer searchPage.Close()

	searchURL := fmt.Sprintf("https://www.google.com/maps/search/%s?hl=id", url.QueryEscape(queryString))
	timeoutMs := float64(w.cfg.Timeout.Milliseconds())
	if timeoutMs <= 0 {
		timeoutMs = 35000
	}

	if _, err := searchPage.Goto(searchURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(timeoutMs),
	}); err != nil {
		return 0, fmt.Errorf("search navigation error: %w", err)
	}

	w.randomDelay(800, 1500)

	// Dynamic Feed scrolling
	feedSelector := "div[role='feed']"
	_, _ = searchPage.WaitForSelector(feedSelector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(5000)})

	previousCount := 0
	noChangeCount := 0
	maxScrolls := 25

	for scroll := 0; scroll < maxScrolls; scroll++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		endEl, _ := searchPage.QuerySelector("span.HlvSq")
		if endEl != nil {
			break
		}

		_, _ = searchPage.Evaluate(`() => {
			const feed = document.querySelector('div[role="feed"]');
			if (feed) { feed.scrollTop = feed.scrollHeight; }
		}`)
		time.Sleep(1200 * time.Millisecond)

		currentElements, _ := searchPage.QuerySelectorAll("a[href*='/maps/place/']")
		currentCount := len(currentElements)
		
		if currentCount == previousCount {
			noChangeCount++
			if noChangeCount >= 2 {
				break
			}
		} else {
			noChangeCount = 0
		}
		previousCount = currentCount
	}

	candidateElements, _ := searchPage.QuerySelectorAll("a[href*='/maps/place/']")
	var detailLinks []string
	seenLocalLinks := make(map[string]bool)

	for _, el := range candidateElements {
		href, err := el.GetAttribute("href")
		if err == nil && href != "" && strings.Contains(href, "/maps/place/") {
			if !seenLocalLinks[href] {
				seenLocalLinks[href] = true
				detailLinks = append(detailLinks, href)
			}
		}
	}

	if len(detailLinks) == 0 {
		w.cfg.Logger.Printf("[Worker %02d] SUCCESS query=%d - No candidate stores found (query exhausted)\n", w.cfg.WorkerID, queryID)
		return 0, nil
	}

	detailPage, err := w.context.NewPage()
	if err != nil {
		return 0, fmt.Errorf("failed to open detail page: %w", err)
	}
	defer detailPage.Close()

	validStoresFound := 0

	for linkIdx, link := range detailLinks {
		select {
		case <-ctx.Done():
			return validStoresFound, ctx.Err()
		default:
		}

		rec, err := w.extractDetail(detailPage, link, timeoutMs)
		if err != nil || rec == nil || rec.Name == "" {
			continue
		}

		// Multi-Region Geographic Boundary Verification
		if w.cfg.JobConfig.Settings.ValidateLocation {
			locCfg := w.cfg.JobConfig.Location
			latVal, lonVal := 0.0, 0.0
			if rec.Latitude != "" && rec.Longitude != "" {
				latVal, _ = strconv.ParseFloat(strings.ReplaceAll(rec.Latitude, ".", ""), 64)
				lonVal, _ = strconv.ParseFloat(strings.ReplaceAll(rec.Longitude, ".", ""), 64)
				if latVal > 1000 || latVal < -1000 { latVal = latVal / 1000000.0 }
				if lonVal > 1000 || lonVal < -1000 { lonVal = lonVal / 1000000.0 }
			}

			targetCity := locCfg.City

			validRes := normalizer.EvaluateLocation(rec.Address, rec.City, rec.Kecamatan, latVal, lonVal, targetCity, locCfg.Province, locCfg.City, locCfg.District, locCfg.CustomLocation)
			
			if !validRes.IsValid {
				w.cfg.Logger.Printf("[REJECTED] Name: %s | Address: %s | Reason: %s | Expected: %s | Detected: %s | Confidence: %d\n",
					rec.Name, rec.Address, validRes.Reason, validRes.Expected, validRes.Detected, validRes.Confidence)
				continue
			}
		}


		// Category / Relevance Classification Check
		if w.cfg.JobConfig.Settings.ValidateCategory {
			if !normalizer.IsCementStoreRelevance(rec.Name, rec.Address, rec.Types) {
				continue
			}
		}

		// Permanently closed filter
		if w.cfg.JobConfig.Settings.SkipPermanentlyClosed && rec.Status == "CLOSED_PERMANENTLY" {
			continue
		}


		// Sanity check mandatory fields
		if rec.PlaceID == "" || rec.Name == "" || rec.Address == "" || rec.Latitude == "" || rec.Longitude == "" {
			continue
		}

		saved, err := w.storage.CheckAndSaveRecord(w.cfg.WorkerID, rec)
		if err != nil {
			w.cfg.Logger.Printf("[Worker %02d] Error saving record: %v\n", w.cfg.WorkerID, err)
			continue
		}

		if saved {
			rec.WorkerID = w.cfg.WorkerID
			validStoresFound++
			w.cfg.Logger.Printf("[Worker %02d] [NEW STORE FOUND] query=%d item=%d/%d: %s | %s | %s\n",
				w.cfg.WorkerID, queryID, linkIdx+1, len(detailLinks), rec.Name, rec.Kecamatan, rec.Phone)

			if w.cfg.OnItemFound != nil {
				w.cfg.OnItemFound(w.cfg.WorkerID, *rec)
			}
		}

	}

	w.cfg.Logger.Printf("[Worker %02d] SUCCESS query=%d - Added %d new verified stores\n",
		w.cfg.WorkerID, queryID, validStoresFound)

	return validStoresFound, nil
}

func (w *Worker) extractDetail(page playwright.Page, urlStr string, timeoutMs float64) (*model.StoreRecord, error) {
	if _, err := page.Goto(urlStr, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(timeoutMs),
	}); err != nil {
		return nil, err
	}

	time.Sleep(1200 * time.Millisecond)

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

	openingHours := w.extractHours(page)

	types := "store, point_of_interest, establishment"
	if category != "" {
		types = strings.ToLower(category) + ", " + types
	}
	if rating == "" {
		types = "tidak ada ulasan, " + types
	}

	content, _ := page.Content()
	contentLower := strings.ToLower(content)

	status := "UNKNOWN"
	if strings.Contains(contentLower, "tutup permanen") || strings.Contains(strings.ToLower(openingHours), "tutup permanen") || strings.Contains(strings.ToLower(name), "permanen") {
		status = "CLOSED_PERMANENTLY"
	} else if strings.Contains(contentLower, "tutup sementara") || strings.Contains(strings.ToLower(openingHours), "tutup sementara") {
		status = "CLOSED_TEMPORARILY"
	} else if openingHours != "" {
		status = "OPEN"
	}

	city := normalizer.ExtractCity(address, w.cfg.JobConfig.Location.City)
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
	if chijMatches := placeIdRegex.FindAllString(page.URL(), -1); len(chijMatches) > 0 {
		placeID = chijMatches[0]
	}
	if placeID == "" {
		if chijMatches := placeIdRegex.FindAllString(content, -1); len(chijMatches) > 0 {
			placeID = chijMatches[0]
		}
	}
	if placeID == "" {
		if hexMatches := hexIdRegex.FindAllString(page.URL(), -1); len(hexMatches) > 0 {
			placeID = hexMatches[0]
		}
	}
	if placeID == "" {
		if metaEl, _ := page.QuerySelector("meta[itemprop='url'], meta[property='og:image']"); metaEl != nil {
			if contentAttr, _ := metaEl.GetAttribute("content"); contentAttr != "" {
				if hexMatches := hexIdRegex.FindAllString(contentAttr, -1); len(hexMatches) > 0 {
					placeID = hexMatches[0]
				}
			}
		}
	}
	if placeID == "" {
		if hexMatches := hexIdRegex.FindAllString(content, -1); len(hexMatches) > 0 {
			placeID = hexMatches[0]
		}
	}

	return &model.StoreRecord{
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
	}, nil
}

func (w *Worker) extractHours(page playwright.Page) string {
	_, _ = page.Evaluate(`() => {
		const btns = Array.from(document.querySelectorAll('button, div[role="button"]'));
		for (const b of btns) {
			if (b.innerText && (b.innerText.includes('Buka') || b.innerText.includes('Tutup') || b.innerText.includes('Open') || b.innerText.includes('Closed'))) {
				b.click();
				break;
			}
		}
	}`)
	time.Sleep(200 * time.Millisecond)

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
	return ""
}

func (w *Worker) randomDelay(minMs, maxMs int) {
	if minMs <= 0 || maxMs <= minMs {
		return
	}
	n := rand.Intn(maxMs-minMs) + minMs
	time.Sleep(time.Duration(n) * time.Millisecond)
}

func (w *Worker) Close() {
	if w.context != nil {
		_ = w.context.Close()
	}
	if w.browser != nil {
		_ = w.browser.Close()
	}
	if w.pw != nil {
		_ = w.pw.Stop()
	}
}
