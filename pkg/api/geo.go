package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"maps-scraper/pkg/model"
)

func LoadGeoData(filePath string) {
	model.LoadGeoData(filePath)
}

func (s *Server) handleProvinces(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	var provinces []string
	for _, p := range model.GeoData {
		provinces = append(provinces, p.Name)
	}
	sort.Strings(provinces)
	_ = json.NewEncoder(w).Encode(provinces)
}

func (s *Server) handleCities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	provinceName := r.URL.Query().Get("province")
	var cities []string

	for _, p := range model.GeoData {
		if strings.EqualFold(p.Name, provinceName) {
			for _, c := range p.Cities {
				cities = append(cities, c.Name)
			}
			break
		}
	}
	sort.Strings(cities)
	_ = json.NewEncoder(w).Encode(cities)
}

func (s *Server) handleDistricts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	cityName := r.URL.Query().Get("city")
	districts := model.GetDistrictsForCity(cityName)

	sort.Strings(districts)
	_ = json.NewEncoder(w).Encode(districts)
}

func ValidateLocationHierarchy(province, city, district string) error {
	return model.ValidateLocationHierarchy(province, city, district)
}
