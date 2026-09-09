package classifier

import (
	"regexp"
	"strings"
	"sync"
	"unicode"

	"maps-scraper/pkg/model"
)

// Standard Medical Categories as defined in STEP 6
const (
	CategoryCancer            = "Cancer"
	CategoryDiabetes          = "Diabetes"
	CategoryHeartDisease      = "Heart Disease"
	CategoryInfectiousDisease = "Infectious Disease"
	CategoryMentalHealth      = "Mental Health"
	CategoryNutrition         = "Nutrition"
	CategoryVaccination       = "Vaccination"
	CategoryNeurology         = "Neurology"
	CategoryPharmacy          = "Pharmacy"
	CategoryGym               = "Gym"
	CategoryGeneralHealth     = "General Health"
)

// CategoryRule defines a category name and its matching keywords/phrases.
type CategoryRule struct {
	Category string   `json:"category"`
	Keywords []string `json:"keywords"`
}

// ClassificationResult contains detailed results of a classification query.
type ClassificationResult struct {
	Category        string   `json:"category"`
	Score           int      `json:"score"`
	TitleScore      int      `json:"title_score"`
	DescScore       int      `json:"desc_score"`
	MatchedKeywords []string `json:"matched_keywords"`
}

type compiledKeyword struct {
	raw       string
	pattern   *regexp.Regexp
	wordCount int
}

type compiledRule struct {
	category string
	keywords []compiledKeyword
}

// Classifier performs deterministic rule-based medical category detection.
type Classifier struct {
	mu              sync.RWMutex
	rules           []compiledRule
	defaultCategory string
	titleWeight     int
	descWeight      int
}

// NewClassifier creates a Classifier with custom rules and default category fallback.
func NewClassifier(rules []CategoryRule, defaultCategory string) *Classifier {
	if defaultCategory == "" {
		defaultCategory = CategoryGeneralHealth
	}

	c := &Classifier{
		defaultCategory: defaultCategory,
		titleWeight:     3,
		descWeight:      1,
	}

	for _, r := range rules {
		c.addRuleInternal(r)
	}

	return c
}

// NewDefaultClassifier initializes the classifier with the standard 10 medical categories.
func NewDefaultClassifier() *Classifier {
	return NewClassifier(defaultCategoryRules(), CategoryGeneralHealth)
}

// AddRule dynamically registers or appends a category rule.
func (c *Classifier) AddRule(rule CategoryRule) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.addRuleInternal(rule)
}

func (c *Classifier) addRuleInternal(rule CategoryRule) {
	catName := strings.TrimSpace(rule.Category)
	if catName == "" {
		return
	}

	var compiled []compiledKeyword
	for _, kw := range rule.Keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}

		words := strings.Fields(kw)
		if len(words) == 0 {
			continue
		}

		// Escape each word for regex and allow flexible whitespace or hyphens between words
		escaped := make([]string, len(words))
		for i, w := range words {
			escaped[i] = regexp.QuoteMeta(w)
		}

		// Compile word-boundary regex: case-insensitive
		patternStr := `(?i)\b` + strings.Join(escaped, `[\s\-]+`) + `\b`
		re, err := regexp.Compile(patternStr)
		if err != nil {
			continue
		}

		compiled = append(compiled, compiledKeyword{
			raw:       strings.ToLower(kw),
			pattern:   re,
			wordCount: len(words),
		})
	}

	// If category already exists in rules, append keywords; otherwise add new rule
	for i := range c.rules {
		if strings.EqualFold(c.rules[i].category, catName) {
			c.rules[i].keywords = append(c.rules[i].keywords, compiled...)
			return
		}
	}

	c.rules = append(c.rules, compiledRule{
		category: catName,
		keywords: compiled,
	})
}

// Classify determines the best matching category from title and description.
func (c *Classifier) Classify(title, description string) string {
	res := c.ClassifyDetailed(title, description)
	return res.Category
}

// ClassifyArticle determines and updates the category for a canonical model.Article.
func (c *Classifier) ClassifyArticle(article *model.Article) string {
	if article == nil {
		return c.defaultCategory
	}
	cat := c.Classify(article.Title, strings.Join(article.Description, " "))
	article.Category = cat
	return cat
}

