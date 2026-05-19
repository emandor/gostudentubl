package solver

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// CodexSolver uses `codex exec` CLI non-interactively to generate draft solutions.
// It captures stdout directly and parses the response section.
type CodexSolver struct{}

func (s *CodexSolver) Solve(ctx context.Context, req SolveRequest) (SolveResponse, error) {
	prompt := buildCopilotPrompt(req) // same prompt structure as CopilotSolver

	// Run via sudo so the codex binary (which requires root) can execute.
	// -E preserves the environment (HOME, etc.) so codex finds its config.
	cmd := exec.CommandContext(ctx, "sudo", "-n", "-E", "codex", "exec",
		"--dangerously-bypass-approvals-and-sandbox",
		prompt,
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return SolveResponse{}, fmt.Errorf("codex exec: %w (stderr: %s)", err, stderr.String())
	}

	// codex writes session info + conversation to stderr; clean response is in stdout only.
	text := stripCodexOutput(stdout.String())
	if strings.TrimSpace(text) == "" {
		return SolveResponse{}, fmt.Errorf("codex: empty response (stderr: %s)", stderr.String())
	}

	return SolveResponse{
		DraftText: text,
		Provider:  "codex",
		Model:     "codex",
	}, nil
}

// codexFooterRe strips trailing tokens/stats lines (in case they appear in stdout).
var codexFooterRe = regexp.MustCompile(`(?mi)^(tokens used|token|changes|requests):?\s*[\d,]*\s*$`)

// stripCodexOutput cleans up codex stdout. When using stdout capture (no --output-last-message),
// stdout contains only the assistant's response; this strips any stray stats lines.
func stripCodexOutput(out string) string {
	cleaned := codexFooterRe.ReplaceAllString(out, "")
	return strings.TrimSpace(cleaned)
}
