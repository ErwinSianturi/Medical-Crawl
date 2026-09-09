package cleaner

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"maps-scraper/pkg/model"
)

func TestCleaner_TextSanitizationAndHTMLStripping(t *testing.T) {
	c := NewArticleCleaner(DefaultCleanerConfig())

	input := &model.Article{
		Title:    "  <h1>Understanding &amp; Managing Diabetes</h1> <script>alert(1)</script> ",
		Category: "diabetes",
		Image:    "https://example.com/images/diabetes.jpg",
		Description: []string{
			`<!-- Lead summary -->
			<p>Diabetes is a chronic condition characterized by <span class="highlight">high blood glucose</span>.</p>
			<style>.highlight { color: red; }</style>
			Home > Health Topics > Diabetes Overview
			Click here to read more.
			Advertisement: Buy testing strips today!
			This website uses cookies to personalize your experience. Accept all cookies.`,
			"Managing blood glucose requires regular physical activity and a balanced diet.",
		},
	}

	cleaned, err := c.CleanAndValidate(input)
	if err != nil {
		t.Fatalf("CleanAndValidate failed: %v", err)
	}

	// 1. Check title
	if strings.Contains(cleaned.Title, "<h1>") || strings.Contains(cleaned.Title, "<script>") {
		t.Errorf("Title still contains HTML tags: %q", cleaned.Title)
	}
	if cleaned.Title != "Understanding & Managing Diabetes" {
		t.Errorf("Unexpected cleaned title: %q", cleaned.Title)
	}

	// 2. Check category title-cased
	if cleaned.Category != "Diabetes" {
		t.Errorf("Expected category 'Diabetes', got %q", cleaned.Category)
	}

	// 3. Check description length (between 1 and 3 paragraphs)
	if len(cleaned.Description) < 1 || len(cleaned.Description) > 3 {
		t.Fatalf("Expected 1 to 3 paragraphs, got %d", len(cleaned.Description))
	}

	for _, p := range cleaned.Description {
		if strings.Contains(p, "<") || strings.Contains(p, ">") {
			t.Errorf("Description paragraph contains HTML tags: %q", p)
		}
		if strings.Contains(p, "alert(1)") || strings.Contains(p, ".highlight") {
			t.Errorf("Description paragraph contains script/style: %q", p)
		}
		if strings.Contains(p, "Home >") || strings.Contains(p, "Click here") || strings.Contains(p, "Advertisement") || strings.Contains(p, "cookies") {
			t.Errorf("Description paragraph contains boilerplate/ads: %q", p)
		}
	}

	// 4. Ensure core content preserved in first paragraph
	if !strings.Contains(cleaned.Description[0], "Diabetes is a chronic condition characterized by high blood glucose.") {
		t.Errorf("Core description text corrupted: %q", cleaned.Description[0])
	}
}

func TestCleaner_EncodingAndMalformedCharacters(t *testing.T) {
	c := NewArticleCleaner(DefaultCleanerConfig())

	input := &model.Article{
		Title:       "Alzheimer\u2019s Disease \u2014 Early Signs \uFFFD",
		Category:    "Neurology",
		Image:       "https://medlineplus.gov/images/alzheimers.png",
		Description: []string{"\u200B“Memory loss” is common\u2026 Doctors say it\u2019s critical to act early.\uFEFF"},
	}

	cleaned, err := c.CleanAndValidate(input)
	if err != nil {
		t.Fatalf("CleanAndValidate failed: %v", err)
	}

	// Smart quote/dash normalization
	if strings.Contains(cleaned.Title, "\uFFFD") {
		t.Errorf("Title contains unicode replacement character: %q", cleaned.Title)
	}
	if !strings.Contains(cleaned.Title, "Alzheimer's Disease - Early Signs") {
		t.Errorf("Smart apostrophe or dash not normalized: %q", cleaned.Title)
	}

	if strings.Contains(cleaned.Description[0], "\u200B") || strings.Contains(cleaned.Description[0], "\uFEFF") {
		t.Errorf("Description contains zero-width spaces: %q", cleaned.Description[0])
	}
	if !strings.Contains(cleaned.Description[0], `"Memory loss" is common... Doctors say it's critical to act early.`) {
		t.Errorf("Description smart quotes/ellipsis not normalized: %q", cleaned.Description[0])
	}
}

