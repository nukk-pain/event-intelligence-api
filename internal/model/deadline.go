package model

import (
	"strings"
	"time"
)

// FarOffDeadlineDays is how distant an event has to be before an already-passed
// deadline stops being credible as its own. Measured against live data on
// 2026-07-30: the inherited values sat 90 to 256 days out, every genuine missed
// deadline was inside 32. Sixty separates them with room on both sides.
const FarOffDeadlineDays = 60

// DeadlinePlausible reports whether a deadline can belong to this event.
//
// An organizer homepage describes only its nearest edition, so a later edition
// listing that same page inherits a deadline that was never its own. Observed
// 2026-07-30: 제54회 맘앤베이비엑스포 (8/6) and 제55회 (11/26) both carried reg
// 06-22 / exh 07-07 from momnbabyexpo.co.kr, and 한가위 (8/14) and 설맞이
// (12/22) both carried 07-10 / 07-17 from fgfair.com. A date already behind us
// cannot be the deadline for something still two months away.
//
// Separately, no real deadline precedes its event by more than a year. A value
// that does is a parse fault, as in the 2025-08-14 stored against a 2026-09-09
// event.
//
// Dates it cannot read are not judged: an absent start date or an unparseable
// reference time leaves the value alone rather than guessing at it.
func DeadlinePlausible(deadline, start, now string) bool {
	d, ok := isoDay(deadline)
	if !ok {
		return true
	}
	s, haveStart := isoDay(start)
	if haveStart && d.Before(s.AddDate(-1, 0, 0)) {
		return false
	}
	today, ok := isoDay(now)
	if !ok || !d.Before(today) || !haveStart {
		return true
	}
	return s.Sub(today).Hours()/24 <= FarOffDeadlineDays
}

func isoDay(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 10 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s[:10])
	return t, err == nil
}
