package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/RightNxw/job-monitor/monitor/internal/api"
	"github.com/RightNxw/job-monitor/monitor/internal/config"
	"github.com/RightNxw/job-monitor/monitor/internal/db"
	discordbot "github.com/RightNxw/job-monitor/monitor/internal/discord"
	"github.com/RightNxw/job-monitor/monitor/internal/intel"
	"github.com/RightNxw/job-monitor/monitor/internal/notify"
	"github.com/RightNxw/job-monitor/monitor/internal/proxy"
	"github.com/RightNxw/job-monitor/monitor/internal/scrapers/ghrepo"
	"github.com/RightNxw/job-monitor/monitor/internal/scrapers/glassdoor"
	"github.com/RightNxw/job-monitor/monitor/internal/scrapers/greenhouse"
	"github.com/RightNxw/job-monitor/monitor/internal/scrapers/instagram"
	"github.com/RightNxw/job-monitor/monitor/internal/scrapers/lever"
	"github.com/RightNxw/job-monitor/monitor/internal/scrapers/reddit"
	"github.com/joho/godotenv"
)

// scraper is the common interface for all job scrapers.
type scraper interface {
	Source() string
	Run(ctx context.Context, proxyURL string) error
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	godotenv.Load() // load .env if present

	cfg := config.Load()

	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required (e.g. postgresql://user:pass@host:5432/dbname?sslmode=require)")
	}

	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer database.Close()

	pool := proxy.NewPool(cfg.ProxiesFile)
	discord := notify.NewDiscord(cfg.DiscordWebhookURL)

	// --- Intel system (Discord channel scraper + Haiku parser) ---
	var discordIntelScraper scraper
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	discordToken := os.Getenv("DISCORD_USER_TOKEN")
	discordChannelsCfg := os.Getenv("DISCORD_CHANNEL_IDS") // comma-separated id:name pairs

	if discordToken != "" && anthropicKey != "" {
		if err := intel.Migrate(database.Conn()); err != nil {
			log.Fatalf("failed to migrate intel tables: %v", err)
		}
		if err := intel.MigrateInterviews(database.Conn()); err != nil {
			log.Fatalf("failed to migrate interview tables: %v", err)
		}

		store := intel.NewStore(database.Conn())
		interviewStore := intel.NewInterviewStore(database.Conn())
		parser := intel.NewParser(anthropicKey)

		var channels []discordbot.Channel
		if discordChannelsCfg != "" {
			for _, entry := range strings.Split(discordChannelsCfg, ",") {
				parts := strings.SplitN(strings.TrimSpace(entry), ":", 2)
				name := parts[0]
				if len(parts) == 2 {
					name = parts[1]
				}
				channels = append(channels, discordbot.Channel{ID: parts[0], Name: name})
			}
		}

		// discordScraper added to scrapers list below
		discordIntelScraper = discordbot.NewScraper(discordToken, channels, store, interviewStore, parser)

		// Run aggregator every hour in background
		aggregator := intel.NewAggregator(store, database.Conn())
		go func() {
			t := time.NewTicker(1 * time.Hour)
			defer t.Stop()
			for range t.C {
				aggregator.Run(7)
			}
		}()
	} else {
		if discordToken == "" {
			log.Println("[discord] skipped - no DISCORD_USER_TOKEN set")
		}
		if anthropicKey == "" {
			log.Println("[discord] skipped - no ANTHROPIC_API_KEY set")
		}
	}

	// --- Job scrapers ---
	keywords := []string{"internship", "co-op", "coop", "co op"}

	var scrapers []scraper

	// Greenhouse boards
	for _, board := range cfg.GreenhouseBoards {
		scrapers = append(scrapers, greenhouse.New(
			board, boardDisplayName(board), database, discord, keywords,
		))
	}

	// Lever boards
	leverBoards := []lever.Board{
		{Slug: "palantir", Company: "Palantir", BaseURL: "https://www.palantir.com/api/lever/v1/postings"},
		{Slug: "spotify", Company: "Spotify", BaseURL: "https://api.lever.co/v0/postings/spotify", Public: true},
		{Slug: "shieldai", Company: "Shield AI", BaseURL: "https://api.lever.co/v0/postings/shieldai", Public: true},
		{Slug: "plaid", Company: "Plaid", BaseURL: "https://api.lever.co/v0/postings/plaid", Public: true},
	}
	for _, board := range leverBoards {
		scrapers = append(scrapers, lever.New(board, database, discord, keywords))
	}

	// GitHub repo monitors (internships only)
	ghRepos := []ghrepo.Repo{
		{
			Slug:   "simplify-intern",
			Name:   "Simplify Internships",
			RawURL: "https://raw.githubusercontent.com/SimplifyJobs/Summer2026-Internships/dev/.github/scripts/listings.json",
		},
	}
	for _, repo := range ghRepos {
		scrapers = append(scrapers, ghrepo.New(repo, database, discord))
	}

	// Instagram story monitors (INSTAGRAM_COOKIES = JSON cookie array from burner account)
	// OCR every story with Tesseract, classify with Haiku, extract links from API
	igCookies := os.Getenv("INSTAGRAM_COOKIES")
	var igParser *intel.Parser
	var igStore *intel.Store
	if anthropicKey != "" {
		igParser = intel.NewParser(anthropicKey)
		igStore = intel.NewStore(database.Conn())
	}
	scrapers = append(scrapers, instagram.New("zero2sudo", igCookies, database, discord, igParser, igStore))

	// Discord intel scraper (if configured)
	if discordIntelScraper != nil {
		scrapers = append(scrapers, discordIntelScraper)
	}

	// Reddit intel scraper (needs ANTHROPIC_API_KEY for Haiku parsing)
	if anthropicKey != "" {
		if err := intel.Migrate(database.Conn()); err != nil {
			log.Printf("[reddit] failed to migrate intel tables: %v", err)
		} else {
			redditStore := intel.NewStore(database.Conn())
			redditParser := intel.NewParser(anthropicKey)
			redditInterviews := intel.NewInterviewStore(database.Conn())
			redditScraper := reddit.NewScraper([]reddit.Subreddit{
				{Name: "csMajors"},
				{Name: "cscareerquestions"},
				{Name: "csinterviews"},
				{Name: "leetcode"},
			}, redditStore, redditInterviews, redditParser, pool)
			scrapers = append(scrapers, redditScraper)
		}
	}

	// Glassdoor interview question scraper
	if err := intel.Migrate(database.Conn()); err != nil {
		log.Printf("[glassdoor] failed to migrate intel tables: %v", err)
	} else {
		gdStore := intel.NewStore(database.Conn())
		scrapers = append(scrapers, glassdoor.NewScraper(glassdoor.DefaultCompanies, gdStore))
	}

	// Start API server in background
	apiPort := os.Getenv("API_PORT")
	if apiPort == "" {
		apiPort = "3000"
	}
	apiServer := api.NewServer(database.Conn())
	go func() {
		if err := apiServer.Start(":" + apiPort); err != nil {
			log.Printf("[api] server error: %v", err)
		}
	}()

	log.Printf("monitor started: %d scrapers, interval=%s, proxies=%d, api=:%s",
		len(scrapers), cfg.Interval, pool.Count(), apiPort)

	// Run once immediately
	runAll(scrapers, pool)

	// Schedule recurring runs
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	for {
		select {
		case <-ticker.C:
			runAll(scrapers, pool)
		case <-ctx.Done():
			log.Println("shutting down")
			return
		}
	}
}

