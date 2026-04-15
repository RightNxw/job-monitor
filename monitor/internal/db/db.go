package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

type DB struct {
	conn *sql.DB
}

type Job struct {
	ID           int64
	Source       string // "greenhouse:spacex", "lever:palantir", "github:simplify-intern"
	ExternalID   string // unique ID from the source
	Title        string
	Company      string
	Location     string
	URL          string
	PostedAt     time.Time
	DiscoveredAt time.Time
	Metadata     string // JSON blob for extra fields
	Status       string // AI-determined: "hiring", "interviewing", "closed", "unknown"
	Questions    string // LeetCode company-tagged questions (scraped, JSON array)
}

// Open connects to a Postgres database (Supabase or any Postgres).
// connStr is a Postgres connection string, e.g.:
//
//	"postgresql://user:pass@host:5432/dbname?sslmode=require"
func Open(connStr string) (*DB, error) {
	conn, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	conn.SetMaxOpenConns(5)
	conn.SetMaxIdleConns(2)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	return &DB{conn: conn}, nil
}

func migrate(conn *sql.DB) error {
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS jobs (
			id            BIGSERIAL PRIMARY KEY,
			source        TEXT NOT NULL,
			external_id   TEXT NOT NULL,
			title         TEXT NOT NULL,
			company       TEXT NOT NULL,
			location      TEXT NOT NULL DEFAULT '',
			url           TEXT NOT NULL DEFAULT '',
			posted_at     TIMESTAMPTZ,
			discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			metadata      JSONB NOT NULL DEFAULT '{}',
			status        TEXT NOT NULL DEFAULT 'new',
			questions     TEXT NOT NULL DEFAULT '',
			UNIQUE(source, external_id)
		);

		CREATE INDEX IF NOT EXISTS idx_jobs_source ON jobs(source);
		CREATE INDEX IF NOT EXISTS idx_jobs_company ON jobs(company);
		CREATE INDEX IF NOT EXISTS idx_jobs_discovered ON jobs(discovered_at);
		CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
	`)
	// Add columns if migrating from older schema
	if err == nil {
		conn.Exec(`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'new'`)
		conn.Exec(`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS questions TEXT NOT NULL DEFAULT ''`)
	}
	return err
}

// InsertJob inserts a job if it doesn't already exist. Returns true if it was new.
func (db *DB) InsertJob(j *Job) (bool, error) {
	var id int64
	err := db.conn.QueryRow(`
		INSERT INTO jobs (source, external_id, title, company, location, url, posted_at, discovered_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (source, external_id) DO NOTHING
		RETURNING id`,
		j.Source, j.ExternalID, j.Title, j.Company, j.Location, j.URL, j.PostedAt, j.DiscoveredAt, j.Metadata,
	).Scan(&id)

	if err == sql.ErrNoRows {
		return false, nil // already existed
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// JobExists checks if a job with this source+external_id already exists.
func (db *DB) JobExists(source, externalID string) (bool, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM jobs WHERE source = $1 AND external_id = $2`, source, externalID).Scan(&count)
	return count > 0, err
}

// RecentJobs returns jobs discovered in the last N hours for a given source.
func (db *DB) RecentJobs(source string, hours int) ([]Job, error) {
	rows, err := db.conn.Query(`
		SELECT id, source, external_id, title, company, location, url, posted_at, discovered_at, metadata, status, questions
		FROM jobs WHERE source = $1 AND discovered_at > NOW() - INTERVAL '1 hour' * $2
		ORDER BY discovered_at DESC`,
		source, hours,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.Source, &j.ExternalID, &j.Title, &j.Company, &j.Location, &j.URL, &j.PostedAt, &j.DiscoveredAt, &j.Metadata, &j.Status, &j.Questions); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// UpdateStatus sets the application status for a job.
func (db *DB) UpdateStatus(id int64, status string) error {
	_, err := db.conn.Exec(`UPDATE jobs SET status = $1 WHERE id = $2`, status, id)
	return err
}

// UpdateQuestions sets the interview questions/notes for a job.
func (db *DB) UpdateQuestions(id int64, questions string) error {
	_, err := db.conn.Exec(`UPDATE jobs SET questions = $1 WHERE id = $2`, questions, id)
	return err
}

// Conn returns the underlying *sql.DB for use by other packages.
func (db *DB) Conn() *sql.DB {
	return db.conn
}

func (db *DB) Close() error {
	return db.conn.Close()
}
