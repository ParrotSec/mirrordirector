package db

import (
	"database/sql"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	_ "github.com/mattn/go-sqlite3"
	"strings"
)

type Cacher interface {
	AddFile(filePath string)
	RemoveFile(filePath string)
	ExistsFile(filePath string) bool
	GetAllFiles() []string
	AddMirror(name string, url string, continent string, country string, blockedCountries []string)
	DeleteMirror(name string)
	GetFileLink(filePath string, country string) (string, string, error)
	AddRecord(filePath string, mirror string, skip bool) error
	DeleteRecord(mirrorName, fileName string) error
}

var ErrMirrorWasNotFound = errors.New("cache: mirror was not found")
var ErrFileWasNotFound = errors.New("cache: file wasn't found")
var ErrRecordWasNotFound = errors.New("cache: record wasn't found")
var ErrSkipFile = errors.New("cache: file wasn't found in any repository")

type cache struct {
	dbPath string
}

var Cache Cacher

// Creates initial database
func CreateCacheDatabase(path string) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		log.Fatalf("Failed to connect to cache database: %v", err)
	}
	defer db.Close()

	// files table, serves for files list
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS files (
    	"id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    	"name" TEXT NOT NULL,
    	UNIQUE(name)
	);`); err != nil {
		log.Fatal(err)
	}

	// mirrors, serves for mirrors list
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS mirrors (
    	"id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    	"name" TEXT NOT NULL,
    	"url" TEXT NOT NULL,
    	"blocked_countries" TEXT NOT NULL,
    	"country" VARCHAR(2) NOT NULL,
    	"continent" VARCHAR(2) NOT NULL,
    	UNIQUE(name)
	);`); err != nil {
		log.Fatal(err)
	}

	// records, serves for records of files marked by flag
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS records (
    	"id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    	"file_id" TEXT NOT NULL,
    	"mirror_id" TEXT NOT NULL,
    	"flag" text CHECK("flag" IN ('SKIP', 'PASS')) NOT NULL,
    	FOREIGN KEY(file_id) REFERENCES files(id),
    	FOREIGN KEY(mirror_id) REFERENCES mirrors(id)
	);`); err != nil {
		log.Fatal(err)
	}
	Cache = &cache{path}
}

func UseCache(c Cacher) {
	Cache = c
}

// Inserts a file to the files table if one does not exist
func (c cache) AddFile(filePath string) {
	db, err := sql.Open("sqlite3", c.dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to cache database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("INSERT OR IGNORE INTO files (name) VALUES (?);",
		filePath); err != nil {
		log.Fatal(err)
	}
}

// Removes a file from the files table
func (c cache) RemoveFile(filePath string) {
	db, err := sql.Open("sqlite3", c.dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to cache database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("DELETE FROM files WHERE name=?;",
		filePath); err != nil {
		log.Fatal(err)
	}
}

func (c cache) ExistsFile(filePath string) bool {
	db, err := sql.Open("sqlite3", c.dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to cache database: %v", err)
	}
	defer db.Close()


	exists := 0
	if err := db.QueryRow("SELECT COUNT(1) FROM files WHERE name=?;",
		filePath).Scan(&exists); err != nil && err != sql.ErrNoRows {
		log.Fatal(err)
	} else {
		if err == sql.ErrNoRows {
			return false
		}
	}
	return true
}

func (c cache) GetAllFiles() (files []string) {
	db, err := sql.Open("sqlite3", c.dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to cache database: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT name FROM files;")
	if err != nil {
		log.Fatal(err)
	}

	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			log.Fatal(err)
		}

		files = append(files, s)
	}

	return
}

// Inserts a file to the files table if one does not exist
func (c cache) AddMirror(name string, url string, continent string, country string,
	blockedCountries []string) {
	db, err := sql.Open("sqlite3", c.dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to cache database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("INSERT or IGNORE INTO mirrors (name, url, continent, country," +
		" blocked_countries) VALUES (?, ?, ?, ?, ?);",
		name, url, continent, country, strings.Join(blockedCountries, ", ")); err != nil {
		log.Fatal(err)
	}
}

// Removes a mirror from the mirrors table
func (c cache) DeleteMirror(name string) {
	db, err := sql.Open("sqlite3", c.dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to cache database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("DELETE FROM mirrors WHERE name=?;",
		name); err != nil {
		log.Fatal(err)
	}
}

// Checks if a cache record on a filename exists for specific mirror
// Returns a link and a mirror name
func (c cache) GetFileLink(filePath string, country string) (string, string, error) {
	db, err := sql.Open("sqlite3", c.dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to cache database: %v", err)
	}
	defer db.Close()

	var fileId int
	var flag string

	type mirrorNameIdUrl struct {
		id int
		name string
		url string
	}

	var matchedMirrors []mirrorNameIdUrl

	if rows, err := db.Query("SELECT id, name, url FROM mirrors WHERE country=?;",
		country); err != nil {
		if err == sql.ErrNoRows {
			return "", "", ErrMirrorWasNotFound
		} else {
			log.Fatal(err)
		}
	} else {
		for rows.Next() {
			var m mirrorNameIdUrl
			if err := rows.Scan(&m.id, &m.name, &m.url); err != nil {
				return "", "", err
			}

			matchedMirrors = append(matchedMirrors, m)
		}
	}

	if err := db.QueryRow("SELECT id FROM files WHERE name=?;",
		filePath).Scan(&fileId); err != nil {
		if err == sql.ErrNoRows {
			return "", "", ErrFileWasNotFound
		} else {
			log.Fatal(err)
		}
	}

	for _, m := range matchedMirrors {
		if err := db.QueryRow("SELECT flag FROM records WHERE file_id=? AND mirror_id=?;",
			fileId, m.id).Scan(&flag); err != nil {
			if err == sql.ErrNoRows {
				continue
			} else {
				log.Fatal(err)
			}
		}

		if flag == "SKIP" {
			continue
		}

		return fmt.Sprintf("%s/%s", m.url, filePath), m.name, nil
	}



	return "", "", ErrRecordWasNotFound
}

func (c cache) AddRecord(filePath string, mirror string, skip bool) error {
	db, err := sql.Open("sqlite3", c.dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to cache database: %v", err)
	}
	defer db.Close()

	var mirrorId, fileId int

	if !skip {
		if err := db.QueryRow("SELECT id FROM mirrors WHERE name=?;",
			mirror).Scan(&mirrorId); err != nil {
			if err == sql.ErrNoRows {
				return ErrMirrorWasNotFound
			} else {
				log.Fatal(err)
			}
		}
	}

	if err := db.QueryRow("SELECT id FROM files WHERE name=?;",
		filePath).Scan(&fileId); err != nil {
		if err == sql.ErrNoRows {
			return ErrFileWasNotFound
		} else {
			log.Fatal(err)
		}
	}

	var flag string
	if skip {
		flag = "SKIP"
	} else {
		flag = "PASS"
	}
	if _, err := db.Exec("REPLACE INTO records (file_id, mirror_id, " +
		"flag) VALUES (?, ?, ?)",
		fileId, mirrorId, flag); err != nil {
		log.Fatal(err)
	}

	return nil
}

// Removes cache record
func (c cache) DeleteRecord(mirrorName, fileName string) error {
	db, err := sql.Open("sqlite3", c.dbPath)
	if err != nil {
		log.Fatalf("Failed to connect to cache database: %v", err)
	}
	defer db.Close()

	var mirrorId, fileId int

	if err := db.QueryRow("SELECT id FROM mirrors WHERE name=?;",
		mirrorName).Scan(&mirrorId); err != nil {
		if err == sql.ErrNoRows {
			return ErrMirrorWasNotFound
		} else {
			log.Fatal(err)
		}
	}

	if err := db.QueryRow("SELECT id FROM files WHERE name=?;",
		fileName).Scan(&fileId); err != nil {
		if err == sql.ErrNoRows {
			return ErrFileWasNotFound
		} else {
			log.Fatal(err)
		}
	}

	if _, err := db.Exec("DELETE FROM records WHERE mirror_id=? AND file_id=? ;",
		mirrorId, fileId); err != nil {
		log.Fatal(err)
	}

	return nil
}