package classifier

import (
	"testing"

	"maps-scraper/pkg/model"
)

func TestClassifier_AllCategories(t *testing.T) {
	c := NewDefaultClassifier()

	tests := []struct {
		name         string
		title        string
		desc         string
		wantCategory string
	}{
		{
			name:         "Cancer Topic",
			title:        "Understanding Breast Cancer Symptoms and Treatment",
			desc:         "Chemotherapy and radiation therapy are common clinical options for malignant tumor eradication.",
			wantCategory: CategoryCancer,
		},
		{
			name:         "Diabetes Topic",
			title:        "Living with Type 2 Diabetes",
			desc:         "Monitoring blood glucose and insulin levels prevents serious hyperglycemia and complications.",
			wantCategory: CategoryDiabetes,
		},
		{
			name:         "Heart Disease Topic",
			title:        "Hypertension and Coronary Artery Health",
			desc:         "High blood pressure increases the risk of heart attack, stroke, and cardiovascular disease.",
			wantCategory: CategoryHeartDisease,
		},
		{
			name:         "Infectious Disease Topic",
			title:        "Avian Influenza Virus Outbreak Guidelines",
			desc:         "The airborne pathogen causes severe respiratory infection and pneumonia in human hosts.",
			wantCategory: CategoryInfectiousDisease,
		},
		{
			name:         "Mental Health Topic",
			title:        "Recognizing Depression and Anxiety in Young Adults",
			desc:         "Psychological therapy and early counseling help alleviate chronic stress and psychiatric distress.",
			wantCategory: CategoryMentalHealth,
		},
		{
			name:         "Nutrition Topic",
			title:        "Dietary Fiber and Micronutrient Balance",
			desc:         "Essential vitamins, minerals, and caloric management aid in preventing obesity and malnutrition.",
			wantCategory: CategoryNutrition,
		},
		{
			name:         "Vaccination Topic",
			title:        "Childhood Immunization Schedules and Booster Shots",
			desc:         "The annual flu vaccine and routine vaccinations trigger antigen antibodies to build lasting immunity.",
			wantCategory: CategoryVaccination,
		},
		{
			name:         "Neurology Topic",
			title:        "Early Detection of Alzheimer's Disease and Memory Loss",
			desc:         "Neurodegenerative disorders impact the central nervous system, leading to chronic migraines and seizures.",
			wantCategory: CategoryNeurology,
		},
		{
			name:         "Pharmacy Topic",
			title:        "Prescription Medication Safety and Dosage Guidelines",
			desc:         "Consult your pharmacist regarding potential drug interactions, pill dosage, and adverse side effects.",
			wantCategory: CategoryPharmacy,
		},
		{
			name:         "General Health Fallback",
			title:        "Annual Wellness Checkup: What to Expect",
			desc:         "Routine preventive visits help doctors assess vital signs and overall bodily well-being.",
			wantCategory: CategoryGeneralHealth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Classify(tt.title, tt.desc)
			if got != tt.wantCategory {
				t.Errorf("Classify(%q, %q) = %q, want %q", tt.title, tt.desc, got, tt.wantCategory)
			}
		})
	}
}

func TestClassifier_CaseInsensitivity(t *testing.T) {
	c := NewDefaultClassifier()

	cases := []struct {
		title string
		desc  string
		want  string
	}{
		{
			title: "DIABETES MANAGEMENT AND INSULIN PUMPS",
			desc:  "BLOOD GLUCOSE MONITORING REDUCES HYPERGLYCEMIA RISKS.",
			want:  CategoryDiabetes,
		},
		{
			title: "cAnCeR oNcOlOgY sCrEeNiNg",
			desc:  "mAlIgNaNt TuMoR dEtEcTiOn SaVeS lIvEs.",
			want:  CategoryCancer,
		},
	}

	for _, tc := range cases {
		got := c.Classify(tc.title, tc.desc)
		if got != tc.want {
			t.Errorf("Case insensitivity failed: got %q, want %q", got, tc.want)
		}
	}
}

func TestClassifier_WordBoundaryProtection(t *testing.T) {
	c := NewDefaultClassifier()

	// "fluid" should NOT trigger Infectious Disease ("flu")
	// "chive" should NOT trigger Infectious Disease ("hiv")
	// "spill" should NOT trigger Pharmacy ("pill")
	tests := []struct {
		name        string
		title       string
		desc        string
		mustNotHave string
	}{
		{
			name:        "Fluid Intake should not match flu",
			title:       "Maintaining Adequate Fluid Intake During Summer",
			desc:        "Hydration is key for proper body fluid regulation.",
			mustNotHave: CategoryInfectiousDisease,
		},
		{
			name:        "Spill should not match pill",
			title:       "Chemical Spill Protocol in Industrial Plants",
			desc:        "Emergency procedures for industrial fluid spill containment.",
			mustNotHave: CategoryPharmacy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := c.ClassifyDetailed(tt.title, tt.desc)
			if res.Category == tt.mustNotHave {
				t.Errorf("Word boundary violation: got %q with matched keywords %v", res.Category, res.MatchedKeywords)
			}
		})
	}
}

