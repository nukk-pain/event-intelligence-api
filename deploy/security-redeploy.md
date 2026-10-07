# Security redeploy: read API and MCP

This is the release/rollback procedure for the two existing HTTP daemons. Run
host-changing commands only during an authorized deployment. Existing event
data, schema, ingest cadence, provider model and ports remain unchanged.

## Release gates and order

1. C0 installs the canonical `X-Real-Client-IP` on all three Caddy proxy routes,
   retaining the legacy headers needed by the old apps until rollout completes.
2. Preserve the current binaries, unit files, drop-ins and permission metadata.
3. Prepare the dedicated accounts, shared DB read group and private quota path.
4. Upload and verify both new binaries before replacing either executable.
5. Install the verified units without replacing existing drop-in directories.
6. Reload systemd and restart the API.
7. Verify the API at the origin before restarting MCP.
8. Verify MCP health, initialize, tools/list and model-free search at the origin.
9. C0 removes legacy headers after origin identity separation is demonstrated.
10. Verify the public edge and update the deployment inventory.

Caddy owns the security headers and trusted proxy ranges. Do not copy the
repository site block over the live site or restore the shared Caddyfile from
an old snapshot. A cache HIT does not prove that origin quotas distinguish
clients. The release stops on a hash mismatch, permission error, failed origin
check, unexpected quota reset or cancellation leak.

## Build and provenance (local)

Build the reviewed commit from a clean ordinary clone. Go 1.26 may omit VCS
metadata in a linked worktree even with `-buildvcs=true`; reject that artifact.

```sh
test -z "$(git status --porcelain)"
GOTOOLCHAIN=go1.26.8 GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=true -o eventsintel-linux ./cmd/eventsintel
GOTOOLCHAIN=go1.26.8 GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=true -o eventmcp-linux ./cmd/eventmcp
go version -m eventsintel-linux
go version -m eventmcp-linux
shasum -a 256 eventsintel-linux eventmcp-linux > release.sha256
```

Both build records must show the reviewed `vcs.revision`, `vcs.modified=false`,
Go 1.26.8, `CGO_ENABLED=0`, `GOOS=linux` and `GOARCH=amd64`. Run source and both
binary `govulncheck` gates. The deployed MCP filename is `eventsintel-mcp`.
This security release does not need a seed DB upload, event migration or crawl.
Other releases that add migrations retain the migration-before-reader order
in [README.md](README.md).

## Accounts and files (host deployment notes)

| Identity/path | Ownership and mode | Access |
| --- | --- | --- |
| `eventsintel` | existing writer account; supplementary `eventsintel-read` | ingest, Chrome state, DB and sidecar creation |
| `eventsintel-api` | new system account; supplementary `eventsintel-read` | read DB/WAL/SHM, no directory or file write |
| `eventmcp` | new system account; no DB group | loopback API and provider HTTP; private quota only |
| deploy root and binaries | `root:root`, directory/executables `0755` | daemon cannot replace binaries |
| `data/` | `eventsintel:eventsintel-read`, `2750` | setgid preserves group on new sidecars; group has no write |
| `events.db`, `events.db-wal`, `events.db-shm` | `eventsintel:eventsintel-read`, `0640` | writer read/write, reader group read |
| ingest lock | writer-owned, `0640` | reader does not acquire writer lock |
| provider/purge env files | `root:root`, `0600` | PID 1 loads EnvironmentFile before dropping privilege |
| `/var/lib/eventmcp-quota` | `eventmcp:eventmcp`, `0700` | MCP persistent state, separate from event DB |
| quota lock and journal | `eventmcp:eventmcp`, `0600` | reservations survive restart and rollback |

Both daemon units use `ProtectSystem=strict`, `ProtectHome`, private temporary
and device namespaces, `NoNewPrivileges`, empty capabilities and bounded
memory/CPU/tasks/file descriptors. PrivateTmp is ephemeral writable storage;
the only MCP persistent write exception is its quota directory. The API has
no `ReadWritePaths`. Ingest keeps its existing primary group, Chrome
StateDirectory, lock, deadline, timer and credential files, with `UMask=0027`
to preserve shared read access on newly created SQLite sidecars.

Use a root shell on the host. Set the reviewed commit and staging directory
explicitly; do not paste a placeholder value into a running deployment.

