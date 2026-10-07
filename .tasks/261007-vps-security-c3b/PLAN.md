# C3b security deployment preparation

## Scope

Harden the two HTTP daemon units, separate the API reader from the existing
ingest writer, document deployment and rollback, and replace the approved dated
Markdown test fixture with today +30/+32 days. Product Go code, event schema,
ports, models, ingest schedule, live services, push and merge remain unchanged.
The existing C3a contracts and coordinator decisions govern this work.

## Evidence Gate

Allowed conclusions: resolved, unresolved, cannot determine. Implementation
completion is distinct from deployment completion; no live release is performed.

| Condition | Evidence |
| --- | --- |
| Dedicated daemon identities and strict sandbox | unit diff, systemd 255 verify and offline security before/after |
| Quota is the only MCP persistent write exception | StateDirectory 0700, UMask 0077, explicit quota env and ReadWritePaths |
| Reader cannot mutate DB/directory and reads new WAL commits | isolated Linux synthetic DB with actual separate UID/group and sandbox |
| Sidecar absence and restart are handled | writer close/checkpoint/recreation/reader-first tests; separate inactive writer bootstrap runs before each API start |
| Deployment can restore binaries, units and permissions | sequential adversarial review of documented failure and rollback commands |
| Date fixture is stable and full gates pass | focused test, make test, make vet, make build, API race check |
| Public commit contains no operational address, private workspace path or secret | staged diff review and security scan |

## Failure modes

| Failure mode | Cheapest check |
| --- | --- |
| Read-only WAL fails when the writer removed sidecars | reader-first synthetic Linux test with absent WAL/SHM |
| New sidecars lose shared read permissions | actual writer UID plus setgid directory and UMask 0027 |
| Sandbox prevents quota persistence or network access | isolated Linux unit execution with fake provider/read API |
| Rollback resets paid-call budget or removes the site | review restoration commands; prohibit quota/DB rewind and Caddy/DNS removal |

## Execution

1. Inspect predecessor reports, current units, DB open paths and deployment guide.
2. Apply daemon hardening and the authorized test fixture correction.
3. Verify the minimal writer bootstrap under the existing A4-08 scope while preserving the read-only API.
4. Verify Linux permissions and unit configuration without changing live service state.
5. Write deployment/rollback commands and record exact limitations.
6. Run repository gates, review the diff, commit locally, and report once.

VPS commands that mutate production belong only in the deployment notes. The
explicitly authorized unit verify/security checks use disposable /tmp copies.
Repeated failures without new evidence require a different safe approach;
missing authority or evidence is reported without claiming deployment success.

## Verification result

- Full tests, vet, both daemon builds, API race and source vulnerability audit pass.
- VPS systemd255 verifies all four unit copies; static scores are API8.7→3.1
  and MCP9.6→3.1. Live units remain unchanged.
- Isolated Ubuntu/systemd255 uses separate writer/reader/MCP UIDs. Final units
  recover absent sidecars before reader startup and preserve0640 group-readable
  files on writer recreation. Reader writes fail; new writer commits are read.
- Missing DB blocks API startup without creating an empty DB; restoring the
  synthetic DB and starting again recovers health and all four test events.
- MCP quota0700/files0600 and counts1→restart→2 persist under the actual unit;
  only a fake loopback model was called. Origin verify.sh passes.
- Deployment shell blocks pass bash syntax checks. Sequential review preserves
  quota/event data and old releases during partial failure and rollback.
- Implementation conclusion: resolved. Live deployment is pending Phase3,
  including amd64 WAL/UID checks, C0 identity, binary hashes and edge smoke.