func TestClassifier_TitlePriority(t *testing.T) {
	c := NewDefaultClassifier()

	// Title clearly indicates Cancer, but description mentions nutritional support for cancer patients
	title := "Chemotherapy Treatment for Lung Cancer"
	desc := "Maintaining a healthy diet, caloric intake, and vitamin balance supports patients."

	res := c.ClassifyDetailed(title, desc)
	if res.Category != CategoryCancer {
		t.Errorf("Expected Title priority for Cancer, got %q (score=%d, titleScore=%d, descScore=%d)",
			res.Category, res.Score, res.TitleScore, res.DescScore)
	}
}

func TestClassifier_VaccineVsInfectionSpecificTieBreak(t *testing.T) {
	c := NewDefaultClassifier()

	// Title specifies flu vaccine. Vaccination should score higher due to multi-word phrase matching
	title := "Annual Flu Vaccine Recommendations"
	desc := "Getting the seasonal flu vaccine prevents influenza infection in high risk populations."

	res := c.ClassifyDetailed(title, desc)
	if res.Category != CategoryVaccination {
		t.Errorf("Expected Vaccination category, got %q (keywords: %v)", res.Category, res.MatchedKeywords)
	}
}

func TestClassifier_PunctuationAndHyphenNormalization(t *testing.T) {
	c := NewDefaultClassifier()

	tests := []struct {
		title string
		desc  string
		want  string
	}{
		{
			title: "Managing Type-2-Diabetes Effectively!",
			desc:  "Blood-sugar checks and HbA1c testing: critical steps.",
			want:  CategoryDiabetes,
		},
		{
			title: "Combating COVID-19 Variants",
			desc:  "Coronavirus airborne transmission requires mask protocols.",
			want:  CategoryInfectiousDisease,
		},
		{
			title: "Living with Parkinson's and Alzheimer's",
			desc:  "Neurological degeneration and cognitive decline.",
			want:  CategoryNeurology,
		},
	}

	for _, tt := range tests {
		got := c.Classify(tt.title, tt.desc)
		if got != tt.want {
			t.Errorf("Normalization failed for %q: got %q, want %q", tt.title, got, tt.want)
		}
	}
}

func TestClassifier_Extensibility(t *testing.T) {
	c := NewDefaultClassifier()

	// Verify Dermatology does not exist initially
	dermatologyTitle := "Treating Severe Eczema and Psoriasis"
	dermatologyDesc := "Topical corticosteroid creams reduce skin inflammation and dermatologic flare-ups."

	initial := c.Classify(dermatologyTitle, dermatologyDesc)
	// Should match General Health or Pharmacy (due to creams/corticosteroid)
	if initial == "Dermatology" {
		t.Fatalf("Unexpected early match for Dermatology")
	}

	// Dynamically extend with Dermatology rule
	c.AddRule(CategoryRule{
		Category: "Dermatology",
		Keywords: []string{"eczema", "psoriasis", "dermatology", "dermatologic", "skin inflammation", "dermatitis"},
	})

	after := c.Classify(dermatologyTitle, dermatologyDesc)
	if after != "Dermatology" {
		t.Errorf("Expected extensible rule to classify as Dermatology, got %q", after)
	}

	// Verify Categories() contains Dermatology
	found := false
	for _, cat := range c.Categories() {
		if cat == "Dermatology" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected Categories() to contain Dermatology")
	}
}

func TestClassifier_ClassifyArticle(t *testing.T) {
	c := NewDefaultClassifier()

	art := &model.Article{
		Title:       "Pediatric Vaccination Safety Standards",
		Category:    "Unknown",
		Image:       "https://example.com/img.jpg",
		Description: []string{"Routine childhood immunization builds community immunity against deadly pathogens."},
	}

	cat := c.ClassifyArticle(art)
	if cat != CategoryVaccination {
		t.Errorf("ClassifyArticle failed: got %q, want %q", cat, CategoryVaccination)
	}
	if art.Category != CategoryVaccination {
		t.Errorf("Article.Category not mutated: got %q, want %q", art.Category, CategoryVaccination)
	}
}

func TestClassifier_EmptyInputsSafeFallback(t *testing.T) {
	c := NewDefaultClassifier()

	got := c.Classify("", "")
	if got != CategoryGeneralHealth {
		t.Errorf("Empty inputs want %q, got %q", CategoryGeneralHealth, got)
	}

	gotArticle := c.ClassifyArticle(nil)
	if gotArticle != CategoryGeneralHealth {
		t.Errorf("Nil article want %q, got %q", CategoryGeneralHealth, gotArticle)
	}
}
