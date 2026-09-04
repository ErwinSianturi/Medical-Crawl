package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"maps-scraper/pkg/model"
)

var DB *sql.DB

func InitDB() error {
	host := getEnv("DB_HOST", "127.0.0.1")
	port := getEnv("DB_PORT", "3306")
	user := getEnv("DB_USER", "root")
	pass := getEnv("DB_PASSWORD", "")
	dbName := getEnv("DB_NAME", "db_3rdtryscraping")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=utf8mb4&parseTime=True&loc=Local", user, pass, host, port)
	
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}

	// Create database if not exists
	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", dbName))
	if err != nil {
		return err
	}

	db.Close() // Re-open with dbname

	dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", user, pass, host, port, dbName)
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		return err
	}

	DB.SetMaxOpenConns(50)
	DB.SetMaxIdleConns(10)

	// Create table
	if err := createTables(); err != nil {
		return err
	}
	
	log.Printf("[DATABASE] MySQL initialized at %s:%s/%s", host, port, dbName)
	return nil
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func createTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS stores (
		id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		place_id VARCHAR(255) UNIQUE NULL,
		name VARCHAR(255) NOT NULL,
		address TEXT,
		province VARCHAR(100),
		city VARCHAR(100),
		kecamatan VARCHAR(100),
		latitude DECIMAL(11, 8),
		longitude DECIMAL(11, 8),
		types VARCHAR(255),
		rating DECIMAL(3, 1),
		phone VARCHAR(100),
		status VARCHAR(50),
		opening_hours TEXT,
		photo_url TEXT,
		website_links TEXT,
		semen_yang_dijual VARCHAR(255),
		link_setingan_titik TEXT,
		link_setingan_koma TEXT,
		worker_id INT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		INDEX idx_province (province),
		INDEX idx_city (city),
		INDEX idx_kecamatan (kecamatan),
		INDEX idx_lat_lon (latitude, longitude)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`

	_, err := DB.Exec(query)
	return err
}

func SaveRecord(rec *model.StoreRecord) error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	query := `
		INSERT INTO stores (
			place_id, name, address, province, city, kecamatan,
			latitude, longitude, types, rating, phone, status,
			opening_hours, photo_url, website_links, semen_yang_dijual,
			link_setingan_titik, link_setingan_koma, worker_id
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)
		ON DUPLICATE KEY UPDATE
			name = VALUES(name),
			address = VALUES(address),
			province = VALUES(province),
			city = VALUES(city),
			kecamatan = VALUES(kecamatan),
			latitude = VALUES(latitude),
			longitude = VALUES(longitude),
			types = VALUES(types),
			rating = VALUES(rating),
			phone = COALESCE(VALUES(phone), phone),
			status = VALUES(status),
			opening_hours = VALUES(opening_hours),
			photo_url = VALUES(photo_url),
			website_links = VALUES(website_links),
			semen_yang_dijual = VALUES(semen_yang_dijual),
			link_setingan_titik = VALUES(link_setingan_titik),
			link_setingan_koma = VALUES(link_setingan_koma),
			worker_id = VALUES(worker_id)
	`

	province := model.GetProvinceForCity(rec.City)

	_, err := DB.Exec(query,
		nullIf(rec.PlaceID, "", "UNKNOWN", "N/A", "-"),
		rec.Name,
		nullIf(rec.Address, "", "UNKNOWN", "N/A", "-"),
		nullIf(province, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.City, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.Kecamatan, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.Latitude, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.Longitude, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.Types, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.Rating, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.Phone, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.Status, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.OpeningHours, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.PhotoURL, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.WebsiteLinks, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.SemenYangDijual, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.LinkSetinganTitik, "", "UNKNOWN", "N/A", "-"),
		nullIf(rec.LinkSetinganKoma, "", "UNKNOWN", "N/A", "-"),
		rec.WorkerID,
	)
	return err
}

func nullIf(val string, checks ...string) interface{} {
	if val == "" {
		return nil
	}
	for _, c := range checks {
		if val == c {
			return nil
		}
	}
	return val
}
