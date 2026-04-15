package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
)

// Server serves the heat map and jobs data as JSON.
type Server struct {
	conn *sql.DB
	mux  *http.ServeMux
}

func NewServer(conn *sql.DB) *Server {
	s := &Server{conn: conn, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/heatmap", s.handleHeatmap)
	s.mux.HandleFunc("GET /api/company/{company}", s.handleCompany)
	s.mux.HandleFunc("GET /api/jobs", s.handleJobs)
	s.mux.HandleFunc("GET /api/interviews", s.handleInterviews)
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	return s
}

func (s *Server) Start(addr string) error {
	log.Printf("[api] listening on %s", addr)
	return http.ListenAndServe(addr, s.mux)
}

// --- Heat map ---

type HeatmapEntry struct {
	Company      string `json:"company"`
	AppsOpen     int    `json:"apps_open"`
	OASent       int    `json:"oa_sent"`
	Interviewing int    `json:"interviewing"`
	Offering     int    `json:"offering"`
	Rejecting    int    `json:"rejecting"`
	Total        int    `json:"total"`
	Stage        string `json:"stage"`
	NewPostings  int    `json:"new_postings"`
}

func (s *Server) handleHeatmap(w http.ResponseWriter, r *http.Request) {
	days := 7
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}

	rows, err := s.conn.Query(`
		SELECT
			ci.company,
			COUNT(*) FILTER (WHERE ci.event_type = 'apps_open') AS apps_open,
			COUNT(*) FILTER (WHERE ci.event_type = 'oa_sent') AS oa_sent,
			COUNT(*) FILTER (WHERE ci.event_type = 'interviewing') AS interviewing,
			COUNT(*) FILTER (WHERE ci.event_type = 'offering') AS offering,
			COUNT(*) FILTER (WHERE ci.event_type = 'rejecting') AS rejecting,
			COUNT(*) AS total,
			COALESCE(cs.current_stage, 'unknown') AS stage
		FROM company_intel ci
		LEFT JOIN company_status cs ON LOWER(ci.company) = LOWER(cs.company)
		WHERE ci.created_at > NOW() - INTERVAL '1 day' * $1
		GROUP BY ci.company, cs.current_stage
		ORDER BY total DESC`, days)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var entries []HeatmapEntry
	for rows.Next() {
		var e HeatmapEntry
		if err := rows.Scan(&e.Company, &e.AppsOpen, &e.OASent, &e.Interviewing, &e.Offering, &e.Rejecting, &e.Total, &e.Stage); err != nil {
			continue
		}

		// Count new job postings for this company in the same window
		s.conn.QueryRow(`
			SELECT COUNT(*) FROM jobs
			WHERE LOWER(company) = LOWER($1) AND discovered_at > NOW() - INTERVAL '1 day' * $2`,
			e.Company, days,
		).Scan(&e.NewPostings)

		entries = append(entries, e)
	}

	writeJSON(w, entries)
}

// --- Company detail ---

type CompanyDetail struct {
	Company  string         `json:"company"`
	Status   *CompanyStatus `json:"status"`
	Signals  []SignalEntry  `json:"recent_signals"`
	Postings []JobEntry     `json:"recent_postings"`
}

type CompanyStatus struct {
	Stage       string `json:"stage"`
	SignalCount int    `json:"signal_count"`
	Summary     string `json:"summary"`
}

type SignalEntry struct {
	EventType   string `json:"event_type"`
	Content     string `json:"content"`
	DiscordUser string `json:"discord_user"`
	Channel     string `json:"channel"`
	CreatedAt   string `json:"created_at"`
}

type JobEntry struct {
	Title        string `json:"title"`
	Location     string `json:"location"`
	URL          string `json:"url"`
	Source       string `json:"source"`
	DiscoveredAt string `json:"discovered_at"`
}