func TestCleaner_DuplicatedDescriptionText(t *testing.T) {
	c := NewArticleCleaner(DefaultCleanerConfig())

	// Exact repeated sentences
	input := &model.Article{
		Title:       "Vaccine Safety and Effectiveness",
		Category:    "Vaccination",
		Image:       "https://example.com/vaccine.jpg",
		Description: []string{"Vaccines are rigorously tested for safety. Vaccines are rigorously tested for safety. Clinical trials confirm high protection."},
	}

	cleaned, err := c.CleanAndValidate(input)
	if err != nil {
		t.Fatalf("CleanAndValidate failed: %v", err)
	}

	count := strings.Count(cleaned.Description[0], "Vaccines are rigorously tested for safety")
	if count != 1 {
		t.Errorf("Duplicated sentence not collapsed, count=%d: %q", count, cleaned.Description[0])
	}
}

func TestCleaner_InvalidImageURLs(t *testing.T) {
	c := NewArticleCleaner(DefaultCleanerConfig())

	invalidImages := []string{
		"ftp://example.com/pic.jpg",
		"http://localhost/pic.jpg",
		"not-a-url",
		"https:///no-domain.jpg",
	}

	for _, img := range invalidImages {
		art := &model.Article{
			Title:       "Valid Article Title",
			Category:    "General Health",
			Image:       img,
			Description: []string{"A valid and sufficiently long article description for testing."},
		}

		_, err := c.CleanAndValidate(art)
		if err == nil {
			t.Errorf("Expected error for invalid image %q, got nil", img)
		}
	}

	// Empty image should fallback to "-"
	emptyImgArt := &model.Article{
		Title:       "Valid Article Title",
		Category:    "General Health",
		Image:       "",
		Description: []string{"A valid and sufficiently long article description for testing."},
	}
	cleanedNoImg, err := c.CleanAndValidate(emptyImgArt)
	if err != nil {
		t.Fatalf("Expected empty image to be normalized to '-', got err: %v", err)
	}
	if cleanedNoImg.Image != "-" {
		t.Errorf("Expected '-', got %q", cleanedNoImg.Image)
	}
}

func TestCleaner_MissingFieldsValidation(t *testing.T) {
	c := NewArticleCleaner(DefaultCleanerConfig())

	// 1. Missing / too short title
	_, err := c.CleanAndValidate(&model.Article{
		Title:       "   ",
		Category:    "General Health",
		Image:       "https://example.com/img.jpg",
		Description: []string{"Valid description text that is long enough."},
	})
	if !errors.Is(err, ErrEmptyTitle) && !errors.Is(err, ErrTitleTooShort) {
		t.Errorf("Expected empty title error, got %v", err)
	}

	// 2. Missing / too short description
	_, err = c.CleanAndValidate(&model.Article{
		Title:       "Valid Title Here",
		Category:    "General Health",
		Image:       "https://example.com/img.jpg",
		Description: []string{"short"},
	})
	if !errors.Is(err, ErrDescTooShort) && !errors.Is(err, ErrEmptyDescription) {
		t.Errorf("Expected short description error, got %v", err)
	}

	// 3. Empty description array
	_, err = c.CleanAndValidate(&model.Article{
		Title:       "Valid Title Here",
		Category:    "General Health",
		Image:       "https://example.com/img.jpg",
		Description: []string{},
	})
	if !errors.Is(err, ErrEmptyDescription) {
		t.Errorf("Expected empty description error for empty array, got %v", err)
	}
}

