package fetch

import (
	"context"
	"errors"
	"testing"
)

func TestStrictPublicCrawlRetainsLoopbackAndMetadataBlocks(t *testing.T) {
	tests := []string{
		"http://127.0.0.1:1/private",
		"http://169.254.169.254/latest/meta-data/",
	}
	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			// Given
			budget := NewPublicCrawlBudget()
			f, err := NewFetcher(
				WithStrictPublicCrawl(budget),
				WithAnyPublicHost(true),
				WithMaxRetries(0),
			)
			if err != nil {
				t.Fatalf("NewFetcher: %v", err)
			}

			// When
			_, err = f.Fetch(context.Background(), rawURL, Conditional{})

			// Then
			if !errors.Is(err, ErrBlockedAddress) {
				t.Fatalf("error = %v, want ErrBlockedAddress", err)
			}
			if usage := budget.Usage(); usage.TransportAttempts != 1 {
				t.Fatalf("transport attempts = %d, want 1 blocked attempt", usage.TransportAttempts)
			}
		})
	}
}