func (s *Server) handleCompany(w http.ResponseWriter, r *http.Request) {
	company := r.PathValue("company")
	if company == "" {
		http.Error(w, "company required", 400)
		return
	}

	detail := CompanyDetail{Company: company}

	// Get status
	var cs CompanyStatus
	err := s.conn.QueryRow(`
		SELECT current_stage, signal_count, summary FROM company_status WHERE LOWER(company) = LOWER($1)`,
		company,
	).Scan(&cs.Stage, &cs.SignalCount, &cs.Summary)
	if err == nil {
		detail.Status = &cs
	}

	// Recent signals
	sigRows, err := s.conn.Query(`
		SELECT event_type, content, discord_user, channel, created_at::text
		FROM company_intel WHERE LOWER(company) = LOWER($1)
		ORDER BY created_at DESC LIMIT 50`, company)
	if err == nil {
		defer sigRows.Close()
		for sigRows.Next() {
			var se SignalEntry
			if sigRows.Scan(&se.EventType, &se.Content, &se.DiscordUser, &se.Channel, &se.CreatedAt) == nil {
				detail.Signals = append(detail.Signals, se)
			}
		}
	}

	// Recent postings
	jobRows, err := s.conn.Query(`
		SELECT title, location, url, source, discovered_at::text
		FROM jobs WHERE LOWER(company) = LOWER($1)
		ORDER BY discovered_at DESC LIMIT 20`, company)
	if err == nil {
		defer jobRows.Close()
		for jobRows.Next() {
			var je JobEntry
			if jobRows.Scan(&je.Title, &je.Location, &je.URL, &je.Source, &je.DiscoveredAt) == nil {
				detail.Postings = append(detail.Postings, je)
			}
		}
	}

	writeJSON(w, detail)
}

// --- Jobs list ---

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	company := r.URL.Query().Get("company")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	query := `SELECT id, source, title, company, location, url, status, discovered_at::text FROM jobs WHERE 1=1`
	args := []any{}
	argN := 1

	if source != "" {
		query += ` AND source = $` + strconv.Itoa(argN)
		args = append(args, source)
		argN++
	}
	if company != "" {
		query += ` AND LOWER(company) = LOWER($` + strconv.Itoa(argN) + `)`
		args = append(args, company)
		argN++
	}

	query += ` ORDER BY discovered_at DESC LIMIT $` + strconv.Itoa(argN)
	args = append(args, limit)

	rows, err := s.conn.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type job struct {
		ID           int64  `json:"id"`
		Source       string `json:"source"`
		Title        string `json:"title"`
		Company      string `json:"company"`
		Location     string `json:"location"`
		URL          string `json:"url"`
		Status       string `json:"status"`
		DiscoveredAt string `json:"discovered_at"`
	}

	var jobs []job
	for rows.Next() {
		var j job
		if rows.Scan(&j.ID, &j.Source, &j.Title, &j.Company, &j.Location, &j.URL, &j.Status, &j.DiscoveredAt) == nil {
			jobs = append(jobs, j)
		}
	}

	writeJSON(w, jobs)
}

// --- Interview reports ---

func (s *Server) handleInterviews(w http.ResponseWriter, r *http.Request) {
	company := r.URL.Query().Get("company")
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}

	query := `
		SELECT ir.id, ir.company, ir.role, ir.stage, ir.questions, ir.difficulty,
		       ir.outcome, ir.timeline, ir.location, ir.source, ir.source_url,
		       ir.author, ir.content, ir.matched_jobs, ir.created_at::text
		FROM interview_reports ir
		WHERE ir.created_at > NOW() - INTERVAL '1 day' * $1`
	args := []any{days}

	if company != "" {
		query += ` AND LOWER(ir.company) = LOWER($2)`
		args = append(args, company)
	}
	query += ` ORDER BY ir.created_at DESC LIMIT 100`

	rows, err := s.conn.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type report struct {
		ID          int64           `json:"id"`
		Company     string          `json:"company"`
		Role        string          `json:"role"`
		Stage       string          `json:"stage"`
		Questions   string          `json:"questions"`
		Difficulty  string          `json:"difficulty"`
		Outcome     string          `json:"outcome"`
		Timeline    string          `json:"timeline"`
		Location    string          `json:"location"`
		Source      string          `json:"source"`
		SourceURL   string          `json:"source_url"`
		Author      string          `json:"author"`
		Content     string          `json:"content"`
		MatchedJobs json.RawMessage `json:"matched_jobs"`
		CreatedAt   string          `json:"created_at"`
	}

	var reports []report
	for rows.Next() {
		var rpt report
		if rows.Scan(&rpt.ID, &rpt.Company, &rpt.Role, &rpt.Stage, &rpt.Questions, &rpt.Difficulty, &rpt.Outcome, &rpt.Timeline, &rpt.Location, &rpt.Source, &rpt.SourceURL, &rpt.Author, &rpt.Content, &rpt.MatchedJobs, &rpt.CreatedAt) == nil {
			reports = append(reports, rpt)
		}
	}

	writeJSON(w, reports)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(data)
}
