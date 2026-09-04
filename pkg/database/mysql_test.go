package database_test

import (
	"os"
	"testing"

	"maps-scraper/pkg/database"
	"maps-scraper/pkg/model"
)

func TestMySQL_InsertDuplicateUpdate(t *testing.T) {
	// Use a test database
	os.Setenv("DB_NAME", "db_3rdtryscraping_test")
	defer os.Unsetenv("DB_NAME")

	err := database.InitDB()
	if err != nil {
		t.Skipf("Skipping DB test because DB cannot be initialized: %v", err)
	}
	defer database.DB.Close()

	// Ensure connection is valid
	if err := database.DB.Ping(); err != nil {
		t.Skipf("Skipping DB test, MySQL not reachable: %v", err)
	}

	// Make sure the table is clean
	_, _ = database.DB.Exec("DELETE FROM stores WHERE place_id = 'test_place_id'")

	rec := &model.StoreRecord{
		PlaceID:   "test_place_id",
		Name:      "Toko Test",
		Address:   "Jl Test",
		City:      "KOTA SURABAYA",
		Kecamatan: "TEGALSARI",
		Latitude:  "-7.1",
		Longitude: "112.1",
		WorkerID:  1,
	}

	// 1. Insert
	err = database.SaveRecord(rec)
	if err != nil {
		t.Fatalf("Failed to insert record: %v", err)
	}

	// 2. Insert Duplicate (Should Upsert/Update without error)
	rec.Name = "Toko Test Updated"
	err = database.SaveRecord(rec)
	if err != nil {
		t.Fatalf("Failed to upsert duplicate record: %v", err)
	}

	// 3. Verify Update
	var name string
	err = database.DB.QueryRow("SELECT name FROM stores WHERE place_id = ?", rec.PlaceID).Scan(&name)
	if err != nil {
		t.Fatalf("Failed to query inserted record: %v", err)
	}
	if name != "Toko Test Updated" {
		t.Errorf("Expected name 'Toko Test Updated', got '%s'", name)
	}

	// Clean up
	_, _ = database.DB.Exec("DELETE FROM stores WHERE place_id = 'test_place_id'")
}
