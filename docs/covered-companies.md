# Companies this actually watches

49 companies, covered by two scrapers, because they all post through either
Greenhouse or Lever. Adding another company on the same platform is a single
line.

The ones that run their own careers site are not in here and each need their
own scraper. That list is in [custom-scrapers.md](custom-scrapers.md).

## What it pulls

Internship and co-op postings only. Every listing is checked against the
keywords `internship`, `co-op`, `coop` and `co op` before it is stored, so
full-time and senior roles never make it in.

For each posting it keeps the title, company, location, the apply URL, when it
was posted and when it was first seen. Postings are deduplicated on the source
plus the platform's own listing id, so the same job is never stored twice and
you only get notified the first time it appears.

## Greenhouse (45)

Slugs live in `boards` in `monitor/internal/config/config.go`, display names in
`boardDisplayName` in `monitor/cmd/monitor/main.go`.

Airbnb, Airtable, Anduril, Anthropic, Applied Intuition, Asana, Aurora, Block,
Brex, Cloudflare, Coinbase, Databricks, Datadog, Discord, DoorDash, Duolingo,
Elastic, Epic Games, Figma, Flexport, GitLab, Gusto, Instacart, Jane Street,
LinkedIn, Lyft, MongoDB, Nuro, Okta, Pinterest, Reddit, Riot Games, Robinhood,
Roblox, Scale AI, SpaceX, Stripe, Toast, Twilio, Twitch, Unity, Vercel,
Verkada, Waymo, Zscaler

## Lever (4)

Defined in `leverBoards` in `monitor/cmd/monitor/main.go`. Lever is less
uniform than Greenhouse: Spotify, Shield AI and Plaid come off the public
`api.lever.co` endpoint, while Palantir proxies it through their own domain, so
that one carries its own base URL.

Palantir, Plaid, Shield AI, Spotify

## Adding one

If the company posts through Greenhouse, take the slug out of their careers URL
(`job-boards.greenhouse.io/<slug>`), add it to the `boards` list in
`config.go`, and add a display name to the map in `main.go`. That is the whole
change.

Lever works the same way through `leverBoards`, plus a base URL if they proxy
it.
