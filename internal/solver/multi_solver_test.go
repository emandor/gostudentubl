package solver

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// mockSolver is a test double that succeeds after `failN` attempts.
type mockSolver struct {
	provider string
	failN    int32 // fail first N calls, then succeed
	calls    atomic.Int32
	resp     SolveResponse
}

func (m *mockSolver) Solve(_ context.Context, _ SolveRequest) (SolveResponse, error) {
	n := m.calls.Add(1)
	if int32(n) <= m.failN {
		return SolveResponse{}, errors.New("mock error")
	}
	return m.resp, nil
}

func TestMultiSolver_AllSucceed(t *testing.T) {
	ms := &MultiSolver{
		MaxRetry: 2,
		Providers: []namedProvider{
			{Name: "p1", Solver: &mockSolver{provider: "p1", resp: SolveResponse{DraftText: "aaa", Provider: "p1"}}},
			{Name: "p2", Solver: &mockSolver{provider: "p2", resp: SolveResponse{DraftText: "bbb", Provider: "p2"}}},
		},
	}
	results := ms.SolveAll(context.Background(), SolveRequest{})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Error != nil {
			t.Errorf("provider %s: unexpected error: %v", r.Provider, r.Error)
		}
	}
}

func TestMultiSolver_RetryAndSucceed(t *testing.T) {
	ms := &MultiSolver{
		MaxRetry: 3,
		Providers: []namedProvider{
			{Name: "flaky", Solver: &mockSolver{failN: 2, resp: SolveResponse{DraftText: "success", Provider: "flaky"}}},
		},
	}
	results := ms.SolveAll(context.Background(), SolveRequest{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Error != nil {
		t.Errorf("expected success after retries, got error: %v", r.Error)
	}
	if r.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", r.Attempts)
	}
}

func TestMultiSolver_AllFail(t *testing.T) {
	ms := &MultiSolver{
		MaxRetry: 2,
		Providers: []namedProvider{
			{Name: "bad", Solver: &mockSolver{failN: 999, resp: SolveResponse{}}},
		},
	}
	results := ms.SolveAll(context.Background(), SolveRequest{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error == nil {
		t.Error("expected error for always-failing provider")
	}
	if results[0].Attempts != 2 {
		t.Errorf("expected 2 attempts (maxRetry), got %d", results[0].Attempts)
	}
}

func TestPickBest_SelectsLongest(t *testing.T) {
	results := []ProviderResult{
		{Provider: "a", DraftText: "short"},
		{Provider: "b", DraftText: "this is much longer text"},
		{Provider: "c", Error: errors.New("fail")},
	}
	best := PickBest(results)
	if best == nil {
		t.Fatal("expected a best result")
	}
	if best.Provider != "b" {
		t.Errorf("expected provider b, got %s", best.Provider)
	}
}

func TestPickBest_AllFailed(t *testing.T) {
	results := []ProviderResult{
		{Provider: "a", Error: errors.New("fail")},
	}
	best := PickBest(results)
	if best != nil {
		t.Error("expected nil when all failed")
	}
}

func TestMultiSolver_DefaultMaxRetry(t *testing.T) {
	var calls atomic.Int32
	s := &mockSolver{
		failN: 999,
		resp:  SolveResponse{},
	}
	_ = s
	ms := &MultiSolver{
		MaxRetry: 0, // should default to 2
		Providers: []namedProvider{
			{Name: "x", Solver: &mockSolver{failN: 999, resp: SolveResponse{}}},
		},
	}
	results := ms.SolveAll(context.Background(), SolveRequest{})
	_ = calls
	if results[0].Attempts != 2 {
		t.Errorf("expected default 2 attempts, got %d", results[0].Attempts)
	}
}

func TestNewMulti_ParsesProviders(t *testing.T) {
	ms := NewMulti("copilot,codex", "http://example.com", "key", "model", "session", 3)
	if len(ms.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(ms.Providers))
	}
	if ms.Providers[0].Name != "copilot" {
		t.Errorf("expected first provider copilot, got %s", ms.Providers[0].Name)
	}
	if ms.Providers[1].Name != "codex" {
		t.Errorf("expected second provider codex, got %s", ms.Providers[1].Name)
	}
	if ms.MaxRetry != 3 {
		t.Errorf("expected MaxRetry=3, got %d", ms.MaxRetry)
	}
}

func TestStripCodexOutput_RemovesFooter(t *testing.T) {
	input := "This is the draft answer.\n\ntokens used: 1234\n"
	got := stripCodexOutput(input)
	if strings.Contains(got, "tokens used") {
		t.Errorf("footer not stripped: %q", got)
	}
	if !strings.Contains(got, "This is the draft answer.") {
		t.Errorf("content removed unexpectedly: %q", got)
	}
}

func TestStripCodexOutput_EmptyInput(t *testing.T) {
	got := stripCodexOutput("")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
