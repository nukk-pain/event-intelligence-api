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
8. Smoke test all public entry points (not just the API paths — a stale binary
   with no root handler will 404 on `/` while the API still looks healthy):

   ```sh
   for p in / /healthz /llms.txt /api/v1 /api/v1/schema /api/v1/openapi.yaml /api/v1/events; do
     code=$(curl -s -o /dev/null -w '%{http_code}' "https://events.nukk.net$p")
     echo "$code  $p"
   done
   ```

   Expect 200 on every path. `/` must return the JSON landing (contains
   `"service":"event-intelligence-api"`); a bare `404 page not found` means the
   deployed binary is stale — rebuild and redeploy before proceeding.

## Rollback

- `systemctl disable --now eventsintel-api eventsintel-ingest.timer`
- Remove the `events.nukk.net` block from `/etc/caddy/Caddyfile` → reload.
- Delete the Cloudflare A record.
- `git revert` is not applicable (no repo); keep the prior binary as `eventsintel.bak`.
