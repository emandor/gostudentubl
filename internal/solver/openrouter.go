package solver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/emandor/gostudentubl/internal/llm"
)

const solverSystemPrompt = `Kamu adalah asisten akademik yang membantu mahasiswa menyelesaikan tugas atau kuis di perguruan tinggi.
Buat draf solusi yang komprehensif yang bisa digunakan mahasiswa sebagai referensi.

Sertakan:
1. Analisis soal — apa yang diminta
2. Pendekatan langkah-demi-langkah yang jelas
3. Konsep-konsep kunci yang harus dikuasai
4. Contoh jawaban lengkap atau kode jika relevan
5. **WAJIB — dua tabel Markdown berikut (sertakan keduanya):**

   **Tabel A — Forward Propagation per iterasi** (untuk tugas perhitungan/neural network/backpropagation):
   | Iterasi | Layer | Input (X) | Weight (W) | Bias (b) | Net (Z=W·X+b) | Aktivasi f(Z) | Output (Y) |
   |---------|-------|-----------|------------|----------|---------------|---------------|------------|
   | 1       | Hidden | ...      | ...        | ...      | ...           | Sigmoid       | ...        |
   | 1       | Output | ...      | ...        | ...      | ...           | Sigmoid       | ...        |
   | 2       | Hidden | ...      | ...        | ...      | ...           | Sigmoid       | ...        |
   | 2       | Output | ...      | ...        | ...      | ...           | Sigmoid       | ...        |

   **Tabel B — Konvergensi / Update Bobot per iterasi** (untuk backpropagation):
   | Iterasi | W_lama | Learning Rate (α) | Gradient (δ) | ΔW = α×δ×x | W_baru | Loss/Error |
   |---------|--------|-------------------|--------------|------------|--------|------------|
   | 1       | ...    | ...               | ...          | ...        | ...    | ...        |
   | 2       | ...    | ...               | ...          | ...        | ...    | ...        |

   Untuk tugas rangkuman/essay: ganti dengan tabel ringkasan konsep:
   | No | Konsep | Definisi | Rumus/Formula | Contoh Nilai |
   |----|--------|----------|---------------|--------------|

   Jika nilai spesifik tidak tersedia, gunakan nilai contoh realistis (X=0.5, W=0.8, b=0.1, α=0.1) dan beri catatan itu adalah contoh ilustrasi.

6. Tips mengerjakan dan manajemen waktu

Tulis dalam Bahasa Indonesia yang dicampur dengan istilah teknis dalam Bahasa Inggris.
Format output dalam Markdown — HARUS menyertakan tabel iterasi (|col|col|) untuk menunjukkan proses kalkulasi per langkah/iterasi.`

// OpenRouterSolver wraps the existing llm.LLMClient to generate draft solutions.
// It uses its own HTTP client with a generous timeout for long-form generation.
type OpenRouterSolver struct {
	LLM llm.LLMClient
}

// NewOpenRouterSolver creates an OpenRouterSolver with a dedicated HTTP client
// (120s timeout) so long draft generations don't hit the shared short timeout.
func NewOpenRouterSolver(endpoint, apiKey, model string) *OpenRouterSolver {
	hc := &http.Client{Timeout: 120 * time.Second}
	return &OpenRouterSolver{
		LLM: &llm.Client{
			Endpoint: endpoint,
			APIKey:   apiKey,
			Model:    model,
			HC:       hc,
		},
	}
}

func (s *OpenRouterSolver) Solve(ctx context.Context, req SolveRequest) (SolveResponse, error) {
	content := fmt.Sprintf(
		"Bantu saya menyelesaikan %s berikut:\n\nMata Kuliah: %s\nTopik: %s\nItem: %s\nTenggat: %s\n\nKonten detail:\n%s",
		req.EventType,
		req.CourseName,
		req.ItemTitle,
		req.ItemName,
		req.DueDate,
		req.RawContent,
	)

	resp, err := s.LLM.GetSuggestion(ctx, llm.SuggestionRequest{
		EventType:  req.EventType,
		CourseName: req.CourseName,
		ItemName:   req.ItemName,
		ItemTitle:  req.ItemTitle,
		Content:    content,
		MaxTokens:  2500,
	})
	if err != nil {
		return SolveResponse{}, fmt.Errorf("openrouter solve: %w", err)
	}

	return SolveResponse{
		DraftText:  resp.Suggestion,
		Provider:   "openrouter",
		Model:      resp.Model,
		TokensUsed: resp.TokensUsed,
	}, nil
}
