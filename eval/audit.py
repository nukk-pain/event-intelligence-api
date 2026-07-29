#!/usr/bin/env python3
"""Accuracy audit for every field Solar added during ingest enrichment.

Coverage numbers say Solar filled a field; this audit asks whether the filled
value is actually supported by the official page. Every value is checked
against the event's own source pages, and a value whose evidence cannot be
found is classified as unverified rather than assumed correct.

Classifications:
  verified            evidence found on a source page, with deadline context
  partial             evidence found, but the surrounding context does not
                      name the specific deadline type (date could belong to
                      something else on the page)
  unverified          no evidence found on any reachable source page
  wrong_type          date found but context names the OTHER deadline
  unreachable         no source page could be fetched
For register_url:
  verified            URL answers 2xx AND is linked from a source page
  partial             URL answers 2xx but is not linked from the pages read
  broken              URL does not answer 2xx

Usage: python3 eval/audit.py            # writes eval/audit_results.json
       python3 eval/audit.py --report   # prints summary from existing results
"""

import json
import re
import sys
import time
import urllib.request
from pathlib import Path

HERE = Path(__file__).parent
UA = {"User-Agent": "eventsintel-eval/1.0 (+https://events.nukk.net)"}

REGISTER_CONTEXT = ["등록", "참가 신청", "참가신청", "참관", "사전등록", "registration", "register"]
EXHIBIT_CONTEXT = ["부스", "출품", "전시 신청", "전시신청", "참가업체", "exhibitor", "booth", "exhibit"]
DEADLINE_WORDS = ["마감", "까지", "접수", "신청", "deadline", "due", "close"]


def fetch(url: str) -> str:
    req = urllib.request.Request(url, headers=UA)
    with urllib.request.urlopen(req, timeout=20) as resp:
        return resp.read(1 << 20).decode("utf-8", errors="replace")


def date_patterns(iso: str) -> list[str]:
    y, m, d = iso.split("-")
    mi, di = int(m), int(d)
    return [
        rf"{y}\s*[-./년]\s*0?{mi}\s*[-./월]\s*0?{di}",
        rf"(?<!\d)0?{mi}\s*[./월]\s*0?{di}(?!\d)",
    ]


def find_date(text: str, iso: str) -> tuple[bool, str]:
    """Returns (found, surrounding context) for the first match."""
    for pat in date_patterns(iso):
        match = re.search(pat, text)
        if match:
            lo, hi = max(0, match.start() - 90), match.end() + 90
            return True, re.sub(r"\s+", " ", text[lo:hi])
    return False, ""


def classify_deadline(row: dict, pages: dict[str, str]) -> dict:
    field = row["field"]
    want_ctx = REGISTER_CONTEXT if field == "registration_deadline" else EXHIBIT_CONTEXT
    other_ctx = EXHIBIT_CONTEXT if field == "registration_deadline" else REGISTER_CONTEXT
    best = {"classification": "unverified", "evidence": ""}
    reached = False
    for url, text in pages.items():
        if text is None:
            continue
        reached = True
        found, ctx = find_date(text, row["value"])
        if not found:
            continue
        ctx_l = ctx.lower()
        has_deadline = any(w in ctx_l for w in DEADLINE_WORDS)
        has_want = any(w.lower() in ctx_l for w in want_ctx)
        has_other = any(w.lower() in ctx_l for w in other_ctx)
        if has_deadline and has_want:
            return {"classification": "verified", "evidence": f"{url} :: {ctx}"}
        if has_deadline and has_other and not has_want:
            best = {"classification": "wrong_type", "evidence": f"{url} :: {ctx}"}
        elif best["classification"] == "unverified":
            best = {"classification": "partial", "evidence": f"{url} :: {ctx}"}
    if not reached:
        return {"classification": "unreachable", "evidence": ""}
    return best


def classify_name_en(row: dict, pages: dict[str, str]) -> dict:
    needle = re.sub(r"\s+", " ", row["value"]).strip().lower()
    reached = False
    for url, text in pages.items():
        if text is None:
            continue
        reached = True
        hay = re.sub(r"\s+", " ", text).lower()
        if needle and needle in hay:
            return {"classification": "verified", "evidence": url}
        # Partial: every significant token appears, just not contiguously.
        tokens = [t for t in re.split(r"[^a-z0-9]+", needle) if len(t) > 2]
        if tokens and all(t in hay for t in tokens):
            return {"classification": "partial", "evidence": f"{url} :: tokens present, not contiguous"}
    return {"classification": "unreachable" if not reached else "unverified", "evidence": ""}


