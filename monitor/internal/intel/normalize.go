package intel

import "strings"

// companyAliases maps lowercase variants to canonical company names.
// Every key must be lowercase. The canonical name is the value.
var companyAliases = map[string]string{
	// ---- FAANG / Big Tech ----
	"amazon":              "Amazon",
	"aws":                 "Amazon",
	"amzn":                "Amazon",
	"amazon web services": "Amazon",
	"meta":                "Meta",
	"facebook":            "Meta",
	"fb":                  "Meta",
	"google":              "Google",
	"goog":                "Google",
	"googl":               "Google",
	"alphabet":            "Google",
	"deepmind":            "DeepMind",
	"waymo":               "Waymo",
	"apple":               "Apple",
	"aapl":                "Apple",
	"microsoft":           "Microsoft",
	"msft":                "Microsoft",
	"netflix":             "Netflix",
	"nflx":                "Netflix",
	"nvidia":              "NVIDIA",
	"nvda":                "NVIDIA",

	// ---- Other Major Tech ----
	"uber":       "Uber",
	"lyft":       "Lyft",
	"airbnb":     "Airbnb",
	"stripe":     "Stripe",
	"coinbase":   "Coinbase",
	"palantir":   "Palantir",
	"pltr":       "Palantir",
	"databricks": "Databricks",
	"bloomberg":  "Bloomberg",
	"bbg":        "Bloomberg",
	"salesforce": "Salesforce",
	"crm":        "Salesforce",
	"adobe":      "Adobe",
	"adbe":       "Adobe",
	"oracle":     "Oracle",
	"orcl":       "Oracle",
	"intel":      "Intel",
	"intc":       "Intel",
	"snap":       "Snap",
	"snapchat":   "Snap",
	"pinterest":  "Pinterest",
	"pins":       "Pinterest",
	"spotify":    "Spotify",
	"spot":       "Spotify",
	"reddit":     "Reddit",
	"discord":    "Discord",
	"figma":      "Figma",
	"datadog":    "Datadog",
	"ddog":       "Datadog",
	"mongodb":    "MongoDB",
	"mdb":        "MongoDB",
	"robinhood":  "Robinhood",
	"hood":       "Robinhood",
	"plaid":      "Plaid",
	"openai":     "OpenAI",
	"anthropic":  "Anthropic",
	"tesla":      "Tesla",
	"tsla":       "Tesla",
	"spacex":     "SpaceX",
	"anduril":    "Anduril",
	"scale ai":   "Scale AI",
	"scaleai":    "Scale AI",
	"brex":       "Brex",
	"okta":       "Okta",
	"zscaler":    "Zscaler",
	"zs":         "Zscaler",
	"twilio":     "Twilio",
	"twlo":       "Twilio",
	"shopify":    "Shopify",
	"shop":       "Shopify",
	"doordash":   "DoorDash",
	"dd":         "DoorDash",
	"instacart":  "Instacart",
	"roblox":     "Roblox",
	"rblx":       "Roblox",
	"cloudflare": "Cloudflare",
	"cf":         "Cloudflare",
	"net":        "Cloudflare",

	// ---- Finance / Quant ----
	"goldman sachs":        "Goldman Sachs",
	"goldman":              "Goldman Sachs",
	"gs":                   "Goldman Sachs",
	"jane street":          "Jane Street",
	"janestreet":           "Jane Street",
	"jane st":              "Jane Street",
	"citadel":              "Citadel",
	"citadel securities":   "Citadel",
	"hudson river trading": "Hudson River Trading",
	"hrt":                  "Hudson River Trading",
	"d.e. shaw":            "D.E. Shaw",
	"de shaw":              "D.E. Shaw",
	"deshaw":               "D.E. Shaw",
	"two sigma":            "Two Sigma",
	"twosigma":             "Two Sigma",
	"2sigma":               "Two Sigma",
	"jump trading":         "Jump Trading",
	"jump":                 "Jump Trading",
	"tower research":       "Tower Research",
	"drw":                  "DRW",
	"imc":                  "IMC Trading",
	"imc trading":          "IMC Trading",
	"optiver":              "Optiver",
	"akuna":                "Akuna Capital",
	"akuna capital":        "Akuna Capital",
	"five rings":           "Five Rings",
	"five rings capital":   "Five Rings",
	"susquehanna":          "Susquehanna",
	"sig":                  "Susquehanna",
	"jpmorgan":             "JPMorgan",
	"jp morgan":            "JPMorgan",
	"jpm":                  "JPMorgan",
	"morgan stanley":       "Morgan Stanley",
	"ms":                   "Morgan Stanley",
	"bank of america":      "Bank of America",
	"bofa":                 "Bank of America",
	"bac":                  "Bank of America",
	"citi":                 "Citi",
	"citibank":             "Citi",
	"citigroup":            "Citi",
	"barclays":             "Barclays",

	// ---- Unicorns / Growth ----
	"tiktok":              "TikTok",
	"bytedance":           "ByteDance",
	"byte dance":          "ByteDance",
	"linkedin":            "LinkedIn",
	"lnkd":                "LinkedIn",
	"twitter":             "X",
	"x":                   "X",
	"x corp":              "X",
	"walmart":             "Walmart",
	"wmt":                 "Walmart",
	"walmart labs":        "Walmart",
	"walmart global tech": "Walmart",
	"visa":                "Visa",
	"v":                   "Visa",
	"mastercard":          "Mastercard",
	"ma":                  "Mastercard",
	"paypal":              "PayPal",
	"pypl":                "PayPal",
	"block":               "Block",
	"square":              "Block",
	"sq":                  "Block",
	"zillow":              "Zillow",
	"z":                   "Zillow",
	"dropbox":             "Dropbox",
	"dbx":                 "Dropbox",
	"atlassian":           "Atlassian",
	"team":                "Atlassian",
	"vmware":              "VMware",
	"vmw":                 "VMware",
	"servicenow":          "ServiceNow",
	"now":                 "ServiceNow",
	"snowflake":           "Snowflake",
	"snow":                "Snowflake",
	"splunk":              "Splunk",
	"elastic":             "Elastic",
	"estc":                "Elastic",
	"confluent":           "Confluent",
	"cflt":                "Confluent",
	"hashicorp":           "HashiCorp",
	"hcp":                 "HashiCorp",
	"palo alto":           "Palo Alto Networks",
	"palo alto networks":  "Palo Alto Networks",
	"panw":                "Palo Alto Networks",
	"crowdstrike":         "CrowdStrike",
	"crwd":                "CrowdStrike",
	"fortinet":            "Fortinet",
	"ftnt":                "Fortinet",

	// ---- Startups / AI ----
	"databricks ai": "Databricks",
	"hugging face":  "Hugging Face",
	"huggingface":   "Hugging Face",
	"hf":            "Hugging Face",
	"cohere":        "Cohere",
	"mistral":       "Mistral",
	"mistral ai":    "Mistral",
	"midjourney":    "Midjourney",
	"stability":     "Stability AI",
	"stability ai":  "Stability AI",
	"inflection":    "Inflection AI",
	"inflection ai": "Inflection AI",
	"perplexity":    "Perplexity",
	"perplexity ai": "Perplexity",
	"cursor":        "Cursor",
	"anysphere":     "Cursor",
	"vercel":        "Vercel",
	"supabase":      "Supabase",
	"notion":        "Notion",
	"airtable":      "Airtable",
	"retool":        "Retool",
	"linear":        "Linear",
}

// NormalizeCompany maps company name variants to a canonical form.
// Case-insensitive. Returns the canonical name if a mapping exists,
// otherwise returns the input with leading/trailing whitespace trimmed
// and title-cased.
func NormalizeCompany(name string) string {
	cleaned := strings.TrimSpace(name)
	if cleaned == "" {
		return ""
	}

	lower := strings.ToLower(cleaned)

	// Collapse multiple spaces for multi-word lookups
	lower = collapseSpaces(lower)

	if canonical, ok := companyAliases[lower]; ok {
		return canonical
	}

	// No alias found, title case the original
	return toTitleCase(cleaned)
}

// collapseSpaces replaces runs of whitespace with a single space.
func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
		} else {
			b.WriteRune(r)
			inSpace = false
		}
	}
	return b.String()
}

// toTitleCase uppercases the first letter of each word.
// This avoids the deprecated strings.Title.
func toTitleCase(s string) string {
	lower := strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(lower))
	capitalizeNext := true
	for _, r := range lower {
		if r == ' ' || r == '\t' || r == '-' {
			b.WriteRune(r)
			capitalizeNext = true
		} else if capitalizeNext {
			b.WriteRune(rune(strings.ToUpper(string(r))[0]))
			capitalizeNext = false
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