```sh
set -eu
EVENTS_DEPLOY_ROOT=$(systemctl show eventsintel-api.service -p WorkingDirectory --value)
test -n "$EVENTS_DEPLOY_ROOT"
: "${EVENTS_COMMIT:?Set the reviewed commit SHA}"
: "${EVENTS_STAGE:?Set the uploaded release staging directory}"
EVENTS_RELEASE="$(date -u +%Y%m%dT%H%M%SZ)-$EVENTS_COMMIT"
EVENTS_BACKUP="$EVENTS_DEPLOY_ROOT/releases/$EVENTS_RELEASE"
install -d -o root -g root -m 0700 "$EVENTS_BACKUP/units"
cp -a "$EVENTS_DEPLOY_ROOT/eventsintel" "$EVENTS_BACKUP/eventsintel"
cp -a "$EVENTS_DEPLOY_ROOT/eventsintel-mcp" "$EVENTS_BACKUP/eventsintel-mcp"
systemctl show eventsintel-ingest.timer -p ActiveState -p UnitFileState > "$EVENTS_BACKUP/timer-state"
for unit in eventsintel-api eventmcp-api eventsintel-ingest; do
  cp -a "/etc/systemd/system/$unit.service" "$EVENTS_BACKUP/units/"
  if test -d "/etc/systemd/system/$unit.service.d"; then
    cp -a "/etc/systemd/system/$unit.service.d" "$EVENTS_BACKUP/units/"
  fi
done
if test -f /etc/systemd/system/eventsintel-wal-prepare.service; then
  cp -a /etc/systemd/system/eventsintel-wal-prepare.service "$EVENTS_BACKUP/units/"
else
  touch "$EVENTS_BACKUP/units/eventsintel-wal-prepare.absent"
fi
stat -c '%u %g %a %n' "$EVENTS_DEPLOY_ROOT" "$EVENTS_DEPLOY_ROOT/data" "$EVENTS_DEPLOY_ROOT/eventsintel" "$EVENTS_DEPLOY_ROOT/eventsintel-mcp" > "$EVENTS_BACKUP/permissions"
find "$EVENTS_DEPLOY_ROOT/data" -maxdepth 1 -type f \( -name 'events.db' -o -name 'events.db-wal' -o -name 'events.db-shm' -o -name 'ingest.lock' \) -exec stat -c '%u %g %a %n' {} + >> "$EVENTS_BACKUP/permissions"
systemctl stop eventsintel-ingest.timer
if systemctl is-active --quiet eventsintel-ingest.service; then
  if grep -qx 'ActiveState=active' "$EVENTS_BACKUP/timer-state"; then
    systemctl start eventsintel-ingest.timer
  fi
  echo 'Wait for the current ingest to finish before retrying.' >&2
  exit 1
fi
getent group eventsintel-read >/dev/null || groupadd --system eventsintel-read
id -u eventsintel-api >/dev/null 2>&1 || useradd --system --user-group --no-create-home --shell /usr/sbin/nologin eventsintel-api
id -u eventmcp >/dev/null 2>&1 || useradd --system --user-group --no-create-home --shell /usr/sbin/nologin eventmcp
usermod -a -G eventsintel-read eventsintel
usermod -a -G eventsintel-read eventsintel-api
chown eventsintel:eventsintel-read "$EVENTS_DEPLOY_ROOT/data"
chmod 2750 "$EVENTS_DEPLOY_ROOT/data"
for path in events.db events.db-wal events.db-shm ingest.lock; do
  if test -f "$EVENTS_DEPLOY_ROOT/data/$path"; then
    chown eventsintel:eventsintel-read "$EVENTS_DEPLOY_ROOT/data/$path"
    chmod 0640 "$EVENTS_DEPLOY_ROOT/data/$path"
  fi
done
if test -e /var/lib/eventmcp-quota; then
  test "$(stat -c '%a %U %G' /var/lib/eventmcp-quota)" = '700 eventmcp eventmcp'
else
  install -d -o eventmcp -g eventmcp -m 0700 /var/lib/eventmcp-quota
fi
```

On a later redeploy do not recursively chown, truncate, delete or restore an
older quota journal. Verify existing quota ownership/modes rather than hiding
unexpected state. Keep env contents out of terminal output and reports. Preserve
the existing ingest `cf-purge` drop-in.