// ClassifyDetailed evaluates title and description against all registered category rules.
func (c *Classifier) ClassifyDetailed(title, description string) ClassificationResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	normTitle := c.normalizeText(title)
	normDesc := c.normalizeText(description)

	var bestCategory string
	var bestScore int
	var bestTitleScore int
	var bestDescScore int
	var bestKeywords []string

	for _, rule := range c.rules {
		titleScore, titleMatches := c.evaluateField(normTitle, rule.keywords, c.titleWeight)
		descScore, descMatches := c.evaluateField(normDesc, rule.keywords, c.descWeight)

		totalScore := titleScore + descScore

		if totalScore > bestScore {
			bestScore = totalScore
			bestTitleScore = titleScore
			bestDescScore = descScore
			bestCategory = rule.category

			// Combine unique matches
			matchMap := make(map[string]bool)
			bestKeywords = nil
			for _, m := range append(titleMatches, descMatches...) {
				if !matchMap[m] {
					matchMap[m] = true
					bestKeywords = append(bestKeywords, m)
				}
			}
		} else if totalScore > 0 && totalScore == bestScore {
			// Deterministic tie-breaker: title score priority
			if titleScore > bestTitleScore {
				bestScore = totalScore
				bestTitleScore = titleScore
				bestDescScore = descScore
				bestCategory = rule.category
			}
		}
	}

	if bestScore <= 0 || bestCategory == "" {
		return ClassificationResult{
			Category:        c.defaultCategory,
			Score:           0,
			TitleScore:      0,
			DescScore:       0,
			MatchedKeywords: nil,
		}
	}

	return ClassificationResult{
		Category:        bestCategory,
		Score:           bestScore,
		TitleScore:      bestTitleScore,
		DescScore:       bestDescScore,
		MatchedKeywords: bestKeywords,
	}
}

func (c *Classifier) evaluateField(text string, keywords []compiledKeyword, weight int) (int, []string) {
	if text == "" {
		return 0, nil
	}

	score := 0
	var matches []string
	matchedRaw := make(map[string]bool)

	for _, kw := range keywords {
		if kw.pattern.MatchString(text) {
			if !matchedRaw[kw.raw] {
				matchedRaw[kw.raw] = true
				// Multi-word terms get proportional weight bonus for specificity
				score += kw.wordCount * weight
				matches = append(matches, kw.raw)
			}
		}
	}

	return score, matches
}

// normalizeText cleans punctuation and unifies spacing for reliable matching.
func (c *Classifier) normalizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || r == '-' || r == '\'' {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}

	return b.String()
}

// Categories returns the list of registered categories.
func (c *Classifier) Categories() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cats := make([]string, 0, len(c.rules)+1)
	hasDefault := false

	for _, r := range c.rules {
		cats = append(cats, r.category)
		if strings.EqualFold(r.category, c.defaultCategory) {
			hasDefault = true
		}
	}

	if !hasDefault {
		cats = append(cats, c.defaultCategory)
	}

	return cats
}