func TestDeduplicator_ExactAndWhitespaceDuplicates(t *testing.T) {
	d := NewDeduplicator(0.85)

	art1 := &model.Article{
		Title:       "Understanding Hypertension and Blood Pressure",
		Category:    "Heart Disease",
		Image:       "https://example.com/heart.jpg",
		Description: []string{"High blood pressure is a silent risk factor for cardiovascular disease."},
	}

	res1 := d.CheckArticle(art1, "https://example.com/art1")
	if res1.IsDuplicate {
		t.Fatalf("First article should not be duplicate: %s", res1.Reason)
	}

	// 1. Exact Duplicate
	res2 := d.CheckArticle(art1, "https://example.com/art1")
	if !res2.IsDuplicate {
		t.Errorf("Expected exact duplicate detection")
	}

	// 2. Whitespace and Case Variation
	artWhitespace := &model.Article{
		Title:       "  understanding   hypertension  and  blood   pressure  ",
		Category:    "Heart Disease",
		Image:       "https://example.com/heart2.jpg",
		Description: []string{"High blood pressure is a silent risk factor for cardiovascular disease."},
	}
	res3 := d.CheckArticle(artWhitespace, "https://example.com/art2")
	if !res3.IsDuplicate {
		t.Errorf("Expected whitespace-normalized duplicate detection")
	}

	// 3. Repeated Article with minor punctuation difference
	artPunctuation := &model.Article{
		Title:       "Understanding Hypertension & Blood Pressure!",
		Category:    "Heart Disease",
		Image:       "https://example.com/heart3.jpg",
		Description: []string{"Different description."},
	}
	res4 := d.CheckArticle(artPunctuation, "https://example.com/art3")
	if !res4.IsDuplicate {
		t.Errorf("Expected normalized title duplicate detection")
	}
}

func TestDeduplicator_FuzzyDuplicates(t *testing.T) {
	d := NewDeduplicator(0.85)

	orig := &model.Article{
		Title:       "Chemotherapy Options for Non-Small Cell Lung Cancer",
		Category:    "Cancer",
		Image:       "https://example.com/lung.jpg",
		Description: []string{"Overview of oncological protocols."},
	}
	d.CheckArticle(orig, "https://example.com/orig")

	// Very similar title (> 85% Levenshtein similarity)
	similar := &model.Article{
		Title:       "Chemotherapy Option for Non-Small Cell Lung Cancers",
		Category:    "Cancer",
		Image:       "https://example.com/lung2.jpg",
		Description: []string{"Different overview."},
	}

	res := d.CheckArticle(similar, "https://example.com/similar")
	if !res.IsDuplicate {
		t.Errorf("Expected fuzzy title duplicate detection")
	}

	// Distinct title (< 85% similarity)
	distinct := &model.Article{
		Title:       "Radiation Therapy for Breast Cancer",
		Category:    "Cancer",
		Image:       "https://example.com/breast.jpg",
		Description: []string{"Overview of radiation protocols."},
	}

	resDistinct := d.CheckArticle(distinct, "https://example.com/distinct")
	if resDistinct.IsDuplicate {
		t.Errorf("Expected distinct article to pass, got duplicate: %s", resDistinct.Reason)
	}
}

