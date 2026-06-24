# Deploy: events.nukk.net

Reverse-proxied Go service on the shared DigitalOcean VPS `developer-vps`
(`152.42.210.34`), mirroring the cold/holiday/calendar.nukk.net pattern.

| Item | Value |
|---|---|
| Host | `events.nukk.net` (Cloudflare-proxied, zone `nukk.net`) |
| VPS | `developer-vps` (`root@developer-vps-01`) |
| Service dir | `/srv/developer/events-intel/` |
| Binary | `eventsintel` (linux/amd64, CGo-free static) |
| DB | `/srv/developer/events-intel/data/events.db` (SQLite WAL) |
| API bind | `127.0.0.1:3005` (Caddy reverse-proxies) |
| Caddy | append `deploy/events.nukk.net.caddy` to `/etc/caddy/Caddyfile` |

## Artifacts

- `eventsintel-api.service` — systemd unit, runs `eventsintel serve` on `127.0.0.1:3005`.
- `eventsintel-ingest.service` + `.timer` — 24h ingest batch (flock single-flight, 30/min polite rate).
- `events.nukk.net.caddy` — Caddy site block.

## DNS (Cloudflare)

Token: `~/.config/cloudflare/nukk-net-token` (zone DNS edit). Create:
`A events.nukk.net → 152.42.210.34`. Per the TLS ordering: create **DNS-only**
(`proxied:false`) first so Caddy can obtain the LE cert, verify origin HTTPS 200,
then flip `proxied:true`.

## Deploy steps (executed 2026-06-21)

1. Cross-compile: `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o eventsintel-linux ./cmd/eventsintel`
2. `ssh developer-vps mkdir -p /srv/developer/events-intel/data`
3. `scp` binary + seeded `events.db` → service dir.
4. Install systemd units, `systemctl daemon-reload`, enable+start api service & ingest timer.
5. Create Cloudflare A record (DNS-only).
6. Append Caddy block → `caddy validate` → `systemctl reload caddy`.
7. Verify origin HTTPS 200 → flip Cloudflare `proxied:true`.
8. **Verify (MANDATORY — every deploy ends here):** run `deploy/verify.sh`. It is
   the gate that proves the live site serves the new build correctly, not just that
   a process is up. Do not consider a deploy done until it prints `ALL CHECKS
   PASSED`.

   ```sh
   deploy/verify.sh                         # the public edge (default)
   deploy/verify.sh http://127.0.0.1:3005   # origin only (run on the VPS), pre-edge
   ```

   It checks: every public path returns 200; the Accept-negotiated `/` serves the
   **HTML UI to browsers** and the **JSON service index to agents** (a stale binary
   404s `/`; a mis-cached edge serves the JSON variant to browsers — both caught
   here); `/api/v1/events` is non-empty; and the API surface is edge-cached. Any
   `FAIL` line means stop and fix before calling the deploy complete.

## Re-deploy (code update to an already-live service)

1. Cross-compile the linux binary (step 1 above).
2. `scp` it to `eventsintel.new`, back up the current binary (`cp -a eventsintel
   eventsintel.bak-$(date -u +%Y%m%dT%H%M%SZ)`), verify the uploaded sha256, then
   `mv eventsintel.new eventsintel`.
3. **If the change includes a schema migration** (new `internal/store/migrations/*`):
   back up `data/events.db` first, then run the migration BEFORE restarting the API
   (`serve` reads columns the new code expects) — `systemctl start
   eventsintel-ingest.service` applies migrations on writer startup and does the
   first crawl. The old API keeps serving meanwhile. Code-only changes skip this.
4. `systemctl restart eventsintel-api`.
5. If the change alters HTML / asset versions / cache policy, re-`--apply` the cache
   rule if its expression changed and purge once (see the cache-rule doc).
6. **Run `deploy/verify.sh`** (step 8 above) — non-negotiable.

## Edge caching (Cloudflare)

The service advertises `Cache-Control` on read responses, but Cloudflare needs an
explicit Cache Rule to edge-cache HTML/`/api/...` paths (otherwise `cf-cache-status:
DYNAMIC`, full origin round trip per page open). Setup, verification, and token
scopes are in `deploy/cloudflare-cache-rule.md`; apply with
`deploy/apply-cache-rule.sh` (dry-run by default, `--apply` to commit).

Purge: `eventsintel ingest` auto-purges `purge_everything` after a run that stored
events, **when** the ingest unit has `EVENTSINTEL_CF_PURGE_ZONE` +
`EVENTSINTEL_CF_PURGE_TOKEN` set (no-op otherwise — token needs Zone→Cache Purge).
Add them to `eventsintel-ingest.service` via `Environment=`/`EnvironmentFile=`.
After deploying a binary that changes the HTML or bumps asset `?v=` versions,
purge once manually (see the cache-rule doc) so the edge drops the stale HTML.

## Rollback

- `systemctl disable --now eventsintel-api eventsintel-ingest.timer`
- Remove the `events.nukk.net` block from `/etc/caddy/Caddyfile` → reload.
- Remove the Cloudflare Cache Rule (`http_request_cache_settings` entrypoint) if rolling back edge caching.
- Delete the Cloudflare A record.
- `git revert` is not applicable (no repo); keep the prior binary as `eventsintel.bak`.
