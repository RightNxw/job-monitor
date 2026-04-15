package intel

import (
	"database/sql"
	"log"
)

// Aggregator periodically computes company pipeline status from recent signals.
type Aggregator struct {
	store *Store
	conn  *sql.DB
}

func NewAggregator(store *Store, conn *sql.DB) *Aggregator {
	return &Aggregator{store: store, conn: conn}
}

// Run aggregates signals from the last N days and updates company_status.
func (a *Aggregator) Run(days int) {
	// Get all companies with recent signals
	rows, err := a.conn.Query(`
		SELECT DISTINCT company FROM company_intel
		WHERE created_at > NOW() - INTERVAL '1 day' * $1`, days)
	if err != nil {
		log.Printf("[aggregator] query error: %v", err)
		return
	}
	defer rows.Close()

	var companies []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			continue
		}
		companies = append(companies, c)
	}

	updated := 0
	for _, company := range companies {
		counts, err := a.store.GetSignalCounts(company, days)
		if err != nil {
			log.Printf("[aggregator] error getting counts for %s: %v", company, err)
			continue
		}

		stage := DetermineStage(counts)
		summary := FormatSummary(counts, days)

		if err := a.store.UpdateCompanyStatus(company, stage, counts.Total, summary); err != nil {
			log.Printf("[aggregator] error updating %s: %v", company, err)
			continue
		}
		updated++
	}

	log.Printf("[aggregator] updated %d company statuses from %d days of signals", updated, days)
}