def classify_register_url(row: dict, pages: dict[str, str]) -> dict:
    target = row["value"]
    status = None
    try:
        req = urllib.request.Request(target, headers=UA, method="GET")
        with urllib.request.urlopen(req, timeout=20) as resp:
            status = resp.status
    except Exception as exc:  # noqa: BLE001 - any failure means not reachable
        return {"classification": "broken", "evidence": f"fetch failed: {exc}"}
    if not (200 <= status < 300):
        return {"classification": "broken", "evidence": f"status {status}"}
    bare = target.rstrip("/")
    for url, text in pages.items():
        if text and (bare in text or target in text):
            return {"classification": "verified", "evidence": f"linked from {url}, answers {status}"}
    return {"classification": "partial", "evidence": f"answers {status}; link not found on source pages read"}


def run_audit() -> None:
    rows = json.loads((HERE / "solar_added_fields.json").read_text())
    page_cache: dict[str, str | None] = {}

    def get_pages(urls: list[str]) -> dict[str, str | None]:
        out = {}
        for url in urls:
            if url not in page_cache:
                try:
                    page_cache[url] = fetch(url)
                except Exception:
                    page_cache[url] = None
                time.sleep(1.2)  # politeness across many distinct hosts
            out[url] = page_cache[url]
        return out

    results = []
    for i, row in enumerate(rows, 1):
        pages = get_pages(row.get("evidence_urls", []))
        if row["field"] in ("registration_deadline", "exhibitor_deadline"):
            verdict = classify_deadline(row, pages)
        elif row["field"] == "name_en":
            verdict = classify_name_en(row, pages)
        elif row["field"] == "register_url":
            verdict = classify_register_url(row, pages)
        else:
            verdict = {"classification": "unaudited", "evidence": ""}
        if row.get("retired"):
            verdict["historical_classification"] = verdict["classification"]
            verdict["classification"] = "revoked"
        results.append({**row, **verdict})
        print(f"[{i:2}/{len(rows)}] {row['field']:<22} {verdict['classification']:<12} {row['event_id'][:40]}")
    (HERE / "audit_results.json").write_text(
        json.dumps(results, ensure_ascii=False, indent=1))
    report()


def report() -> None:
    results = json.loads((HERE / "audit_results.json").read_text())
    from collections import Counter
    retired_keys = {
        (row["event_id"], row["field"], row["value"])
        for row in json.loads((HERE / "solar_added_fields.json").read_text())
        if row.get("retired")
    }
    active, retired = [], []
    for row in results:
        key = (row["event_id"], row["field"], row["value"])
        if key in retired_keys or row.get("classification") == "revoked":
            retired.append(row)
        else:
            active.append(row)
    total = len(active)
    by_class = Counter(r["classification"] for r in active)
    print("\n== Solar enrichment accuracy audit ==")
    print(f"fields audited: {total}")
    if retired:
        print(f"  revoked      {len(retired):>3}  (historical rows removed by migration)")
    for cls in ["verified", "partial", "unverified", "wrong_type", "broken", "unreachable"]:
        if by_class.get(cls) or cls == "wrong_type":
            print(f"  {cls:<12} {by_class[cls]:>3}  ({by_class[cls]/total*100:.0f}%)")
    print("\nby field:")
    fields = sorted({r["field"] for r in active})
    for f in fields:
        sub = [r for r in active if r["field"] == f]
        v = sum(1 for r in sub if r["classification"] == "verified")
        print(f"  {f:<24} verified {v}/{len(sub)}")
    summary = {
        "total": total,
        "by_classification": dict(by_class),
        "revoked": len(retired),
        "by_field": {f: {"total": len([r for r in active if r["field"] == f]),
                          "verified": len([r for r in active if r["field"] == f and r["classification"] == "verified"])}
                     for f in fields},
    }
    (HERE / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    if "--report" in sys.argv:
        report()
    else:
        run_audit()
