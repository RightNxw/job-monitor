package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	// Discord webhook URL for notifications
	DiscordWebhookURL string

	// Path to proxies.txt (one proxy per line)
	ProxiesFile string

	// Postgres connection string (Supabase or any Postgres)
	DatabaseURL string

	// Scrape interval
	Interval time.Duration

	// Greenhouse board slugs to monitor
	GreenhouseBoards []string
}

func Load() *Config {
	interval := 15 * time.Minute
	if v := os.Getenv("MONITOR_INTERVAL_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			interval = time.Duration(n) * time.Minute
		}
	}

	databaseURL := os.Getenv("DATABASE_URL")

	proxiesFile := os.Getenv("MONITOR_PROXIES_FILE")
	if proxiesFile == "" {
		proxiesFile = "proxies.txt"
	}

	boards := []string{
		// Tier 1
		"janestreet",
		// Tier 2
		"anthropic", "stripe", "databricks", "airbnb", "lyft", "coinbase", "roblox", "spacex",
		// Tier 3
		"linkedin", "pinterest", "reddit", "discord", "figma", "datadog", "cloudflare", "mongodb",
		// Tier 4
		"block", "robinhood", "twilio", "okta", "zscaler", "elastic",
		// Tier 5
		"doordashusa", "instacart", "brex", "scaleai", "andurilindustries", "flexport", "gusto", "toast", "aurorainnovation",
		// Tier 6
		"waymo", "nuro", "appliedintuition", "verkada", "airtable", "asana", "duolingo",
		"epicgames", "riotgames", "unity3d", "twitch", "gitlab", "vercel",
	}

	return &Config{
		DiscordWebhookURL: os.Getenv("DISCORD_WEBHOOK_URL"),
		ProxiesFile:       proxiesFile,
		DatabaseURL:       databaseURL,
		Interval:          interval,
		GreenhouseBoards:  boards,
	}
}
