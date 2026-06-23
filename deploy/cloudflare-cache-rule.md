# Cloudflare edge caching for events.nukk.net

The Go service already advertises a cache policy on every read success
(`Cache-Control: public, max-age=120, s-maxage=3600, stale-while-revalidate=86400`,
see `internal/api/cache.go`). But Cloudflare does **not** cache HTML or
extensionless/`/api/...` paths by default — it reports `cf-cache-status: DYNAMIC`
and round-trips to the DigitalOcean origin on every page open. Static assets
(`/assets/*`) cache automatically by extension; everything else needs an explicit
**Cache Rule** to be edge-cached.

This doc is the SSOT for that rule plus the ingest-time purge. Nothing here is
applied automatically — run the script in dry-run first, then with `--apply`.

## What the rule does

Cache (eligible + respect origin TTL) for the cacheable read surface:

- `/` (the HTML UI and the JSON service index)
- `/api/v1/*` (events list/detail/sources/changes, meta, schema, openapi.yaml)
- `/llms.txt`

…**except** requests that negotiate Markdown via the `Accept` header. On the
Cloudflare Free plan `Vary: Accept` is ignored, so a single URL keyed cache could
serve a cached JSON body to an `Accept: text/markdown` client (or vice-versa).
Bypassing cache when `Accept` contains `text/markdown` removes that collision; the
`?format=md` query is a distinct cache key and is unaffected. Browser/agent JSON
traffic — the overwhelming majority — is cached.

Errors (4xx/5xx/429) carry **no** `Cache-Control` (the `writeError` path omits it),
so Cloudflare won't cache them even under "respect origin TTL".

### Ruleset Engine expression

```
(http.host eq "events.nukk.net"
 and not any(http.request.headers["accept"][*] contains "text/markdown")
 and (http.request.uri.path eq "/"
      or starts_with(http.request.uri.path, "/api/v1/")
      or http.request.uri.path eq "/llms.txt"))
```

Action: **Set cache settings** → cache eligibility `eligible for cache`,
Edge TTL `Respect origin` (honours `s-maxage=3600`), Browser TTL `Respect origin`.

## Token / permissions

The existing token at `~/.config/cloudflare/nukk-net-token` has **Zone → DNS edit**
only. Cache Rules and purge need additional scopes:

| Operation | Required token permission |
|---|---|
| Create/replace the cache rule | Zone → **Cache Rules** → Edit |
| `purge_everything` after ingest | Zone → **Cache Purge** → Purge |

If the existing token lacks these, mint a new token scoped to zone `nukk.net` with
those two permissions. The purge token goes into the ingest unit env
(`EVENTSINTEL_CF_PURGE_TOKEN`), never into the repo.

## Apply

```sh
# dry-run (default): prints zone id, the rule JSON, and the target endpoint only
CF_API_TOKEN=$(cat ~/.config/cloudflare/nukk-net-token) ./deploy/apply-cache-rule.sh

# apply for real
CF_API_TOKEN=<cache-rules-token> ./deploy/apply-cache-rule.sh --apply
```

## Verify

```sh
# Two consecutive requests; the second should be HIT.
curl -sD- -o /dev/null https://events.nukk.net/api/v1/events | grep -i cf-cache-status
curl -sD- -o /dev/null https://events.nukk.net/api/v1/events | grep -i cf-cache-status   # expect HIT

# Markdown-by-header must NOT be served a cached JSON (expect DYNAMIC/BYPASS):
curl -sD- -o /dev/null -H 'Accept: text/markdown' https://events.nukk.net/api/v1/events | grep -i cf-cache-status
```

## Purge (ingest-time + deploy-time)

- **Ingest**: `eventsintel ingest` purges `purge_everything` automatically when a
  run stored events, IF `EVENTSINTEL_CF_PURGE_ZONE` + `EVENTSINTEL_CF_PURGE_TOKEN`
  are set on the ingest unit (no-op otherwise). See `internal/cfpurge`.
- **Deploy**: after deploying a binary that changes the HTML or bumps asset `?v=`
  versions, purge once manually so the edge drops the old HTML:

  ```sh
  curl -s -X POST "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/purge_cache" \
    -H "Authorization: Bearer $CF_PURGE_TOKEN" -H "Content-Type: application/json" \
    --data '{"purge_everything":true}'
  ```
