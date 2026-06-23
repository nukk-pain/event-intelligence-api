package benchmark

import "github.com/smpain/event-intelligence-api/internal/sources"

var chinaCatalog = []catalogEvent{
	{
		EventID:      "benchmark-waic-shanghai-2026",
		URL:          "https://www.worldaic.com.cn/en/",
		Name:         "World Artificial Intelligence Conference 2026",
		StartRaw:     "2026-07-17",
		EndRaw:       "2026-07-20",
		Country:      "CN",
		Timezone:     "Asia/Shanghai",
		Format:       "onsite",
		VenueName:    "Shanghai",
		City:         "Shanghai",
		Organizer:    "World Artificial Intelligence Conference",
		Summary:      "World Artificial Intelligence Conference 2026 is a Shanghai AI conference and exhibition covering global AI research, industry applications, startups, and ecosystem partnerships.",
		ClassifyHint: "AI startups enterprise AI China Shanghai investors research",
		Actions: sources.ActionSignals{
			CanRegister: boolPtr(true),
			CanExhibit:  boolPtr(true),
			RegisterURL: stringPtr("https://www.worldaic.com.cn/en/"),
			ExhibitURL:  stringPtr("https://www.worldaic.com.cn/en/"),
			CostHint:    stringPtr("unknown"),
		},
	},
	{
		EventID:      "benchmark-world-robot-conference-beijing-2026",
		URL:          "https://www.worldrobotconference.com/en/",
		Name:         "World Robot Conference 2026",
		StartRaw:     "2026-08-19",
		EndRaw:       "2026-08-23",
		Country:      "CN",
		Timezone:     "Asia/Shanghai",
		Format:       "onsite",
		VenueName:    "Beiren Etrong International Exhibition & Convention Center",
		City:         "Beijing",
		Organizer:    "World Robot Conference",
		Summary:      "World Robot Conference 2026 is a Beijing robotics conference and exhibition covering robot learning, intelligent control, human-robot interaction, robot vision, and industrial robotics.",
		ClassifyHint: "robotics humanoid robotics AI automation China Beijing exhibition",
		Actions: sources.ActionSignals{
			CanRegister: boolPtr(true),
			CanExhibit:  boolPtr(true),
			RegisterURL: stringPtr("https://www.worldrobotconference.com/en/forum/registration/"),
			ExhibitURL:  stringPtr("https://www.worldrobotconference.com/en/exhibition/"),
			CostHint:    stringPtr("unknown"),
		},
	},
}
