# Fetch Package Guide

## Scope

- The shared outbound HTTP boundary for ingestion, official-page enrichment,
  and strict anonymous public discovery.
- Owns transport, redirects, robots policy, SSRF defense, rate limits, body
  decoding, conditional requests, and crawl-budget accounting.

## Where To Look

- `fetch.go`, `transport.go`, `options.go` — fetch lifecycle and configuration.
- `ssrf.go`, `production_hosts.go` — destination-IP and reviewed-host policy.
- `robots.go`, `strict_robots.go`, `limiter.go` — robots and per-host pacing.
- `budget.go`, `public_policy.go`, `document_body.go` — strict public-crawl
  attempt/body/MIME limits.
- `link_headers.go`, `body.go`, `errors.go` — bounded response parsing and typed
  failures.

## Invariants

- Every redirect and connection target is revalidated. `WithAnyPublicHost`
  removes only the hostname allowlist; it never permits private, loopback,
  link-local, metadata, multicast, or credential-bearing targets.
- Strict public crawl fails closed when robots policy is unavailable or
  malformed. Robots, retries, redirects, link headers, compressed expansion,
  and document reads all spend the caller-owned shared budget.
- Public defaults stay bounded: 512 KiB per document, one retry, two redirects,
  64 transport attempts, and 6 MiB aggregate body bytes.
- Rate and `Crawl-delay` gates are per host. A slow host must not throttle an
  unrelated host.
- HTTP/static parsing is primary. A CDP fallback must remain explicitly wired,
  bounded, and unreachable for ordinary static pages.
- `WithAllowLoopback` is test-only and still must not admit metadata or
  link-local addresses.
- Legacy TLS exceptions are exact reviewed hosts; never weaken the global TLS
  transport to accommodate one origin.

## Verification

- Full package: `go test ./internal/fetch -count=1`.
- Security boundary: `go test ./internal/fetch -run 'SSRF|Redirect|Public|Robots' -count=1`.
- Budget accounting: `go test ./internal/fetch -run 'Budget|Body|Gzip|Header' -count=1`.
- Transport/rate changes: `go test -race ./internal/fetch -count=1`.
- A new production source host requires both focused fetch tests and the
  caller-side wiring test; an allowlist entry alone is not integration proof.
