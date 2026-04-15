# VSAT Job Monitor

A Go service that watches job boards for internship postings, plus a Next.js
dashboard to look at what it finds. Everything gets saved to Postgres.

The job board scrapers are the core of it, and the reason the whole thing works
is that most companies don't build their own job board. They pay for an
applicant tracking system instead, and a small handful of those cover a big
chunk of the industry. Greenhouse and Lever are the two you hit most often.

That matters because every company on Greenhouse serves its listings from the
same endpoint in the same shape, and the only thing that changes is the board
slug in the URL. So you don't write a scraper per company, you write one
scraper per platform. Solve Greenhouse once and you have Stripe, Figma, Jane
Street, Anthropic and forty others, and adding the next one is a single line
with its slug in it. Same story for Lever. That is how this covers 49 companies
with two scrapers, and it is most of the trick. The full list is in
[docs/covered-companies.md](docs/covered-companies.md).

That trick only goes so far though, because plenty of companies still run their
own careers site, and it tends to be the ones you actually want. Google, Apple,
Amazon, Meta, Netflix, Microsoft, most of the big trading firms. Every one of
them built their own portal, every one is laid out differently, and every one
needs a scraper written specifically for it. There is no slug you can add.

I counted 55 companies worth doing that for and got to none of them. The list
is in [docs/custom-scrapers.md](docs/custom-scrapers.md) if you want somewhere
to start. That's the real gap in this project: two scrapers cover 49 companies,
and the next 55 want 55 scrapers.

The rest of the project is that same idea pointed at places where people talk.
Job boards tell you a posting exists. Discord and Reddit tell you a company
started sending OAs three days ago. I wanted both in one place.

## Status

I made this a while back and haven't run it in a long time, so treat it as a
snapshot and not something I'm maintaining. I'm putting it up because this
year's cycle started earlier than I thought it would, and it felt more useful
public than sitting in a private repo. I still think it was a cool project.

A good chunk is stubbed out. If you read the code you'll figure out why.
There's anti-bot stuff involved and I'm not shipping that part. The interfaces
are all there and I marked the gaps, so you can figure it out :)

Technically a lot is still in progress, so here is what's actually done. The
job board side works and I trusted it: Greenhouse, Lever, the GitHub feed, the
dedup and Postgres schema under them, and the Discord webhook that pings you
when something new shows up. The Haiku parsing works too, and so does the part
that rolls those signals into a hiring stage per company.

The rest I never really finished. The Discord and Reddit scrapers run, but they
were the last things I built and I didn't give them the time I gave the job
boards, so expect rough edges. I barely touched the dashboard. It was just a
quick way to look at the data and it never got past that. That's why it looks
like it does.

To be specific about Discord and Reddit, since those are the two I'd warn you
about. The core of both works: pull new posts, hand them to Haiku, save what
comes back. What never got done is the state around that. Neither one remembers
where it got to between restarts, so the position lives in memory and resets
every time you start it. Both treat a rate limit response like the source was
just quiet, so you can't tell throttling from a slow day. Reddit also writes
interview reports into a table that only gets created when the Discord path
runs, so running Reddit without a Discord token means those writes fail. A few
database errors get swallowed without logging too. Call it maybe 70 percent
done: the reading and parsing is there, the bookkeeping around it isn't.

I never finished the heat map. I just liked the idea and built enough of it to
see what it would look like, and that is about where it stopped. It's the
Pipeline page now. The thought was that you open one screen and see which
companies are actually moving right now, who is sending OAs, who is
interviewing, who has gone quiet, all in one view instead of piecing it together
from a hundred posts. The aggregator fills it in, but nothing ever clears out
old entries, so a company keeps looking busy long after it went quiet, and the
endpoint behind it runs a query per company inside another query, which will
lock up the database if more than a couple of people hit it. So take it as the
idea, not the feature.

Back when I ran it, it worked on the sources below. No promises it still does.
Sites change, and some of them really don't want to be scraped.

## What it collects

| Source | What it pulls |
| --- | --- |
| **Greenhouse** | Internship postings from 45 board slugs (Jane Street, Anthropic, Stripe, Figma, …) |
| **Lever** | Postings from Palantir, Spotify, Shield AI, Plaid |
| **GitHub** | The SimplifyJobs `Summer2026-Internships` listings feed |
| **Discord** | Messages from intern hiring channels, turned into hiring events |
| **Reddit** | `r/csMajors`, `r/cscareerquestions`, `r/csinterviews`, `r/leetcode` |
| **zero2sudo monitor** | Instagram stories from the zero2sudo account, OCR'd and sorted |
| **Glassdoor** | Interview questions per company (needs a solver, see below) |

It only keeps internship and co-op roles.

## How it works

```
scrapers ──> Claude Haiku ──> Postgres ──> Next.js dashboard
                                  └──────> Discord webhook (new postings)
```

