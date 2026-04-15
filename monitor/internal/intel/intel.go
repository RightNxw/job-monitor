package intel

import (
	"database/sql"
	"fmt"
	"time"
)

// Migrate creates the intel tables.
func Migrate(conn *sql.DB) error {
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS company_intel (
			id             BIGSERIAL PRIMARY KEY,
			company        TEXT NOT NULL,
			event_type     TEXT NOT NULL,
			content        TEXT NOT NULL,
			parsed_data    JSONB NOT NULL DEFAULT '{}',
			role           TEXT NOT NULL DEFAULT '',
			team           TEXT NOT NULL DEFAULT '',
			location       TEXT NOT NULL DEFAULT '',
			questions      TEXT NOT NULL DEFAULT '',
			round          TEXT NOT NULL DEFAULT '',
			timeline       TEXT NOT NULL DEFAULT '',
			discord_user   TEXT NOT NULL DEFAULT '',
			discord_msg_id TEXT NOT NULL DEFAULT '',
			channel        TEXT NOT NULL DEFAULT '',
			created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(discord_msg_id)
		);

		CREATE INDEX IF NOT EXISTS idx_intel_company ON company_intel(company);
		CREATE INDEX IF NOT EXISTS idx_intel_event ON company_intel(event_type);
		CREATE INDEX IF NOT EXISTS idx_intel_created ON company_intel(created_at);

		CREATE TABLE IF NOT EXISTS company_status (
			company        TEXT PRIMARY KEY,
			current_stage  TEXT NOT NULL DEFAULT 'unknown',
			signal_count   INT NOT NULL DEFAULT 0,
			summary        TEXT NOT NULL DEFAULT '',
			last_updated   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	return err
}

// Store represents the intel database operations.
type Store struct {
	conn *sql.DB
}

func NewStore(conn *sql.DB) *Store {
	return &Store{conn: conn}
}

// Event types
const (
	EventAppsOpen     = "apps_open"
	EventOASent       = "oa_sent"
	EventInterviewing = "interviewing"
	EventOffering     = "offering"
	EventRejecting    = "rejecting"
	EventClosed       = "closed"
	EventLCQuestion   = "lc_question"
	EventGeneral      = "general"
)

// Signal is a parsed hiring signal from a Discord/Reddit message.
type Signal struct {
	Company      string
	EventType    string
	Content      string
	ParsedData   string // JSON
	Role         string // "SDE Intern", "SWE New Grad"
	Team         string // "AWS", "Ads"
	Location     string // "NYC", "Seattle"
	Questions    string // "2 LC mediums, graph + DP"
	Round        string // "OA", "phone screen", "final round"
	Timeline     string // "applied 2 weeks ago"
	DiscordUser  string
	DiscordMsgID string
	Channel      string
}

// InsertSignal stores a parsed hiring signal. Returns true if new.
// The company name is normalized before insertion to prevent duplicates.
func (s *Store) InsertSignal(sig *Signal) (bool, error) {
	sig.Company = NormalizeCompany(sig.Company)

	var id int64
	err := s.conn.QueryRow(`
		INSERT INTO company_intel (company, event_type, content, parsed_data, role, team, location, questions, round, timeline, discord_user, discord_msg_id, channel)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (discord_msg_id) DO NOTHING
		RETURNING id`,
		sig.Company, sig.EventType, sig.Content, sig.ParsedData,
		sig.Role, sig.Team, sig.Location, sig.Questions, sig.Round, sig.Timeline,
		sig.DiscordUser, sig.DiscordMsgID, sig.Channel,
	).Scan(&id)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CompanyStatus is the aggregated hiring pipeline status for a company.
type CompanyStatus struct {
	Company      string
	CurrentStage string
	SignalCount  int
	Summary      string
	LastUpdated  time.Time
}

// GetCompanyStatus returns the current status for a company.
func (s *Store) GetCompanyStatus(company string) (*CompanyStatus, error) {
	var cs CompanyStatus
	err := s.conn.QueryRow(`
		SELECT company, current_stage, signal_count, summary, last_updated
		FROM company_status WHERE company = $1`, company,
	).Scan(&cs.Company, &cs.CurrentStage, &cs.SignalCount, &cs.Summary, &cs.LastUpdated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &cs, err
}

// SignalCounts returns event counts for a company in the last N days.
type SignalCounts struct {
	AppsOpen     int
	OASent       int
	Interviewing int
	Offering     int
	Rejecting    int
	Closed       int
	Total        int
}

func (s *Store) GetSignalCounts(company string, days int) (*SignalCounts, error) {
	rows, err := s.conn.Query(`
		SELECT event_type, COUNT(*)
		FROM company_intel
		WHERE company = $1 AND created_at > NOW() - INTERVAL '1 day' * $2
		GROUP BY event_type`,
		company, days,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sc := &SignalCounts{}
	for rows.Next() {
		var eventType string
		var count int
		if err := rows.Scan(&eventType, &count); err != nil {
			return nil, err
		}
		sc.Total += count
		switch eventType {
		case EventAppsOpen:
			sc.AppsOpen = count
		case EventOASent:
			sc.OASent = count
		case EventInterviewing:
			sc.Interviewing = count
		case EventOffering:
			sc.Offering = count
		case EventRejecting:
			sc.Rejecting = count
		case EventClosed:
			sc.Closed = count
		}
	}
	return sc, rows.Err()
}

// UpdateCompanyStatus sets the aggregated status for a company.
func (s *Store) UpdateCompanyStatus(company, stage string, signalCount int, summary string) error {
	_, err := s.conn.Exec(`
		INSERT INTO company_status (company, current_stage, signal_count, summary, last_updated)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (company) DO UPDATE SET
			current_stage = EXCLUDED.current_stage,
			signal_count = EXCLUDED.signal_count,
			summary = EXCLUDED.summary,
			last_updated = NOW()`,
		company, stage, signalCount, summary,
	)
	return err
}

// DetermineStage picks the most likely pipeline stage from signal counts.
func DetermineStage(sc *SignalCounts) string {
	if sc.Total == 0 {
		return "unknown"
	}
	// Priority: most advanced stage with enough signals
	if sc.Closed >= 3 {
		return EventClosed
	}
	if sc.Offering >= 2 {
		return EventOffering
	}
	if sc.Rejecting >= 3 && sc.Offering == 0 {
		return EventRejecting
	}
	if sc.Interviewing >= 2 {
		return EventInterviewing
	}
	if sc.OASent >= 2 {
		return EventOASent
	}
	if sc.AppsOpen >= 2 {
		return EventAppsOpen
	}
	// Not enough signals for any stage, pick the one with most
	max, stage := 0, "unknown"
	for _, pair := range []struct {
		count int
		name  string
	}{
		{sc.Offering, EventOffering},
		{sc.Interviewing, EventInterviewing},
		{sc.OASent, EventOASent},
		{sc.Rejecting, EventRejecting},
		{sc.AppsOpen, EventAppsOpen},
	} {
		if pair.count > max {
			max = pair.count
			stage = pair.name
		}
	}
	return stage
}

// FormatSummary creates a human-readable summary from signal counts.
func FormatSummary(sc *SignalCounts, days int) string {
	parts := []string{}
	if sc.AppsOpen > 0 {
		parts = append(parts, fmt.Sprintf("%d applied", sc.AppsOpen))
	}
	if sc.OASent > 0 {
		parts = append(parts, fmt.Sprintf("%d got OA", sc.OASent))
	}
	if sc.Interviewing > 0 {
		parts = append(parts, fmt.Sprintf("%d interviewing", sc.Interviewing))
	}
	if sc.Offering > 0 {
		parts = append(parts, fmt.Sprintf("%d offers", sc.Offering))
	}
	if sc.Rejecting > 0 {
		parts = append(parts, fmt.Sprintf("%d rejections", sc.Rejecting))
	}
	if len(parts) == 0 {
		return "no recent activity"
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return fmt.Sprintf("Last %d days: %s (%d total signals)", days, result, sc.Total)
}
