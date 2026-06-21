// Package classify maps event text (title + organizer, see
// sources.ParsedEvent.ClassifyText) to controlled-taxonomy categories using cheap
// deterministic keyword rules — NO LLM. The controlled vocabulary lives once in
// taxonomy.go (the SSOT) and is shared with internal/normalize.
//
// Two outcomes are possible:
//   - one or more taxonomy categories matched   → industry event (excluded=false)
//   - nothing matched (an explicit non-industry signal, OR simply no industry
//     keyword) → excluded=true, no categories, confidence "low". Such events are
//     PRESERVED (stored + hidden from the default listing), never dropped.
//
// DEFERRED (later PLAN): a cheap-model fallback for ambiguous titles. v1 deliberately
// returns confidence "low" instead of calling any model, per PLAN Task 2.5 and the
// project cost-efficiency mandate (rules/search before LLM). Do NOT add an LLM call
// here without a PLAN.
package classify

import (
	"strings"
	"unicode/utf8"
)

// aiFollowedByHangul reports whether the text contains "ai" immediately
// followed by a Hangul syllable. Korean event titles commonly compound Latin
// "AI" with Korean without a space (e.g. "AI안전보건박람회"), which the
// space-bounded keyword "ai " misses. This avoids false positives on English
// words like "available" or "email" by requiring a Hangul character after "ai".
func aiFollowedByHangul(lower string) bool {
	idx := 0
	for {
		i := strings.Index(lower[idx:], "ai")
		if i < 0 {
			return false
		}
		pos := idx + i + 2
		if pos < len(lower) {
			r, _ := utf8.DecodeRuneInString(lower[pos:])
			if r >= 0xAC00 && r <= 0xD7A3 {
				return true
			}
		}
		idx = idx + i + 2
	}
}

// rule pairs a taxonomy category with the lowercased keyword fragments that imply
// it. A match is a case-insensitive substring; Korean fragments need no word
// boundaries (CJK has none), Latin ones are kept distinctive to avoid false hits.
type rule struct {
	category string
	keywords []string
}

// categoryRules are evaluated in Categories order so output is deterministic. Keep
// keywords specific: a fragment that also appears in an unrelated context causes
// misclassification (the cheaper failure here is a missed match → low confidence,
// not a wrong category).
var categoryRules = []rule{
	{
		category: CategoryAI,
		keywords: []string{
			"ai ", " ai", "ai엑스포", "ai expo", "인공지능", "artificial intelligence",
			"generative", "생성형", "딥러닝", "deep learning", "머신러닝",
			"machine learning", "llm", "챗봇", "데이터 사이언스",
		},
	},	{
		category: CategoryHumanoidRobotics,
		keywords: []string{
			"로봇", "robot", "휴머노이드", "humanoid", "로보", "robo",
			"자동화", "automation", "스마트팩토리", "smart factory", "스마트 팩토리",
			"무인화", "협동로봇", "cobot",
		},
	},
	{
		category: CategoryBio,
		keywords: []string{
			"바이오", "bio", "제약", "pharma", "신약", "biotech", "생명공학",
			"제약·바이오", "lab automation", "랩 자동화", "셀", "유전체", "genomics",
		},
	},
	{
		category: CategoryMedicalDevices,
		keywords: []string{
			"의료기기", "medical device", "medical devices", "진단기기", "진단", "diagnostic",
			"병원설비", "병원", "hospital", "kimes", "수술", "surgical", "임플란트",
			"치과", "dental", "영상진단",
		},
	},
	{
		category: CategoryDigitalHealth,
		keywords: []string{
			"디지털 헬스", "디지털헬스", "digital health", "헬스케어", "healthcare",
			"health care", "재활", "rehabilitation", "원격의료", "telemedicine",
			"telehealth", "의료it", "헬스케어 it", "웰니스", "wellness",
		},
	},
}

// nonIndustryKeywords mark venue-calendar events that are NOT industry events but
// MUST be preserved (excluded=true, never deleted — disappearance policy). COEX's
// `exhibitions` calendar mixes these in (feasibility doc). Keep this list to clear,
// unambiguous signals so an industry event is not accidentally excluded.
var nonIndustryKeywords = []string{
	"유학설명회", "유학 설명회", "study abroad", "어학연수",
	"채용박람회", "채용 박람회", "취업박람회", "취업 박람회", "일자리 박람회", "잡페어", "job fair",
	"입시설명회", "입시 설명회", "대입", "수험생",
	"동문회", "총동창회", "alumni",
	"웨딩박람회", "웨딩 박람회", "wedding",
	"공무원", "고시", "자격증 시험",
}

// Classify assigns taxonomy categories to event text via keyword rules.
//
// Returns:
//   - categories: matched taxonomy slugs (always a subset of Categories; possibly nil)
//   - excluded:   true for non-industry events (preserved but filtered out of the
//     default API listing); when excluded, categories is always nil
//   - confidence: "high" when at least one category matched, "low" when nothing
//     matched / the title is ambiguous. (No "medium" yet; the cheap-model fallback
//     that would refine ambiguous cases is DEFERRED — see package doc.)
//
// Every returned category is guaranteed to be a member of the SSOT Categories list
// (categoryRules only ever emit rule.category values, each a taxonomy constant).
func Classify(text string) (categories []string, excluded bool, confidence string) {
	lower := strings.ToLower(text)

	// Exclusion is terminal: a non-industry event is kept (for provenance) but
	// gets no categories and is flagged. Checked first so e.g. an AI-themed
	// "유학설명회" is excluded rather than mis-tagged ai.
	for _, kw := range nonIndustryKeywords {
		if strings.Contains(lower, kw) {
			return nil, true, "low"
		}
	}

	for _, r := range categoryRules {
		if matchesAny(lower, r.keywords) {
			categories = append(categories, r.category)
		}
	}

	if len(categories) == 0 && aiFollowedByHangul(lower) {
		categories = append(categories, CategoryAI)
	}

	if len(categories) == 0 {
		// No industry keyword matched → the event is outside our taxonomy wedge
		// (ai/robotics/bio/medical-devices/digital-health). It is EXCLUDED:
		// preserved with excluded=true (stored, hidden from the default listing,
		// never dropped) rather than rejected by normalize's rule-1 category
		// requirement. confidence "low" flags it for the DEFERRED cheap-model
		// recall pass — a real industry event whose title lacks our keywords lands
		// here and can be reclassified later without data loss.
		return nil, true, "low"
	}

	return categories, false, "high"
}

func matchesAny(lowerText string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(lowerText, kw) {
			return true
		}
	}
	return false
}