The board scrapers are simple. Pull the JSON, skip anything already seen
(deduped on source plus external id), save the rest.

The interesting part is the intel pipeline in `monitor/internal/intel`. Posts
from Discord and Reddit are just text, like "just got the OA for Amazon SDE
intern, 2 LC mediums". Each one goes to Claude Haiku, which pulls out the
company, the role, what happened (`apps_open`, `oa_sent`, `interviewing`,
`offering`, `rejecting`) and any interview questions in there. Company names
get normalized so "amzn", "Amazon.com" and "AWS" all end up as one company.
Then an aggregator looks at the last 7 days and works out where each company is
in its hiring. That's what the Pipeline page shows.

Instagram stories are pictures, so they go through Tesseract OCR first. OCR on
screenshots is bad a lot of the time, so if it comes back with fewer than five
real words I drop it instead of wasting a model call. If it still looks like
junk, Haiku looks at the image itself.

## Layout

```
monitor/          Go service: scrapers, intel pipeline, JSON API
  cmd/monitor/    entrypoint, wires up the scrapers and runs them on a ticker
  internal/
    scrapers/     one package per source
    intel/        Haiku parsing, OCR, name normalizing, aggregation
    cloudflare/   challenge solver (stubbed, see below)
    engine/       headless JS runtime for the solver (stubbed)
    db/           Postgres schema and migrations
    api/          JSON endpoints
web/              Next.js 16 dashboard (App Router, Tailwind v4)
docs/             notes and roadmap
```

## Running it

You need Go 1.25+, Node 20+, a Postgres database, and `tesseract` installed if
you want the zero2sudo monitor.

```sh
# monitor
cd monitor
cp .env.example .env        # DATABASE_URL is the only one you have to fill in
go run ./cmd/monitor

# dashboard, in another shell
cd web
cp .env.example .env.local  # Supabase URL and publishable key
npm install
npm run dev
```

Tables get created on the first run. Each scraper works on its own: no
`ANTHROPIC_API_KEY` means no intel pipeline, no `DISCORD_USER_TOKEN` means no
Discord, and the job boards keep going either way. It logs what it skipped and
why when it starts.

It also serves JSON on `API_PORT` (3000 by default) at `/api/heatmap`,
`/api/jobs`, `/api/company/{company}`, `/api/interviews` and `/api/health`. The
dashboard doesn't use these, it reads Supabase directly, but they're there if
you want to script something.

### Proxies

Optional. Point `MONITOR_PROXIES_FILE` at a text file with one proxy per line,
either `host:port:user:pass` or a full `http://` or `socks5://` URL. Blank
lines and `#` comments get skipped. Without it everything goes out on your own
IP, which is fine at the default 15 minute interval.

## The Cloudflare solver

Glassdoor sits behind Cloudflare, so you have to get past the challenge before
you can read anything. That solver is not in here.

Every file in `internal/cloudflare` and `internal/engine` is behind a `solver`
build tag, so a normal build gets the stub in
[`internal/cloudflare/stub.go`](monitor/internal/cloudflare/stub.go) instead.
The Glassdoor scraper just logs a skip and moves on. Nothing else cares.

If you want Glassdoor, bring your own. Write `SolveWithRetry` against whatever
you use, a paid solving service, a headless browser, your own thing. It has to
hand back the clearance cookies and the user agent that got them. That user
agent has to match on every request after, or the cookies are worthless.

I left the real implementation in the tree so you can read it, but it's
commented out and won't build as it is. Getting it running takes a patched
build of [`tommie/v8go`](https://github.com/tommie/v8go) and some setup that
isn't in here.

One more thing on HTTP. The scrapers use
[`sardanioss/httpcloak`](https://github.com/sardanioss/httpcloak) for TLS
fingerprinting, built against the public version. If you'd rather not depend on
it, [`bogdanfinn/tls-client`](https://github.com/bogdanfinn/tls-client) does the
same job and more people use it. Try it and see if you still get through.
Swapping means rewriting `internal/httpclient`.

## Notes

This was all built for research and for my own learning. I wanted to see if I
could put the pieces together, and that was most of the point. It is published
in that spirit, not as something to go point at other people's servers.

If you run any of it, that is on you. Respect the sites you are reading from and
respect their terms of service. Follow the rules they set out, honour their
robots.txt and their rate limits, and do not collect or republish anyone's
personal information. Keep it to research and learning, and do not do anything
malicious with it.

The Discord scraper logs in with a real user account token. That's against
Discord's ToS and it can get the account banned. Same deal with the Instagram
cookies. Both are off unless you set the env vars.

So be careful with it. The zero2sudo monitor uses a real Instagram session
token, and both sites ban accounts for this, which is a good reason to leave
those two sources switched off. If you run this, that's on you and not me. I'm
not responsible for banned accounts or anything else that happens.

Best of luck with your applications.

## License

MIT, see [LICENSE](LICENSE).
