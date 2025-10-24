package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/time/rate"
	_ "modernc.org/sqlite"
)

const geoIdArgentina = "100446943"

type JobID = string

type JobPosting struct {
	JobID       string `json:"job_id"`
	Company     string `json:"company"`
	Description string `json:"description"`
	Title       string `json:"title"`
}

type SearchGroup struct {
	SearchTerm string        `json:"search_term"`
	Jobs       []*JobPosting `json:"jobs"`
}

type JobCategoryGroup struct {
	Category string        `json:"category"`
	Searches []SearchGroup `json:"searches"`
}

type JobCategory struct {
	Category    string   `json:"category"`
	SearchTerms []string `json:"search_terms"`
}

func getJobCategories() []JobCategory {
	return []JobCategory{
		{
			Category:    "Data Science",
			SearchTerms: []string{"data scientist", "data science"},
		},
		{
			Category:    "Security",
			SearchTerms: []string{"security engineer", "security analyst"},
		},
	}
}

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <output_file> [sqlite_db_file]\n", os.Args[0])
		os.Exit(1)
	}
	dataDir := os.Args[1]
	var sqliteFile string
	if len(os.Args) == 3 {
		sqliteFile = os.Args[2]
	}

	httpClient := &http.Client{}
	accessToken := os.Getenv("LINKEDIN_TOKEN")

	limiter := rate.NewLimiter(10, 1)

	categories := getJobCategories()
	var jobGroups []JobCategoryGroup
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Initialize jobGroups with categories and empty search groups
	for _, cat := range categories {
		jobGroup := JobCategoryGroup{
			Category: cat.Category,
			Searches: make([]SearchGroup, 0, len(cat.SearchTerms)),
		}
		mu.Lock()
		jobGroups = append(jobGroups, jobGroup)
		mu.Unlock()
	}

	// Process all categories and search terms concurrently
	for _, cat := range categories {
		for _, searchTerm := range cat.SearchTerms {
			wg.Add(1)
			go func(category, searchTerm string) {
				defer wg.Done()

				limiter.Wait(context.TODO())
				log.Printf("Fetching job listings for category %s, search: %s\n", category, searchTerm)

				listings := jobListings(httpClient, limiter, accessToken, searchTerm)
				searchGroup := SearchGroup{
					SearchTerm: searchTerm,
					Jobs:       make([]*JobPosting, 0),
				}

				var searchWg sync.WaitGroup
				var searchMu sync.Mutex

				for jid := range listings {
					searchWg.Add(1)
					go func(jid JobID) {
						defer searchWg.Done()

						limiter.Wait(context.TODO())
						log.Printf("Fetching data for job %s (category: %s, search: %s)\n", jid, category, searchTerm)

						job, err := jobPostings(httpClient, limiter, jid, accessToken)
						if err != nil {
							log.Printf("could not get job posting for job %s: %v", jid, err)
							return
						}

						searchMu.Lock()
						searchGroup.Jobs = append(searchGroup.Jobs, job)
						searchMu.Unlock()
					}(jid)
				}

				searchWg.Wait()

				if len(searchGroup.Jobs) > 0 {
					mu.Lock()
					for i, jobGroup := range jobGroups {
						if jobGroup.Category == category {
							jobGroups[i].Searches = append(jobGroups[i].Searches, searchGroup)
							break
						}
					}
					mu.Unlock()
				}
			}(cat.Category, searchTerm)
		}
	}

	wg.Wait()

	if sqliteFile != "" {
		if err := saveJobsToSQLite(jobGroups, sqliteFile); err != nil {
			log.Fatal("could not save jobs to SQLite: %v", err)
		}
	} else {
		if err := saveJobsToFile(jobGroups, dataDir); err != nil {
			log.Fatal("could not save jobs to file: %v", err)
		}
	}
}

