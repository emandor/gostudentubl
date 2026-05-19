package solver

import (
	"context"
	"sync"
	"time"
)

// SolveRequest holds all input needed to generate a draft solution.
type SolveRequest struct {
	EventType  string // "assignment" or "quiz"
	CourseName string
	ItemTitle  string
	ItemName   string
	DueDate    string
	RawContent string // from item_details.raw_content
}

// SolveResponse is the result of a single-provider draft generation call.
type SolveResponse struct {
	DraftText  string
	Provider   string
	Model      string
	TokensUsed int
}

// ProviderResult holds the outcome (success or failure) of one provider's attempt.
type ProviderResult struct {
	Provider   string
	Model      string
	DraftText  string
	TokensUsed int
	Error      error
	Attempts   int // total attempts made (including retries)
}

// Solver is the interface for AI-powered draft generation.
type Solver interface {
	Solve(ctx context.Context, req SolveRequest) (SolveResponse, error)
}

// namedProvider pairs a display name with a Solver implementation.
type namedProvider struct {
	Name   string
	Solver Solver
}

// MultiSolver runs multiple solvers concurrently, each with retry logic.
// All results (success and failure) are collected and returned.
type MultiSolver struct {
	Providers []namedProvider
	MaxRetry  int // max attempts per provider (default 2)
}

// SolveAll runs every configured provider concurrently and returns all results.
// It never returns an error — failures are captured in ProviderResult.Error.
func (ms *MultiSolver) SolveAll(ctx context.Context, req SolveRequest) []ProviderResult {
	maxRetry := ms.MaxRetry
	if maxRetry <= 0 {
		maxRetry = 2
	}

	results := make([]ProviderResult, len(ms.Providers))
	var wg sync.WaitGroup

	for i, p := range ms.Providers {
		wg.Add(1)
		go func(idx int, np namedProvider) {
			defer wg.Done()
			results[idx] = runWithRetry(ctx, np.Name, np.Solver, req, maxRetry)
		}(i, p)
	}

	wg.Wait()
	return results
}

func runWithRetry(ctx context.Context, name string, s Solver, req SolveRequest, maxRetry int) ProviderResult {
	var lastErr error
	for attempt := 1; attempt <= maxRetry; attempt++ {
		resp, err := s.Solve(ctx, req)
		if err == nil {
			return ProviderResult{
				Provider:   resp.Provider,
				Model:      resp.Model,
				DraftText:  resp.DraftText,
				TokensUsed: resp.TokensUsed,
				Attempts:   attempt,
			}
		}
		lastErr = err
		if attempt < maxRetry {
			backoff := time.Duration(attempt) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return ProviderResult{Provider: name, Error: ctx.Err(), Attempts: attempt}
			case <-time.After(backoff):
			}
		}
	}
	return ProviderResult{Provider: name, Error: lastErr, Attempts: maxRetry}
}
