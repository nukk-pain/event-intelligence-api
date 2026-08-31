package api_test

import (
	"testing"

	"github.com/smpain/event-intelligence-api/internal/model"
)

func TestListEvents_ListKindFilter(t *testing.T) {
	venueEvent := seedEvent("ev-coex", "coex", "ai", false)
	// A domestic event at a venue outside COEX/KINTEX (e.g. an AKEI-listed
	// regional fair) belongs in the 국내 list even without a known venue_id.
	regionalEvent := seedEvent("akei-104742", "", "ai", false)
	regionalEvent.Venue = &model.Venue{Name: "한라체육관", City: "제주"}
	overseasAggregatorEvent := seedEvent("akei-104896", "", "ai", false)
	overseasAggregatorEvent.Venue = &model.Venue{Name: "Magic Box, LA, USA", City: "Los Angeles"}
	overseasAggregatorEvent.Country = "US"
	benchmarkEvent := seedEvent("benchmark-vivatech", "", "ai", false)
	benchmarkEvent.Venue = &model.Venue{Name: "Paris Expo Porte de Versailles", City: "Paris"}
	benchmarkEvent.Country = "FR"
	benchmarkEvent.Sources = []model.Source{
		{URL: "https://vivatech.com/", Type: "organizer", Publisher: "VivaTech", RetrievedAt: "2026-06-23T00:00:00Z"},
	}
	koreanBenchmarkEvent := seedEvent("benchmark-icra-2027", "", "humanoid-robotics", false)
	koreanBenchmarkEvent.Venue = &model.Venue{Name: "COEX", City: "Seoul"}
	unknownCountryEvent := seedEvent("benchmark-tba", "", "ai", false)
	unknownCountryEvent.Country = "ZZ"
	srv := newServer(t, []model.Event{venueEvent, regionalEvent, overseasAggregatorEvent, benchmarkEvent, koreanBenchmarkEvent, unknownCountryEvent})

	venue, _ := getEnvelope(t, srv.URL+"/api/v1/events?list=venue&limit=100")
	if len(venue.Data) != 3 {
		t.Fatalf("list=venue returned %+v, want the three KR-hosted events", eventIDs(venue.Data))
	}
	for _, e := range venue.Data {
		if e.Country != "KR" {
			t.Fatalf("list=venue must contain only KR-hosted rows: %+v", eventIDs(venue.Data))
		}
	}

	benchmark, _ := getEnvelope(t, srv.URL+"/api/v1/events?list=benchmark&limit=100")
	if len(benchmark.Data) != 2 {
		t.Fatalf("list=benchmark returned %+v, want the two known overseas events", eventIDs(benchmark.Data))
	}
	for _, e := range benchmark.Data {
		if e.Country == "KR" || e.Country == "ZZ" || e.Country == "" {
			t.Fatalf("list=benchmark must contain only known non-KR rows: %+v", eventIDs(benchmark.Data))
		}
	}

	all, _ := getEnvelope(t, srv.URL+"/api/v1/events?list=all&limit=100")
	if len(all.Data) != 6 {
		t.Fatalf("list=all returned %d events, want 6", len(all.Data))
	}
}

func eventIDs(events []model.Event) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.EventID)
	}
	return ids
}
