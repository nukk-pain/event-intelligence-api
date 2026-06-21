package classify

import (
	"sort"
	"testing"
)

// labeled holds one classification expectation. want is the set of categories we
// expect to be present (subset check unless wantExact); excluded/confidence are
// the other two return values.
type labeled struct {
	name       string
	text       string
	want       []string // categories expected present
	wantExact  bool     // if true, produced categories must equal want exactly
	excluded   bool
	confidence string // "" means don't assert confidence
}

func TestClassify_LabeledCases(t *testing.T) {
	cases := []labeled{
		// --- one labeled case per taxonomy category ---
		{
			name:       "ai_expo",
			text:       "AI EXPO KOREA 2026 — 인공지능 대전, 생성형 AI 솔루션",
			want:       []string{CategoryAI},
			confidence: "high",
		},
		{
			name:       "humanoid_robotics",
			text:       "로보월드 2026 — 휴머노이드 로봇 및 스마트팩토리 자동화 전시회",
			want:       []string{CategoryHumanoidRobotics},
			confidence: "high",
		},
		{
			name:       "bio",
			text:       "BIO KOREA 2026 — 바이오·제약 파트너링, 신약개발 컨퍼런스",
			want:       []string{CategoryBio},
			confidence: "high",
		},
		{
			name:       "medical_devices",
			text:       "KIMES 2026 국제의료기기·병원설비 전시회, 진단기기",
			want:       []string{CategoryMedicalDevices},
			confidence: "high",
		},
		{
			name:       "digital_health",
			text:       "디지털 헬스케어 박람회 — 재활로봇, 헬스케어 IT, 원격의료",
			want:       []string{CategoryDigitalHealth},
			confidence: "high",
		},
		// --- multi-category event (AI + medical devices) ---
		{
			name: "ai_plus_medical",
			text: "AI 의료영상 진단기기 컨퍼런스 — 인공지능 기반 medical device",
			want: []string{CategoryAI, CategoryMedicalDevices},
		},
		// --- non-industry events: kept but excluded ---
		{
			name:     "study_abroad_fair",
			text:     "2026 미국 유학설명회 — 해외 대학 입학 상담회",
			excluded: true,
		},
		{
			name:     "recruitment_fair",
			text:     "2026 상반기 채용박람회 — 일자리 취업 상담",
			excluded: true,
		},
		// --- conflict: exclusion wins even when an industry keyword is present.
		// classify.go:102-106 promises an exclusion keyword beats an INDUSTRY
		// keyword (so an AI-themed 유학설명회 is excluded, not mis-tagged ai). This
		// is enforced only by the exclusion loop running before the category loop.
		// wantExact with empty want asserts len(categories)==0 (AC-6 spirit).
		{
			name:      "ai_themed_study_abroad_excluded",
			text:      "AI 인공지능 유학설명회 — 해외 대학 입학 상담",
			want:      nil,
			wantExact: true,
			excluded:  true,
		},
		// --- no industry keyword: excluded (out of wedge), low confidence ---
		// A non-matching event is preserved as excluded=true (stored + hidden),
		// never dropped — its 0 categories must not trip normalize rule-1.
		{
			name:       "no_industry_keyword_excluded",
			text:       "제12회 정기 총회 및 네트워킹 행사",
			excluded:   true,
			confidence: "low",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, excluded, confidence := Classify(tc.text)

			if excluded != tc.excluded {
				t.Fatalf("excluded = %v, want %v (text=%q)", excluded, tc.excluded, tc.text)
			}

			if tc.confidence != "" && confidence != tc.confidence {
				t.Errorf("confidence = %q, want %q (text=%q)", confidence, tc.confidence, tc.text)
			}

			if tc.wantExact {
				if !equalSet(got, tc.want) {
					t.Errorf("categories = %v, want exactly %v", got, tc.want)
				}
			} else {
				for _, w := range tc.want {
					if !contains(got, w) {
						t.Errorf("categories = %v, missing expected %q", got, w)
					}
				}
			}
		})
	}
}

// TestClassify_OutOfTaxonomyGuard is the negative test required by Task 2.5: every
// category Classify ever produces MUST be a member of the SSOT Categories list. If
// a future rule emits an out-of-vocab slug, this fails.
func TestClassify_OutOfTaxonomyGuard(t *testing.T) {
	texts := []string{
		"AI EXPO KOREA 2026 인공지능 generative AI",
		"로보월드 휴머노이드 로봇 자동화 스마트팩토리",
		"BIO KOREA 바이오 제약 신약 lab automation",
		"KIMES 의료기기 진단 병원 medical device",
		"디지털 헬스케어 재활 원격의료 healthcare IT",
		"AI 의료 진단기기 로봇 바이오 융합 전시회", // try to trip every rule at once
		"유학설명회 채용박람회 세미나",          // non-industry
		"정기 총회 네트워킹",               // ambiguous
	}
	for _, txt := range texts {
		cats, _, _ := Classify(txt)
		for _, c := range cats {
			if !IsCategory(c) {
				t.Errorf("Classify(%q) produced out-of-taxonomy category %q (not in %v)", txt, c, Categories)
			}
		}
	}
}

// TestClassify_ExcludedNeverHasCategories: an excluded (non-industry) event should
// not also be classified into an industry category — exclusion is terminal.
func TestClassify_ExcludedReturnsNoCategories(t *testing.T) {
	cats, excluded, _ := Classify("2026 유학설명회 해외 대학 입학 상담")
	if !excluded {
		t.Fatalf("expected excluded=true for study-abroad fair")
	}
	if len(cats) != 0 {
		t.Errorf("excluded event should have no categories, got %v", cats)
	}
}

// TestTaxonomy_SSOTMembership: every SSOT constant is reachable via IsCategory /
// IsAudience, and the lists have no duplicates. Guards accidental drift between the
// const declarations and the exported slices.
func TestTaxonomy_SSOTMembership(t *testing.T) {
	for _, c := range Categories {
		if !IsCategory(c) {
			t.Errorf("Categories contains %q but IsCategory says false", c)
		}
	}
	for _, a := range Audiences {
		if !IsAudience(a) {
			t.Errorf("Audiences contains %q but IsAudience says false", a)
		}
	}
	if dup := firstDup(Categories); dup != "" {
		t.Errorf("Categories has duplicate %q", dup)
	}
	if dup := firstDup(Audiences); dup != "" {
		t.Errorf("Audiences has duplicate %q", dup)
	}
	if IsCategory("not-a-real-category") {
		t.Error("IsCategory returned true for a bogus slug")
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}

func firstDup(xs []string) string {
	seen := map[string]struct{}{}
	for _, x := range xs {
		if _, ok := seen[x]; ok {
			return x
		}
		seen[x] = struct{}{}
	}
	return ""
}