// defaultCategoryRules returns the initial rule set for standard medical categories (bilingual EN/ID).
func defaultCategoryRules() []CategoryRule {
	return []CategoryRule{
		{
			Category: CategoryVaccination,
			Keywords: []string{
				"vaccine", "vaccines", "vaccination", "vaccinations",
				"immunization", "immunizations", "immunise", "immunize", "immunized",
				"booster shot", "booster shots", "mrna vaccine", "inoculation", "inoculations",
				"flu vaccine", "covid vaccine", "childhood vaccine", "childhood vaccines",
				"vaccine safety", "antigen",
				"vaksin", "vaksinasi", "imunisasi", "imunisasi anak", "booster",
			},
		},
		{
			Category: CategoryCancer,
			Keywords: []string{
				"cancer", "cancers", "tumor", "tumors", "tumour", "tumours",
				"oncology", "carcinoma", "carcinomas", "melanoma", "leukemia", "leukaemia",
				"lymphoma", "chemotherapy", "radiation therapy", "metastasis", "metastatic",
				"malignant", "malignancy", "biopsy", "sarcoma", "neoplasm", "neoplasms",
				"breast cancer", "lung cancer", "prostate cancer", "colorectal cancer",
				"skin cancer", "chemo",
				"kanker", "kanker payudara", "kanker serviks", "kanker paru", "kemoterapi", "radioterapi",
			},
		},
		{
			Category: CategoryDiabetes,
			Keywords: []string{
				"diabetes", "diabetic", "diabetics", "insulin", "blood sugar",
				"blood glucose", "glucose", "hyperglycemia", "hypoglycemia",
				"type 1 diabetes", "type 2 diabetes", "gestational diabetes",
				"prediabetes", "a1c", "ketoacidosis", "glucometer",
				"hypoglycemic", "hyperglycemic",
				"diabetes melitus", "diabetes melitus tipe 1", "diabetes melitus tipe 2",
				"kencing manis", "gula darah", "glukosa darah",
			},
		},
		{
			Category: CategoryHeartDisease,
			Keywords: []string{
				"heart disease", "cardiovascular", "cardiac", "heart attack",
				"hypertension", "high blood pressure", "coronary artery", "coronary",
				"stroke", "strokes", "arrhythmia", "atherosclerosis", "cholesterol",
				"heart failure", "angina", "myocardial infarction", "blood pressure",
				"cardiomyopathy", "atrial fibrillation", "arterial", "vascular",
				"penyakit jantung", "jantung koroner", "gagal jantung", "serangan jantung",
				"kardiovaskular", "hipertensi", "tekanan darah tinggi",
			},
		},
		{
			Category: CategoryInfectiousDisease,
			Keywords: []string{
				"infectious disease", "infection", "infections", "infectious",
				"virus", "viruses", "viral", "bacteria", "bacterial", "pathogen", "pathogens",
				"antibiotic", "antibiotics", "influenza", "flu", "covid", "covid-19", "coronavirus",
				"pneumonia", "tuberculosis", "hepatitis", "hiv", "aids", "malaria", "dengue",
				"sepsis", "fungal", "parasite", "parasites", "contagious", "epidemic", "pandemic",
				"strep", "staph", "measles", "shingles", "lyme disease",
				"penyakit menular", "infeksi", "demam berdarah", "tbc", "tuberkulosis", "campak", "antraks",
			},
		},
		{
			Category: CategoryMentalHealth,
			Keywords: []string{
				"mental health", "depression", "depressive", "anxiety", "anxious",
				"bipolar", "schizophrenia", "psychiatry", "psychiatric", "psychological",
				"psychology", "psychotherapy", "stress", "ptsd", "autism", "adhd",
				"dementia", "suicide", "suicidal", "eating disorder", "insomnia",
				"mood disorder", "panic disorder", "addiction", "substance abuse",
				"trauma", "grief",
				"kesehatan mental", "gangguan jiwa", "gangguan kecemasan", "depresi", "autisme",
			},
		},
		{
			Category: CategoryNutrition,
			Keywords: []string{
				"nutrition", "nutritional", "diet", "diets", "dietary",
				"nutrient", "nutrients", "vitamin", "vitamins", "mineral", "minerals",
				"obesity", "obese", "overweight", "malnutrition", "fiber", "protein",
				"carbohydrate", "carbohydrates", "weight loss", "healthy eating",
				"calorie", "calories", "dietitian", "supplement", "supplements",
				"antioxidants", "hydration", "fasting",
				"gizi", "nutrisi", "stunting", "pola makan", "kurang gizi", "obesitas",
			},
		},
		{
			Category: CategoryNeurology,
			Keywords: []string{
				"neurology", "neurological", "nervous system", "brain", "brain injury",
				"alzheimer", "alzheimer's", "alzheimers", "parkinson", "parkinson's", "parkinsons",
				"epilepsy", "epileptic", "seizure", "seizures", "multiple sclerosis",
				"neuropathy", "migraine", "migraines", "headache", "headaches",
				"neurodegenerative", "neuro", "spinal cord", "concussion", "cerebral",
				"kelainan saraf", "saraf", "otak", "demensia", "epilepsi",
			},
		},
		{
			Category: CategoryPharmacy,
			Keywords: []string{
				"pharmacy", "pharmaceutical", "pharmacology", "pharmacist",
				"medication", "medications", "prescription", "prescriptions",
				"drug", "drugs", "dosage", "medicine", "medicines",
				"pill", "pills", "tablet", "tablets", "adverse reaction",
				"side effect", "side effects", "overdose", "drug interaction",
				"contraindication", "over the counter",
				"obat", "farmasi", "resep dokter", "dosis obat", "efek samping",
			},
		},
		{
			Category: CategoryGym,
			Keywords: []string{
				"gym", "nge-gym", "nge gym", "fitness", "fitness center", "angkat beban",
				"treadmill", "barbell", "dumbbell", "bina raga", "workout", "kebugaran fisik",
				"latihan beban", "strength training", "bodybuilding", "pusat kebugaran",
			},
		},
	}
}
