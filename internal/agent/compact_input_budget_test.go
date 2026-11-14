package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/EvilFreelancer/coddy-agent/internal/config"
	"github.com/EvilFreelancer/coddy-agent/internal/session"
)

func TestAutoCompactionInputBudgetBoundary(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		window, output, used int
		codex, want          bool
	}{
		{name: "below input threshold", window: 100000, output: 50000, used: 39999},
		{name: "at input threshold", window: 100000, output: 50000, used: 40000, want: true},
		{name: "zero budget below window threshold", window: 100000, output: 100000, used: 1000},
		{name: "zero budget at window threshold", window: 100000, output: 100000, used: 80000, want: true},
		{name: "output exceeds window", window: 100000, output: 120000, used: 1000},
		{name: "fractional threshold is not rounded down", window: 100, output: 1, used: 79},
		{name: "codex working window already excludes output", window: 100000, output: 50000, used: 40000, codex: true},
		{name: "codex at window threshold", window: 100000, output: 50000, used: 80000, codex: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := seededCompactState(t, 3)
			keep := 1
			provider := &compactCannedProvider{t: t, summary: "summary"}
			ag := compactTestAgent(t, st, config.Compaction{KeepRecentTurns: &keep}, provider)
			ag.cfg.Models[0].MaxContextTokens = tc.window
			ag.cfg.Models[0].MaxTokens = tc.output
			if tc.codex {
				ag.cfg.Providers[0].Type = "codex"
			}
			st.SetLastContextBreakdown(&session.ContextBreakdown{EstimatedTotal: tc.used})
			if got := ag.maybeAutoCompact(context.Background()); got != tc.want {
				t.Fatalf("compacted = %v, want %v (calls = %d)", got, tc.want, len(provider.requests))
			}
			if got := summaryRowsOf(st) > 0; got != tc.want {
				t.Fatalf("summary present = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAutoCompactionInputBudgetBoundsInTurnAim(t *testing.T) {
	st := stateOf(t, fixtureTurn("Audit every module.", 8, 1, 3900))
	provider := &compactCannedProvider{t: t, summary: "summary"}
	ag, logs := windowAgent(t, st, config.Compaction{}, provider, 24000, 500, conversationTokens(windowOf(st), false))
	ag.cfg.Models[0].MaxTokens = 12000
	if !ag.maybeAutoCompact(context.Background()) {
		t.Fatalf("expected in-turn compaction:\n%s", logs)
	}
	// Half of 80% of the 12000-token input budget, plus the summary
	// the target deliberately leaves room for, must suffice for the kept tail.
	if got := ag.inTurnRequestTokens(1); got >= 5500 {
		t.Fatalf("kept request = %d, want below 5500:\n%s", got, logs)
	}
}

func TestAutoCompactionInputBudgetSpendsUnhelpfulInTurnFold(t *testing.T) {
	st := inTurnSession(t, "Audit every module.", 6)
	provider := &compactCannedProvider{t: t, summary: "summary"}
	ag, logs := windowAgent(t, st, config.Compaction{}, provider, 32000, 13000, conversationTokens(windowOf(st), false))
	ag.cfg.Models[0].MaxTokens = 16000
	if !ag.maybeAutoCompact(context.Background()) {
		t.Fatalf("expected first fold:\n%s", logs)
	}
	if !ag.inTurnFoldSpent {
		t.Fatalf("overhead exceeds input threshold but fold was not spent:\n%s", logs)
	}
	for i := 0; i < 2; i++ {
		for _, m := range steps(fmt.Sprintf("later%d_", i), 1) {
			st.AddMessage(m)
		}
		if ag.maybeAutoCompact(context.Background()) {
			t.Fatalf("unhelpful fold repeated at step %d:\n%s", i, logs)
		}
	}
	if len(provider.requests) != 1 {
		t.Fatalf("summarizer calls = %d, want 1", len(provider.requests))
	}
}