## WAL startup gate

SQLite read-only WAL requires existing usable sidecars when the reader cannot
create them. `mode=ro` does not solve their absence, and `immutable=1` is invalid
for a DB still updated by ingest. See [SQLite read-only WAL](https://sqlite.org/wal.html#read_only_databases).

`eventsintel-api.service` requires and starts after
`eventsintel-wal-prepare.service`. This short oneshot runs as the existing
writer under `UMask=0027`, checks the DB exists, and uses the host's `/usr/bin/sqlite3`
to apply `.filectrl persist_wal 1` before reading schema metadata. It preserves
sidecars when that connection closes without changing event rows. The oneshot
does not remain active, so each API start prepares them again if a writer
removed them while the API was down. API sandbox permissions stay read-only;
only the separate writer preparation unit can write the data directory.

Check the host sqlite3 supports this file control before rollout. Validate
reader-first startup, writer close/restart, checkpoint, sidecar recreation and
API restart on a synthetic Linux DB under the actual UID/group and unit sandbox.
Do not delete operational WAL/SHM to run this test. The isolated test must also
show denied reader file/directory writes and new writer commits becoming visible.
Failure of preparation blocks the reader; never grant the API data write access
to work around it. A missing DB fails without creating an empty replacement.

## Binary/unit switch and health

In the staging directory compare both uploaded hashes against the reviewed
manifest before these commands. Install files on the destination filesystem
and rename there so the executable switch is atomic. Both old copies remain.

```sh
install -o root -g root -m 0755 "$EVENTS_STAGE/eventsintel-linux" "$EVENTS_DEPLOY_ROOT/eventsintel.new"
install -o root -g root -m 0755 "$EVENTS_STAGE/eventmcp-linux" "$EVENTS_DEPLOY_ROOT/eventsintel-mcp.new"
sha256sum "$EVENTS_DEPLOY_ROOT/eventsintel.new" "$EVENTS_DEPLOY_ROOT/eventsintel-mcp.new"
```

Stop if either hash differs. Then run:

```sh
mv -f "$EVENTS_DEPLOY_ROOT/eventsintel.new" "$EVENTS_DEPLOY_ROOT/eventsintel"
mv -f "$EVENTS_DEPLOY_ROOT/eventsintel-mcp.new" "$EVENTS_DEPLOY_ROOT/eventsintel-mcp"
install -o root -g root -m 0644 "$EVENTS_STAGE/deploy/eventsintel-api.service" /etc/systemd/system/eventsintel-api.service
install -o root -g root -m 0644 "$EVENTS_STAGE/deploy/eventmcp-api.service" /etc/systemd/system/eventmcp-api.service
install -o root -g root -m 0644 "$EVENTS_STAGE/deploy/eventsintel-ingest.service" /etc/systemd/system/eventsintel-ingest.service
install -o root -g root -m 0644 "$EVENTS_STAGE/deploy/eventsintel-wal-prepare.service" /etc/systemd/system/eventsintel-wal-prepare.service
systemd-analyze verify /etc/systemd/system/eventsintel-api.service /etc/systemd/system/eventmcp-api.service /etc/systemd/system/eventsintel-ingest.service /etc/systemd/system/eventsintel-wal-prepare.service
systemctl daemon-reload
systemctl restart eventsintel-api.service
bash "$EVENTS_STAGE/deploy/verify.sh" http://127.0.0.1:3005
```

The origin verification script proves paths, negotiated content and non-empty
event reads; it does not require edge-cache evidence on loopback. On success:

```sh
systemctl restart eventmcp-api.service
curl --retry 3 --retry-connrefused --retry-delay 1 -fsS http://127.0.0.1:3008/healthz
```

Use a short readiness retry before origin verification when a newly started
daemon has not bound its socket. Check that the actual processes have the
expected UID, `NoNewPrivs: 1` and `CapEff: 0000000000000000`. Compare each
deployed executable and `/proc/<MainPID>/exe` SHA-256 with the release manifest.
Confirm env loading and quota modes without printing keys. Confirm Caddy's
upstream timeout allows the 30-second MCP handler/45-second server write budget.
Then run the following JSON-RPC probes against both origin `/mcp` and public
`https://events.nukk.net/mcp`, changing only `EVENTS_MCP_URL`:

```sh
EVENTS_MCP_URL=http://127.0.0.1:3008/mcp
curl -fsS -H 'Content-Type: application/json' "$EVENTS_MCP_URL" --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"release-check","version":"1"}}}'
curl -fsS -H 'Content-Type: application/json' "$EVENTS_MCP_URL" --data '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
curl -fsS -H 'Content-Type: application/json' "$EVENTS_MCP_URL" --data '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_events","arguments":{}}}'
```

Require the expected JSON-RPC ID/result, both tools, and no RPC `error` or tool
`isError=true`. HTTP 200 alone is insufficient. No live paid `ask_events` probe
is required; test provider cancellation and budget persistence with a fake
provider. The public MCP health path is `/mcp/healthz`.

Restore the ingest timer's recorded active/enabled state after origin success;
do not change its schedule or force a crawl. Finish with `deploy/verify.sh` at
the public edge, MCP health/RPC probes and the deployment inventory update from
the operator's private workspace. Keep commit/hash/unit/permission evidence in
the private release record. Do not publish host addresses or private paths.

The deployment never changes the timer's enabled state. Restore its saved
active state with:

```sh
if grep -qx 'ActiveState=active' "$EVENTS_BACKUP/timer-state"; then
  systemctl start eventsintel-ingest.timer
fi
```

## Rollback

For an MCP-only failure keep the healthy API serving. Coordinate with C0 to
restrict public `/mcp` first: an older MCP binary does not enforce the durable
paid-call budget. Keep MCP unavailable unless the restored version still has
equivalent cost protection. C0 restores the old app's required forwarding
headers while preserving the current origin protection and other sites.

Select the preserved release explicitly. For each affected service, restore its
previous executable with an adjacent file and atomic rename:

```sh
systemctl stop eventsintel-ingest.timer
if systemctl is-active --quiet eventsintel-ingest.service; then
  echo 'Wait for ingest to finish before restoring writer files or permissions.' >&2
  exit 1
fi
install -o root -g root -m 0755 "$EVENTS_BACKUP/eventsintel" "$EVENTS_DEPLOY_ROOT/eventsintel.rollback"
mv -f "$EVENTS_DEPLOY_ROOT/eventsintel.rollback" "$EVENTS_DEPLOY_ROOT/eventsintel"
install -o root -g root -m 0755 "$EVENTS_BACKUP/eventsintel-mcp" "$EVENTS_DEPLOY_ROOT/eventsintel-mcp.rollback"
mv -f "$EVENTS_DEPLOY_ROOT/eventsintel-mcp.rollback" "$EVENTS_DEPLOY_ROOT/eventsintel-mcp"
for unit in eventsintel-api eventmcp-api eventsintel-ingest; do
  cp -a "$EVENTS_BACKUP/units/$unit.service" "/etc/systemd/system/$unit.service"
done
if test -f "$EVENTS_BACKUP/units/eventsintel-wal-prepare.absent"; then
  rm -f /etc/systemd/system/eventsintel-wal-prepare.service
else
  cp -a "$EVENTS_BACKUP/units/eventsintel-wal-prepare.service" /etc/systemd/system/eventsintel-wal-prepare.service
fi
while read -r uid gid mode path; do
  if test -e "$path"; then
    chown "$uid:$gid" "$path"
    chmod "$mode" "$path"
  fi
done < "$EVENTS_BACKUP/permissions"
systemctl daemon-reload
systemctl restart eventsintel-api.service
bash "$EVENTS_STAGE/deploy/verify.sh" http://127.0.0.1:3005
```

Run only the affected executable/unit restores; the block shows a full
two-daemon rollback. Existing drop-ins were never replaced; compare them with
the preserved copies and restore only files changed during rollout. Restart MCP
only behind C0's restriction when rolling back to a version without the paid
budget. Restore the recorded ingest timer state, then repeat origin and edge
checks appropriate to the retained service surfaces.

Preserve the event DB, WAL/SHM, current quota reservations, new service accounts
and every previous release. Restoring the journal from an older snapshot would
recharge today's budget. Do not remove DNS, the Caddy site, cache rules or the
healthy API. Those actions are site decommissioning, not a release rollback.
