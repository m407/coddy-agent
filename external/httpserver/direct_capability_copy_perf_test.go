//go:build http

package httpserver

import (
	"context"
	"fmt"
	"testing"

	"github.com/EvilFreelancer/coddy-agent/internal/config"
	"github.com/EvilFreelancer/coddy-agent/internal/session"
)

func TestDirectCapabilityConfigReusesUnchangedConfig(t *testing.T) {
	levels := []string{"high"}
	for _, tc := range []struct {
		name   string
		levels *[]string
	}{
		{"explicit", &levels},
		{"no catalog", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Models: []config.ModelEntry{{Model: "p/model", ReasoningLevels: tc.levels}}}
			srv := &Server{mgr: &session.Manager{}}
			if got := srv.directCapabilityConfig(context.Background(), cfg, "p/model", "", ""); got != cfg {
				t.Fatal("unchanged capabilities copied the configuration")
			}
		})
	}
}

func BenchmarkDirectCapabilityConfigUnchanged(b *testing.B) {
	levels := []string{"high"}
	for _, n := range []int{1, 1000} {
		b.Run(fmt.Sprintf("models=%d", n), func(b *testing.B) {
			cfg := &config.Config{Models: make([]config.ModelEntry, n)}
			cfg.Models[0] = config.ModelEntry{Model: "p/model", ReasoningLevels: &levels}
			srv := &Server{mgr: &session.Manager{}}
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = srv.directCapabilityConfig(ctx, cfg, "p/model", "", "")
			}
		})
	}
}