func saveJobsToFile(jobGroups []JobCategoryGroup, jobsFilePath string) error {
	dir := filepath.Dir(jobsFilePath)
	if _, err := os.Stat(dir); err != nil {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("could not create directory '%s': %v", dir, err)
		}
	}

	f, err := os.OpenFile(jobsFilePath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("could not open file '%s': %v", jobsFilePath, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(jobGroups); err != nil {
		return fmt.Errorf("could not encode jobs to json: %v", err)
	}

	return nil
}

func saveJobsToSQLite(jobGroups []JobCategoryGroup, sqliteFile string) error {
	db, err := sql.Open("sqlite", sqliteFile)
	if err != nil {
		return fmt.Errorf("could not open SQLite database '%s': %v", sqliteFile, err)
	}
	defer db.Close()

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("could not enable foreign keys: %v", err)
	}

	// Create tables
	createTables := []string{
		`CREATE TABLE IF NOT EXISTS jobs (
			job_id TEXT PRIMARY KEY,
			company TEXT NOT NULL,
			description TEXT NOT NULL,
			title TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS categories (
			category_id INTEGER PRIMARY KEY AUTOINCREMENT,
			category_name TEXT NOT NULL UNIQUE
		)`,
		`CREATE TABLE IF NOT EXISTS jobs_categories (
			job_id TEXT,
			category_id INTEGER,
			PRIMARY KEY (job_id, category_id),
			FOREIGN KEY (job_id) REFERENCES jobs(job_id),
			FOREIGN KEY (category_id) REFERENCES categories(category_id)
		)`,
		`CREATE TABLE IF NOT EXISTS searches (
			search_id INTEGER PRIMARY KEY AUTOINCREMENT,
			search_term TEXT NOT NULL UNIQUE
		)`,
		`CREATE TABLE IF NOT EXISTS searches_jobs (
			search_id INTEGER,
			job_id TEXT,
			first_seen TEXT NOT NULL,
			last_seen TEXT NOT NULL,
			PRIMARY KEY (search_id, job_id),
			FOREIGN KEY (search_id) REFERENCES searches(search_id),
			FOREIGN KEY (job_id) REFERENCES jobs(job_id)
		)`,
	}

	for _, query := range createTables {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("could not create table: %v", err)
		}
	}

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("could not begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Get execution timestamp
	timestamp := time.Now().Format(time.RFC3339)

	// Insert data
	for _, jobGroup := range jobGroups {
		// Insert or get category
		var categoryID int64
		err = tx.QueryRow(`
			INSERT INTO categories (category_name) VALUES (?)
			ON CONFLICT(category_name) DO UPDATE SET category_name=category_name
			RETURNING category_id`, jobGroup.Category).Scan(&categoryID)
		if err != nil {
			return fmt.Errorf("could not insert/get category '%s': %v", jobGroup.Category, err)
		}

		for _, searchGroup := range jobGroup.Searches {
			// Insert or get search term
			var searchID int64
			err = tx.QueryRow(`
				INSERT INTO searches (search_term) VALUES (?)
				ON CONFLICT(search_term) DO UPDATE SET search_term=search_term
				RETURNING search_id`, searchGroup.SearchTerm).Scan(&searchID)
			if err != nil {
				return fmt.Errorf("could not insert/get search term '%s': %v", searchGroup.SearchTerm, err)
			}

			for _, job := range searchGroup.Jobs {
				// Insert job if not exists
				_, err = tx.Exec(`
					INSERT OR IGNORE INTO jobs (job_id, company, description, title)
					VALUES (?, ?, ?, ?)`,
					job.JobID, job.Company, job.Description, job.Title)
				if err != nil {
					return fmt.Errorf("could not insert job '%s': %v", job.JobID, err)
				}

				// Insert job-category relationship
				_, err = tx.Exec(`
					INSERT OR IGNORE INTO jobs_categories (job_id, category_id)
					VALUES (?, ?)`, job.JobID, categoryID)
				if err != nil {
					return fmt.Errorf("could not insert job-category relationship for job '%s' and category '%d': %v", job.JobID, categoryID, err)
				}

				// Insert or update search-job relationship
				_, err = tx.Exec(`
					INSERT INTO searches_jobs (search_id, job_id, first_seen, last_seen)
					VALUES (?, ?, ?, ?)
					ON CONFLICT(search_id, job_id) DO UPDATE SET last_seen = ?`,
					searchID, job.JobID, timestamp, timestamp, timestamp)
				if err != nil {
					return fmt.Errorf("could not insert/update search-job relationship for search '%d' and job '%s': %v", searchID, job.JobID, err)
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("could not commit transaction: %v", err)
	}

	return nil
}
