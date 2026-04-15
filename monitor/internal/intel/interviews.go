package intel

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

// MigrateInterviews creates the interview_reports table.
func MigrateInterviews(conn *sql.DB) error {
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS interview_reports (
			id             BIGSERIAL PRIMARY KEY,
			company        TEXT NOT NULL,
			role           TEXT NOT NULL DEFAULT '',
			stage          TEXT NOT NULL DEFAULT '',
			questions      TEXT NOT NULL DEFAULT '',
			difficulty     TEXT NOT NULL DEFAULT '',
			outcome        TEXT NOT NULL DEFAULT '',
			timeline       TEXT NOT NULL DEFAULT '',
			location       TEXT NOT NULL DEFAULT '',
			source         TEXT NOT NULL DEFAULT '',
			source_url     TEXT NOT NULL DEFAULT '',
			source_id      TEXT NOT NULL DEFAULT '',
			author         TEXT NOT NULL DEFAULT '',
			content        TEXT NOT NULL DEFAULT '',
			parsed_data    JSONB NOT NULL DEFAULT '{}',
			matched_jobs   JSONB NOT NULL DEFAULT '[]',
			created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(source_id)
		);

		CREATE INDEX IF NOT EXISTS idx_interview_company ON interview_reports(company);
		CREATE INDEX IF NOT EXISTS idx_interview_stage ON interview_reports(stage);
		CREATE INDEX IF NOT EXISTS idx_interview_created ON interview_reports(created_at);
	`)
	return err
}

// InterviewReport is a user-reported interview experience.
type InterviewReport struct {
	Company     string
	Role        string // "SDE Intern", "SWE New Grad"
	Stage       string // "oa", "phone_screen", "onsite", "final_round"
	Questions   string // "2 LC mediums - graph BFS + DP"
	Difficulty  string // "easy", "medium", "hard"
	Outcome     string // "passed", "rejected", "pending", "ghosted"
	Timeline    string // "applied 2 weeks ago, OA last week"
	Location    string
	Source      string // "reddit:csMajors", "discord:intern_process"
	SourceURL   string
	SourceID    string // unique dedup key
	Author      string
	Content     string // raw message text
	ParsedData  string // full Haiku JSON
	MatchedJobs []int64
}

// InterviewStore handles interview report storage and job matching.
type InterviewStore struct {
	conn *sql.DB
}

func NewInterviewStore(conn *sql.DB) *InterviewStore {
	return &InterviewStore{conn: conn}
}

// Insert stores an interview report and matches it to jobs. Returns true if new.
func (s *InterviewStore) Insert(r *InterviewReport) (bool, error) {
	r.Company = NormalizeCompany(r.Company)
	r.MatchedJobs = s.findMatchingJobs(r.Company, r.Role)

	matchedJSON, _ := json.Marshal(r.MatchedJobs)
	if r.MatchedJobs == nil {
		matchedJSON = []byte("[]")
	}

	var id int64
	err := s.conn.QueryRow(`
		INSERT INTO interview_reports (company, role, stage, questions, difficulty, outcome, timeline, location, source, source_url, source_id, author, content, parsed_data, matched_jobs)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)
		ON CONFLICT (source_id) DO NOTHING
		RETURNING id`,
		r.Company, r.Role, r.Stage, r.Questions, r.Difficulty, r.Outcome,
		r.Timeline, r.Location, r.Source, r.SourceURL, r.SourceID,
		r.Author, r.Content, r.ParsedData, string(matchedJSON),
	).Scan(&id)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if len(r.MatchedJobs) > 0 {
		log.Printf("[interview] %s %s matched %d jobs", r.Company, r.Stage, len(r.MatchedJobs))
	}

	return true, nil
}

// findMatchingJobs finds jobs in the jobs table that match this company + role.
func (s *InterviewStore) findMatchingJobs(company, role string) []int64 {
	query := `SELECT id FROM jobs WHERE LOWER(company) = LOWER($1)`
	args := []any{company}

	if role != "" {
		query += ` AND LOWER(title) LIKE '%' || LOWER($2) || '%'`
		args = append(args, role)
	}

	query += ` ORDER BY discovered_at DESC LIMIT 10`

	rows, err := s.conn.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// RecentReports returns interview reports for a company in the last N days.
func (s *InterviewStore) RecentReports(company string, days int) ([]InterviewReport, error) {
	rows, err := s.conn.Query(`
		SELECT company, role, stage, questions, difficulty, outcome, timeline, location, source, source_url, author, content, created_at
		FROM interview_reports
		WHERE LOWER(company) = LOWER($1) AND created_at > NOW() - INTERVAL '1 day' * $2
		ORDER BY created_at DESC`, company, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []InterviewReport
	for rows.Next() {
		var r InterviewReport
		var createdAt time.Time
		if err := rows.Scan(&r.Company, &r.Role, &r.Stage, &r.Questions, &r.Difficulty, &r.Outcome, &r.Timeline, &r.Location, &r.Source, &r.SourceURL, &r.Author, &r.Content, &createdAt); err != nil {
			continue
		}
		reports = append(reports, r)
	}
	return reports, nil
}
