# Companies needing custom scrapers

The monitor covers Greenhouse and Lever boards generically, add a slug and it
works. The companies below run their own career portals, so each needs a
dedicated scraper to pull internship and co-op listings.

Listed alphabetically. There are 55 of them, which is rather the point:
the platform trick does not work here, and each one is its own piece of work.

| Company | Careers site |
| --- | --- |
| Adobe | https://www.adobe.com/careers.html |
| Amazon | https://www.amazon.jobs/ |
| AMD | https://www.amd.com/en/corporate/careers |
| Apple | https://jobs.apple.com/ |
| Atlassian | https://www.atlassian.com/company/careers |
| Bloomberg | https://www.bloomberg.com/careers/ |
| Canva | https://www.canva.com/careers/ |
| Cisco | https://jobs.cisco.com/ |
| Citadel Securities | https://www.citadelsecurities.com/careers/ |
| Confluent | https://www.confluent.io/careers/ |
| CrowdStrike | https://www.crowdstrike.com/careers/ |
| Cruise | https://www.getcruise.com/careers/ |
| D.E. Shaw | https://www.deshaw.com/careers |
| GitHub | https://github.com/about/careers |
| Goldman Sachs | https://www.goldmansachs.com/careers/ |
| Google | https://www.google.com/about/careers/ |
| Grammarly | https://www.grammarly.com/careers |
| HashiCorp | https://www.hashicorp.com/careers |
| Hudson River Trading | https://www.hudsonrivertrading.com/careers/ |
| IBM | https://www.ibm.com/careers/ |
| Intel | https://jobs.intel.com/ |
| Intuit | https://www.intuit.com/careers/ |
| JPMorgan Chase | https://careers.jpmorgan.com/ |
| Mastercard | https://www.mastercard.us/en-us/vision/who-we-are/careers.html |
| Meta | https://www.metacareers.com/ |
| Microsoft | https://careers.microsoft.com/ |
| Netflix | https://jobs.netflix.com/ |
| Niantic | https://nianticlabs.com/careers/ |
| Notion | https://www.notion.so/careers |
| NVIDIA | https://www.nvidia.com/en-us/about-nvidia/careers/ |
| OpenAI | https://openai.com/careers/ |
| Oracle | https://www.oracle.com/careers/ |
| Palo Alto Networks | https://jobs.paloaltonetworks.com/ |
| PayPal | https://www.paypal.com/us/webapps/mpp/jobs |
| Qualcomm | https://www.qualcomm.com/company/careers |
| Retool | https://retool.com/careers |
| Rippling | https://www.rippling.com/careers |
| Salesforce | https://www.salesforce.com/company/careers/ |
| Samsung (US) | https://www.samsung.com/us/careers/ |
| SAP | https://jobs.sap.com/ |
| SentinelOne | https://www.sentinelone.com/careers/ |
| ServiceNow | https://www.servicenow.com/careers.html |
| Shopify | https://www.shopify.com/careers |
| Snap | https://snap.com/en-US/jobs |
| Snowflake | https://careers.snowflake.com/ |
| Sony (US) | https://www.playstation.com/en-us/corporate/playstation-careers/ |
| Splunk | https://www.splunk.com/en_us/careers.html |
| Supabase | https://supabase.com/careers |
| Tesla | https://www.tesla.com/careers/ |
| Two Sigma | https://www.twosigma.com/careers/ |
| Uber | https://www.uber.com/us/en/careers/ |
| Visa | https://usa.visa.com/careers.html |
| VMware / Broadcom | https://www.broadcom.com/company/careers |
| Workday | https://www.workday.com/en-us/company/careers.html |
| Zillow | https://www.zillow.com/careers/ |

## Adding one

A scraper is any type satisfying the interface in `monitor/cmd/monitor/main.go`:

```go
type scraper interface {
	Source() string
	Run(ctx context.Context, proxyURL string) error
}
```

`internal/scrapers/lever` is the shortest one to copy: fetch the listings,
filter to the internship keywords, and insert each through `db.InsertJob`,
which deduplicates on `(source, external_id)` and reports whether the row was
new. Register it in the `scrapers` slice in `main.go` and it joins the cycle.
