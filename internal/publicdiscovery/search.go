package publicdiscovery

import (
	"context"
	"sort"
	"strings"
)

func (provider *Provider) Search(ctx context.Context, query string) (Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Result{}, ErrQueryRequired
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if !provider.crawled {
		provider.immutableCandidates, provider.immutableBudget = provider.crawl(ctx)
		provider.crawled = true
	}
	return Result{
		Candidates: rankCandidates(provider.immutableCandidates, query),
		Budget:     cloneBudget(provider.immutableBudget),
	}, nil
}

type scoredCandidate struct {
	candidate Candidate
	score     int
	order     int
}

func rankCandidates(candidates []Candidate, query string) []Candidate {
	tokens := strings.Fields(strings.ToLower(query))
	scored := make([]scoredCandidate, 0, len(candidates))
	for index, candidate := range candidates {
		title := strings.ToLower(candidate.Title)
		candidateURL := strings.ToLower(candidate.URL)
		score := 0
		for _, token := range tokens {
			if strings.Contains(title, token) {
				score += 3
			}
			if strings.Contains(candidateURL, token) {
				score++
			}
		}
		scored = append(scored, scoredCandidate{candidate: candidate, score: score, order: index})
	}
	sort.SliceStable(scored, func(left, right int) bool {
		if scored[left].score == scored[right].score {
			return scored[left].order < scored[right].order
		}
		return scored[left].score > scored[right].score
	})
	result := make([]Candidate, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.candidate)
	}
	return result
}

func cloneBudget(budget BudgetState) BudgetState {
	budget.TruncationReasons = append([]TruncationReason(nil), budget.TruncationReasons...)
	return budget
}