func TestDeduplicator_NoInternalIdentifiersInJSON(t *testing.T) {
	c := NewArticleCleaner(DefaultCleanerConfig())
	d := NewDeduplicator(0.85)

	art := &model.Article{
		Title:       "Healthy Nutrition and Caloric Deficit",
		Category:    "Nutrition",
		Image:       "https://example.com/food.jpg",
		Description: []string{"Balancing protein, fiber, and carbohydrates supports healthy weight."},
	}

	cleaned, err := c.CleanAndValidate(art)
	if err != nil {
		t.Fatalf("Clean failed: %v", err)
	}

	res := d.CheckArticle(cleaned, "https://example.com/nutri")
	if res.IsDuplicate {
		t.Fatalf("Unexpected duplicate")
	}

	// Verify JSON output has strictly the 4 fields and no internal hash/metadata
	jsonData, err := json.Marshal(cleaned)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(jsonData, &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(m) != 4 {
		t.Errorf("Expected exactly 4 fields in JSON, got %d: %v", len(m), m)
	}

	for _, key := range []string{"title", "category", "image", "description"} {
		if _, ok := m[key]; !ok {
			t.Errorf("Missing required key %q in JSON output", key)
		}
	}

	// Verify no internal hash leaked
	for k := range m {
		if strings.Contains(strings.ToLower(k), "hash") || strings.Contains(strings.ToLower(k), "id") {
			t.Errorf("Leaked internal identifier in JSON: %q", k)
		}
	}
}

func TestCleaner_CleanImageURL(t *testing.T) {
	c := NewArticleCleaner(DefaultCleanerConfig())

	testCases := []struct {
		input    string
		expected string
	}{
		{
			input:    "https://d1vbn70lmn1nqe.cloudfront.net/prod/wp-content/uploads/2026/04/24022439/Tidur-1.jpg-1.jpg.webp",
			expected: "https://d1vbn70lmn1nqe.cloudfront.net/prod/wp-content/uploads/2026/04/24022439/Tidur-1.jpg-1.jpg",
		},
		{
			input:    "https://d1vbn70lmn1nqe.cloudfront.net/prod/wp-content/uploads/2026/09/09031408/SEO-6-Aug_Kehamilan-dan-Persalinan.jpg.jpg",
			expected: "https://d1vbn70lmn1nqe.cloudfront.net/prod/wp-content/uploads/2026/09/09031408/SEO-6-Aug_Kehamilan-dan-Persalinan.jpg.jpg",
		},
		{
			input:    "https://www.halodoc.com/images/banner.png.webp",
			expected: "https://www.halodoc.com/images/banner.png",
		},
		{
			input:    "-",
			expected: "-",
		},
		{
			input:    "",
			expected: "",
		},
	}

	for _, tc := range testCases {
		res := c.CleanImageURL(tc.input)
		if res != tc.expected {
			t.Errorf("CleanImageURL(%q) = %q, expected %q", tc.input, res, tc.expected)
		}
	}
}

func TestCleaner_KompasDatelineAndDonationAppealStripping(t *testing.T) {
	c := NewArticleCleaner(DefaultCleanerConfig())

	paras := []string{
		"KOMPAS.com - Sebuah laporan dalam jurnal Emerging Infectious Diseases edisi September 2026 mengungkap lonjakan tajam kasus infeksi jamur Trichophyton indotineae, penyebab kurap yang kebal terhadap terbinafine, salah satu obat antijamur yang paling umum digunakan.",
		"Infeksi jamur ini dilaporkan berkembang pesat di berbagai wilayah dan memerlukan diagnosis laboratorium yang tepat.",
		"Saudara-saudara kita di Nusa Tenggara Timur tengah dirundung duka karena gempa M 7,7. Mari kita mengulurkan tangan untuk membantu meringankan beban yang sedang mereka hadapi. Kirim bantuan Anda melalui tautan https://bit.ly/BantuWargaNTT",
	}

	cleaned := c.CleanParagraphs(paras)

	if len(cleaned) != 2 {
		t.Fatalf("Expected exactly 2 paragraphs, got %d: %v", len(cleaned), cleaned)
	}

	// 1. Verify dateline stripped from first paragraph
	expectedLead := "Sebuah laporan dalam jurnal Emerging Infectious Diseases edisi September 2026 mengungkap lonjakan tajam kasus infeksi jamur Trichophyton indotineae, penyebab kurap yang kebal terhadap terbinafine, salah satu obat antijamur yang paling umum digunakan."
	if cleaned[0] != expectedLead {
		t.Errorf("Expected leading KOMPAS.com to be stripped.\nGot: %q\nWant: %q", cleaned[0], expectedLead)
	}

	// 2. Verify donation appeal paragraph is completely removed
	for _, p := range cleaned {
		if strings.Contains(p, "BantuWargaNTT") || strings.Contains(p, "mengulurkan tangan") || strings.Contains(p, "dirundung duka") {
			t.Errorf("Donation appeal was not stripped: %q", p)
		}
	}
}

