package api_test

import (
	"testing"

	"github.com/smpain/event-intelligence-api/internal/model"
)

func TestListEvents_ListKindFilter(t *testing.T) {
	venueEvent := seedEvent("ev-coex", "coex", "ai", false)
	benchmarkEvent := seedEvent("benchmark-vivatech", "", "ai", false)
	benchmarkEvent.Venue = &model.Venue{Name: "Paris Expo Porte de Versailles", City: "Paris"}
	benchmarkEvent.Country = "FR"
	benchmarkEvent.Sources = []model.Source{
		{URL: "https://vivatech.com/", Type: "organizer", Publisher: "VivaTech", RetrievedAt: "2026-06-23T00:00:00Z"},
	}
	srv := newServer(t, []model.Event{venueEvent, benchmarkEvent})

	venue, _ := getEnvelope(t, srv.URL+"/api/v1/events?list=venue&limit=100")
	if len(venue.Data) != 1 || venue.Data[0].EventID != "ev-coex" {
		t.Fatalf("list=venue returned %+v, want only ev-coex", eventIDs(venue.Data))
	}

	benchmark, _ := getEnvelope(t, srv.URL+"/api/v1/events?list=benchmark&limit=100")
	if len(benchmark.Data) != 1 || benchmark.Data[0].EventID != "benchmark-vivatech" {
		t.Fatalf("list=benchmark returned %+v, want only benchmark-vivatech", eventIDs(benchmark.Data))
	}

	all, _ := getEnvelope(t, srv.URL+"/api/v1/events?list=all&limit=100")
	if len(all.Data) != 2 {
		t.Fatalf("list=all returned %d events, want 2", len(all.Data))
	}
}

func eventIDs(events []model.Event) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.EventID)
	}
	return ids
}
