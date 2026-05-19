package solver

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// CopilotSolver uses `gh copilot` CLI non-interactively to generate draft solutions.
type CopilotSolver struct {
	// TmuxSession is the tmux session name used for monitoring (created if absent).
	TmuxSession string
}

func (s *CopilotSolver) Solve(ctx context.Context, req SolveRequest) (SolveResponse, error) {
	prompt := buildCopilotPrompt(req)

	// Ensure tmux session exists (for external monitoring, not required for exec).
	session := s.TmuxSession
	if session == "" {
		session = "copilot-solver"
	}
	if err := exec.Command("tmux", "has-session", "-t", session).Run(); err != nil {
		_ = exec.Command("tmux", "new-session", "-d", "-s", session).Run()
	}

	cmd := exec.CommandContext(ctx, "copilot", "-p", prompt, "--allow-all-tools")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return SolveResponse{}, fmt.Errorf("gh copilot solve: %w (stderr: %s)", err, stderr.String())
	}

	text := stripCopilotFooter(string(out))
	return SolveResponse{
		DraftText: text,
		Provider:  "copilot",
		Model:     "copilot",
	}, nil
}

func buildCopilotPrompt(req SolveRequest) string {
	return fmt.Sprintf(
		`Kamu adalah asisten akademik yang membantu mahasiswa menyelesaikan tugas atau kuis di perguruan tinggi.
Buat draf solusi yang komprehensif yang bisa digunakan mahasiswa sebagai referensi.

Sertakan:
1. Analisis soal — apa yang diminta
2. Pendekatan langkah-demi-langkah yang jelas
3. Konsep-konsep kunci yang harus dikuasai
4. Contoh jawaban lengkap atau kode jika relevan
5. WAJIB — dua tabel Markdown berikut:

   Tabel A — Forward Propagation per iterasi (untuk tugas perhitungan/neural network/backpropagation):
   | Iterasi | Layer | Input (X) | Weight (W) | Bias (b) | Net (Z=W·X+b) | Aktivasi f(Z) | Output (Y) |
   |---------|-------|-----------|------------|----------|---------------|---------------|------------|
   | 1       | Hidden | ...      | ...        | ...      | ...           | Sigmoid       | ...        |
   | 2       | Hidden | ...      | ...        | ...      | ...           | Sigmoid       | ...        |

   Tabel B — Update Bobot per iterasi (untuk backpropagation):
   | Iterasi | W_lama | Learning Rate (α) | Gradient (δ) | ΔW | W_baru | Loss |
   |---------|--------|-------------------|--------------|-----|--------|------|

   Untuk tugas rangkuman/essay:
   | No | Konsep | Definisi | Rumus/Formula | Contoh Nilai |
   |----|--------|----------|---------------|--------------|

   Jika nilai tidak tersedia, gunakan nilai contoh realistis dan beri catatan.

6. Tips mengerjakan dan manajemen waktu

Tulis dalam Bahasa Indonesia + istilah teknis Bahasa Inggris.
HARUS menyertakan tabel iterasi Markdown (|col|col|) untuk menunjukkan proses kalkulasi per iterasi.

Bantu saya menyelesaikan %s berikut:

Mata Kuliah: %s
Topik: %s
Item: %s
Tenggat: %s

Konten detail:
%s`,
		req.EventType,
		req.CourseName,
		req.ItemTitle,
		req.ItemName,
		req.DueDate,
		req.RawContent,
	)
}

var copilotFooterRe = regexp.MustCompile(`(?m)^(Changes|Requests|Tokens):\s*\d+.*$`)

func stripCopilotFooter(out string) string {
	cleaned := copilotFooterRe.ReplaceAllString(out, "")
	return strings.TrimSpace(cleaned)
}
