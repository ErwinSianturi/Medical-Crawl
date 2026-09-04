package model

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
)

type District struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type City struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Districts []District `json:"districts"`
}

type Province struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Cities []City `json:"cities"`
}

var GeoData []Province

func LoadGeoData(filePath string) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Printf("Warning: Failed to load geo data from %s: %v", filePath, err)
		return
	}
	err = json.Unmarshal(data, &GeoData)
	if err != nil {
		log.Printf("Warning: Failed to unmarshal geo data: %v", err)
	} else {
		log.Printf("Successfully loaded %d provinces from %s", len(GeoData), filePath)
	}
}

func ValidateLocationHierarchy(province, city, district string) error {
	if province == "" {
		return nil
	}

	var foundProv *Province
	for i := range GeoData {
		if strings.EqualFold(GeoData[i].Name, province) {
			foundProv = &GeoData[i]
			break
		}
	}
	if foundProv == nil {
		return fmt.Errorf("invalid province: %s", province)
	}

	if city == "" {
		return nil
	}

	var foundCity *City
	for i := range foundProv.Cities {
		if strings.EqualFold(foundProv.Cities[i].Name, city) {
			foundCity = &foundProv.Cities[i]
			break
		}
	}
	if foundCity == nil {
		return fmt.Errorf("invalid city '%s' for province '%s'", city, province)
	}

	if district == "" || strings.EqualFold(district, "All Districts") {
		return nil
	}

	var foundDist *District
	for i := range foundCity.Districts {
		if strings.EqualFold(foundCity.Districts[i].Name, district) {
			foundDist = &foundCity.Districts[i]
			break
		}
	}
	if foundDist == nil {
		return fmt.Errorf("invalid district '%s' for city '%s'", district, city)
	}

	return nil
}

// CleanCityName strips "KOTA ", "KABUPATEN ", "KAB. " prefixes and whitespace
func CleanCityName(city string) string {
	c := strings.ToLower(strings.TrimSpace(city))
	c = strings.ReplaceAll(c, "kota ", "")
	c = strings.ReplaceAll(c, "kabupaten ", "")
	c = strings.ReplaceAll(c, "kab. ", "")
	return strings.TrimSpace(c)
}

// GetDistrictsForCity returns a list of district names for a given city
func GetDistrictsForCity(city string) []string {
	var districts []string
	cleanCity := CleanCityName(city)

	for _, p := range GeoData {
		for _, c := range p.Cities {
			cleanC := CleanCityName(c.Name)
			if strings.EqualFold(c.Name, city) || (cleanCity != "" && strings.EqualFold(cleanC, cleanCity)) {
				for _, d := range c.Districts {
					districts = append(districts, strings.Title(strings.ToLower(d.Name)))
				}
				if len(districts) > 0 {
					return districts
				}
			}
		}
	}
	// Fallbacks
	if strings.EqualFold(city, "Surabaya") || strings.EqualFold(city, "KOTA SURABAYA") || cleanCity == "surabaya" {
		return []string{
			"Asemrowo", "Benowo", "Bubutan", "Bulak", "Dukuh Pakis", "Gayungan", "Genteng",
			"Gubeng", "Gunung Anyar", "Jambangan", "Karangpilang", "Kenjeran",
			"Krembangan", "Lakarsantri", "Mulyorejo", "Pabean Cantian", "Pakal", "Rungkut",
			"Sambikerep", "Sawahan", "Semampir", "Simokerto", "Sukolilo", "Sukomanunggal",
			"Tambaksari", "Tandes", "Tegalsari", "Tenggilis Mejoyo", "Wiyung", "Wonocolo",
			"Wonokromo",
		}
	} else if strings.EqualFold(city, "Jakarta") || strings.EqualFold(city, "DKI JAKARTA") || cleanCity == "jakarta" {
		return []string{
			"Cempaka Putih", "Gambir", "Johar Baru", "Kemayoran", "Menteng", "Sawah Besar", "Senen", "Tanah Abang",
			"Cilincing", "Kelapa Gading", "Koja", "Pademangan", "Penjaringan", "Tanjung Priok",
			"Cengkareng", "Grogol Petamburan", "Kalideres", "Kebon Jeruk", "Kembangan", "Palmerah", "Taman Sari", "Tambora",
			"Cilandak", "Jagakarsa", "Kebayoran Baru", "Kebayoran Lama", "Mampang Prapatan", "Pancoran", "Pasar Minggu", "Pesanggrahan", "Setiabudi", "Tebet",
			"Cakung", "Cipayung", "Ciracas", "Duren Sawit", "Jatinegara", "Kramat Jati", "Makasar", "Matraman", "Pasar Rebo", "Pulo Gadung",
		}
	}
	return districts
}

// GetAllCities returns a map of all valid city names to their original names, including stripped variants
func GetAllCities() []string {
	var cities []string
	for _, p := range GeoData {
		for _, c := range p.Cities {
			cities = append(cities, c.Name)
		}
	}
	return cities
}

// GetProvinceForCity returns the province name for a given city
func GetProvinceForCity(cityName string) string {
	for _, p := range GeoData {
		for _, c := range p.Cities {
			if strings.EqualFold(c.Name, cityName) {
				return p.Name
			}
		}
	}
	return ""
}

// GetDistrictsForProvince returns all districts within a given province
func GetDistrictsForProvince(provinceName string) []string {
	var districts []string
	for _, p := range GeoData {
		if strings.EqualFold(p.Name, provinceName) {
			for _, c := range p.Cities {
				for _, d := range c.Districts {
					districts = append(districts, strings.Title(strings.ToLower(d.Name)))
				}
			}
			break
		}
	}
	return districts
}
