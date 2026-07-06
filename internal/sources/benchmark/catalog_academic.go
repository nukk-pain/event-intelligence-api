package benchmark

import "github.com/smpain/event-intelligence-api/internal/sources"

var academicCatalog = []catalogEvent{
	{
		EventID:      "benchmark-icml-2026",
		URL:          "https://icml.cc/Conferences/2026",
		Name:         "ICML 2026",
		StartRaw:     "2026-07-06",
		EndRaw:       "2026-07-11",
		Country:      "KR",
		Timezone:     "Asia/Seoul",
		Format:       "onsite",
		VenueName:    "COEX Convention & Exhibition Center",
		City:         "Seoul",
		Organizer:    "International Machine Learning Society",
		Summary:      "ICML 2026 is the Forty-Third International Conference on Machine Learning in Seoul, covering machine learning research, tutorials, workshops, expo programming, and AI ecosystem signals.",
		ClassifyHint: "AI machine learning research Seoul COEX tutorials workshops expo",
		Actions: sources.ActionSignals{
			CanRegister: boolPtr(true),
			CanExhibit:  boolPtr(false),
			RegisterURL: stringPtr("https://icml.cc/Register/view-registration"),
			CostHint:    stringPtr("paid"),
		},
		Notes: "Official page says tutorial and main conference registrations are sold out, workshop registration remains open, and exhibitor applications are closed.",
	},
}