func runAll(scrapers []scraper, pool *proxy.Pool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for _, s := range scrapers {
		proxyURL := pool.Get()
		if err := s.Run(ctx, proxyURL); err != nil {
			log.Printf("[%s] error: %v", s.Source(), err)
		}
	}
}

func boardDisplayName(board string) string {
	names := map[string]string{
		"janestreet":        "Jane Street",
		"anthropic":         "Anthropic",
		"stripe":            "Stripe",
		"databricks":        "Databricks",
		"airbnb":            "Airbnb",
		"lyft":              "Lyft",
		"coinbase":          "Coinbase",
		"roblox":            "Roblox",
		"spacex":            "SpaceX",
		"linkedin":          "LinkedIn",
		"pinterest":         "Pinterest",
		"reddit":            "Reddit",
		"discord":           "Discord",
		"figma":             "Figma",
		"datadog":           "Datadog",
		"cloudflare":        "Cloudflare",
		"mongodb":           "MongoDB",
		"block":             "Block",
		"robinhood":         "Robinhood",
		"twilio":            "Twilio",
		"okta":              "Okta",
		"zscaler":           "Zscaler",
		"elastic":           "Elastic",
		"doordashusa":       "DoorDash",
		"instacart":         "Instacart",
		"brex":              "Brex",
		"scaleai":           "Scale AI",
		"andurilindustries": "Anduril",
		"flexport":          "Flexport",
		"gusto":             "Gusto",
		"toast":             "Toast",
		"aurorainnovation":  "Aurora",
		"waymo":             "Waymo",
		"nuro":              "Nuro",
		"appliedintuition":  "Applied Intuition",
		"verkada":           "Verkada",
		"airtable":          "Airtable",
		"asana":             "Asana",
		"duolingo":          "Duolingo",
		"epicgames":         "Epic Games",
		"riotgames":         "Riot Games",
		"unity3d":           "Unity",
		"twitch":            "Twitch",
		"gitlab":            "GitLab",
		"vercel":            "Vercel",
	}
	if name, ok := names[board]; ok {
		return name
	}
	return board
}
