package solver

import (
	"context"
	"fmt"
	"strings"
)

// BothSolver runs both OpenRouter and Copilot, returning the longer response.
type BothSolver struct {
	Primary  *OpenRouterSolver
	Fallback *CopilotSolver
}

func (s *BothSolver) Solve(ctx context.Context, req SolveRequest) (SolveResponse, error) {
	primary, primaryErr := s.Primary.Solve(ctx, req)
	fallback, fallbackErr := s.Fallback.Solve(ctx, req)

	if primaryErr != nil && fallbackErr != nil {
		return SolveResponse{}, fmt.Errorf("both solvers failed — primary: %v; fallback: %v", primaryErr, fallbackErr)
	}
	if primaryErr != nil {
		return fallback, nil
	}
	if fallbackErr != nil {
		return primary, nil
	}
	// Pick the longer response.
	if len(fallback.DraftText) > len(primary.DraftText) {
		return fallback, nil
	}
	return primary, nil
}

// FallbackSolver tries Primary first; on error falls back to Fallback.
type FallbackSolver struct {
	Primary  Solver
	Fallback Solver
}

func (s *FallbackSolver) Solve(ctx context.Context, req SolveRequest) (SolveResponse, error) {
	resp, err := s.Primary.Solve(ctx, req)
	if err == nil {
		return resp, nil
	}
	fallbackResp, fallbackErr := s.Fallback.Solve(ctx, req)
	if fallbackErr != nil {
		return SolveResponse{}, fmt.Errorf("primary: %w; fallback: %v", err, fallbackErr)
	}
	return fallbackResp, nil
}

// New constructs a Solver based on the mode string (legacy, kept for backward compat).
func New(mode, endpoint, apiKey, model string, tmuxSession string) Solver {
	or := NewOpenRouterSolver(endpoint, apiKey, model)
	cop := &CopilotSolver{TmuxSession: tmuxSession}

	switch mode {
	case "openrouter":
		return or
	case "copilot":
		return cop
	case "both":
		return &BothSolver{Primary: or, Fallback: cop}
	default: // "openrouter-with-copilot-fallback"
		return &FallbackSolver{Primary: or, Fallback: cop}
	}
}

// NewMulti constructs a MultiSolver from a comma-separated provider list.
// providers: "copilot,openrouter,codex" (order determines iteration; results are always concurrent).
func NewMulti(providers, endpoint, apiKey, model, tmuxSession string, maxRetry int) *MultiSolver {
	or := NewOpenRouterSolver(endpoint, apiKey, model)
	cop := &CopilotSolver{TmuxSession: tmuxSession}
	cdx := &CodexSolver{}

	if maxRetry <= 0 {
		maxRetry = 2
	}

	ms := &MultiSolver{MaxRetry: maxRetry}
	for _, p := range strings.Split(providers, ",") {
		p = strings.TrimSpace(p)
		switch p {
		case "copilot":
			ms.Providers = append(ms.Providers, namedProvider{Name: "copilot", Solver: cop})
		case "openrouter":
			ms.Providers = append(ms.Providers, namedProvider{Name: "openrouter", Solver: or})
		case "codex":
			ms.Providers = append(ms.Providers, namedProvider{Name: "codex", Solver: cdx})
		}
	}
	return ms
}

// PickBest returns the successful ProviderResult with the longest DraftText.
// Returns nil if all failed.
func PickBest(results []ProviderResult) *ProviderResult {
	var best *ProviderResult
	for i := range results {
		r := &results[i]
		if r.Error != nil || strings.TrimSpace(r.DraftText) == "" {
			continue
		}
		if best == nil || len(r.DraftText) > len(best.DraftText) {
			best = r
		}
	}
	return best
}
