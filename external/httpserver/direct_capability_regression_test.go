//go:build http

package httpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EvilFreelancer/coddy-agent/internal/config"
	"github.com/EvilFreelancer/coddy-agent/internal/llm"
	"github.com/EvilFreelancer/coddy-agent/internal/session"
)

func TestDirectColdTierWithExplicitReasoning(t *testing.T) {
	for _, endpoint := range []string{"/v1/chat/completions", "/v1/responses"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := &config.Config{Providers: []config.ProviderConfig{{Name: "p", Type: "codex"}}, Models: []config.ModelEntry{{Model: "p/test", ReasoningLevels: &[]string{"high"}}}}
			mgr := session.NewManager(cfg, noopSender{}, nil, slog.Default(), t.TempDir(), nil)
			var calls atomic.Int32
			mgr.SetContextWindowLister(func(context.Context, llm.ProviderInput) ([]llm.ModelEntry, error) {
				calls.Add(1)
				tiers := []string{"ultrafast"}
				return []llm.ModelEntry{{ID: "test", ServiceTiers: &tiers}}, nil
			}, nil)
			srv := New(cfg, mgr, slog.Default(), t.TempDir())
			defer srv.Drain()
			srv.makeLLMFromYAML = func(_ *config.Config, _ string, opts llm.RequestOptions) (llm.Provider, error) {
				if opts.ServiceTier != "ultrafast" {
					t.Errorf("tier=%q", opts.ServiceTier)
				}
				return fakeProvider{reply: "ok"}, nil
			}
			body := `{"model":"p/test","service_tier":"ultrafast","messages":[{"role":"user","content":"hi"}]}`
			if endpoint == "/v1/responses" {
				body = `{"model":"p/test","service_tier":"ultrafast","input":"hi"}`
			}
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body)))
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if calls.Load() != 1 {
				t.Fatalf("catalog calls=%d", calls.Load())
			}
		})
	}
}

func TestDirectNoCapabilityParametersDoesNotWait(t *testing.T) {
	for _, endpoint := range []string{"/v1/chat/completions", "/v1/responses"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := &config.Config{Providers: []config.ProviderConfig{{Name: "p", Type: "codex"}}, Models: []config.ModelEntry{{Model: "p/test"}}}
			mgr := session.NewManager(cfg, noopSender{}, nil, slog.Default(), t.TempDir(), nil)
			var calls atomic.Int32
			mgr.SetContextWindowLister(func(context.Context, llm.ProviderInput) ([]llm.ModelEntry, error) {
				calls.Add(1)
				return nil, fmt.Errorf("unexpected discovery")
			}, nil)
			srv := New(cfg, mgr, slog.Default(), t.TempDir())
			defer srv.Drain()
			srv.makeLLMFromYAML = func(_ *config.Config, _ string, _ llm.RequestOptions) (llm.Provider, error) {
				return fakeProvider{reply: "ok"}, nil
			}
			body := `{"model":"p/test","messages":[{"role":"user","content":"hi"}]}`
			if endpoint == "/v1/responses" {
				body = `{"model":"p/test","input":"hi"}`
			}
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body)))
			if err := mgr.WaitContextWindowsIdle(time.Second); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || calls.Load() != 0 {
				t.Fatalf("status=%d catalog calls=%d body=%s", w.Code, calls.Load(), w.Body.String())
			}
		})
	}
}

func TestDirectCapabilityModelDefaults(t *testing.T) {
	for _, tc := range []struct{ name, reasoning, tier string }{
		{name: "reasoning default", reasoning: "xhigh"},
		{name: "tier default", tier: "ultrafast"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Providers: []config.ProviderConfig{{Name: "p", Type: "codex"}}, Models: []config.ModelEntry{{Model: "p/test", ReasoningDefault: tc.reasoning, ServiceTier: tc.tier}}}
			mgr := session.NewManager(cfg, noopSender{}, nil, slog.Default(), t.TempDir(), nil)
			var calls atomic.Int32
			mgr.SetContextWindowLister(func(context.Context, llm.ProviderInput) ([]llm.ModelEntry, error) {
				calls.Add(1)
				levels := []string{"xhigh"}
				return []llm.ModelEntry{{ID: "test", ReasoningLevels: &levels}}, nil
			}, nil)
			srv := New(cfg, mgr, slog.Default(), t.TempDir())
			defer srv.Drain()
			got := srv.directCapabilityConfig(context.Background(), cfg, "p/test", "", "")
			if calls.Load() != 1 {
				t.Fatalf("catalog calls=%d", calls.Load())
			}
			if tc.reasoning != "" && got.DefaultReasoningLevelFor(&got.Models[0]) != "xhigh" {
				t.Fatal("catalog default lost")
			}
		})
	}
}
