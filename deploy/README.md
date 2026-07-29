# Deploy: events.nukk.net

Reverse-proxied Go service on the shared DigitalOcean VPS `developer-vps`
(`152.42.210.34`), mirroring the cold/holiday/calendar.nukk.net pattern.

| Item | Value |
|---|---|
| Host | `events.nukk.net` (Cloudflare-proxied, zone `nukk.net`) |
| VPS | `developer-vps` (`root@developer-vps-01`) |
| Service dir | `/srv/developer/events-intel/` |
| Binary | `eventsintel` (linux/amd64, CGo-free static) |
| Headless runtime | Google Chrome (non-Snap CDP runtime), started lazily once per ingest batch |
| DB | `/srv/developer/events-intel/data/events.db` (SQLite WAL) |
| API bind | `127.0.0.1:3005` (Caddy reverse-proxies) |
| Caddy | append `deploy/events.nukk.net.caddy` to `/etc/caddy/Caddyfile` |

## Artifacts

- `eventsintel-api.service` — systemd unit, runs `eventsintel serve` on `127.0.0.1:3005`.
- `eventsintel-ingest.service` + `.timer` — 24h ingest batch (flock single-flight, 30/min polite rate).
- `events.nukk.net.caddy` — Caddy site block.

## Headless DOM fallback runtime

The daily ingest can render a page only after the existing robots-aware HTTP
fetch succeeds with `200`. It uses one lazy Chromium process for the batch,
opens a tab only for a JS shell, limits the batch to 30 rendered pages, and
cancels each page after 15 seconds. The conservative sequential upper bound is
7m30s, leaving time inside the ingest unit's 20-minute deadline for normal
HTTP work.

Install the non-Snap Chrome runtime and prepare the unprivileged service
account before deploying the binary. Ubuntu's `chromium` package is a Snap
wrapper and cannot launch from this system-level ingest unit because Snap
rejects that cgroup.

```sh
sudo apt-get update
curl -fsSLo /tmp/google-chrome-stable_current_amd64.deb https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb
sudo apt-get install -y /tmp/google-chrome-stable_current_amd64.deb
id -u eventsintel >/dev/null 2>&1 || sudo useradd --system --home /srv/developer/events-intel --shell /usr/sbin/nologin eventsintel
sudo install -d -o eventsintel -g eventsintel /srv/developer/events-intel/data
sudo install -d -o eventsintel -g eventsintel /srv/developer/events-intel/snap
sudo install -d -o eventsintel -g eventsintel /var/lib/eventsintel
sudo chown -R eventsintel:eventsintel /srv/developer/events-intel/data
google-chrome --headless --disable-gpu --version
free -h
```

The allocator prefers `/usr/bin/google-chrome` when present, so it cannot select
the Ubuntu Snap wrapper. The browser runs as `eventsintel`, with Chrome's sandbox enabled and GPU
disabled. Its systemd `StateDirectory` supplies the writable
`/var/lib/eventsintel` Chrome home; the deploy directory stays root-owned. It
has no direct Internet path: the already-approved document is
served through a per-page loopback origin, while same-origin browser resources
are obtained only through the existing SSRF-, allowlist-, robots-, and
rate-limited fetcher. Do not add a separate renderer service or a local-to-VPS
database writer: the VPS SQLite database remains the source of truth. If
Chromium cannot start or a page times out, ingest continues from the already
fetched static body.

Managed challenges and explicit bot blocks are not bypassed. A non-200 static
fetch never reaches Chromium; a source blocked this way stays unavailable to
automation and may be represented only through the reviewed benchmark catalog
with a human-confirmed source note.

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
7. For this renderer change, also run one manual ingest and inspect the journal:

   ```sh
   sudo systemctl start eventsintel-ingest.service
   sudo journalctl -u eventsintel-ingest.service -n 200 --no-pager
   ```

   Confirm the run completes inside 20 minutes, then run `make eval-report` on
   the VPS and retain the `wrong_type` result with the deploy evidence.

   Before the first restart, copy both updated unit files, run `systemctl
   daemon-reload`, and verify `systemctl show -p User -p Group
   eventsintel-ingest.service` reports `eventsintel`. The existing database
   directory must remain owned by that account.

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
