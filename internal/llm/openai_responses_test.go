package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIResponsesModelRouting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n")
	}))
	defer srv.Close()
	p, err := NewProvider(ProviderInput{Type: "openai", Model: "gpt-6-astra", BaseURL: srv.URL, RetryDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Stream(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil, func(StreamChunk) {})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIProtocolOverride(t *testing.T) {
	for _, protocol := range []string{"chat", "responses"} {
		t.Run(protocol, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				want := "/responses"
				if protocol == "chat" {
					want = "/chat/completions"
				}
				if r.URL.Path != want {
					t.Errorf("path=%s, want %s", r.URL.Path, want)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if protocol == "chat" {
					_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				} else {
					_, _ = fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n")
				}
			}))
			defer srv.Close()
			p, err := NewProvider(ProviderInput{Type: "openai", Model: "gpt-6-astra", Protocol: protocol, BaseURL: srv.URL, RetryDisabled: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = p.Stream(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil, func(StreamChunk) {}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenAIResponsesBlocking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["stream"] == true {
			t.Error("blocking request opened a stream")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"r1","status":"completed","service_tier":"default","output":[{"id":"m1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[]}]}],"usage":{"input_tokens":2,"output_tokens":1}}`)
	}))
	defer srv.Close()
	p, err := NewProvider(ProviderInput{Type: "openai", Model: "gpt-6-astra", BaseURL: srv.URL, DisableStream: true, ServiceTier: "default", RetryDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Content != "done" || r.RequestedServiceTier != "default" || r.ActualServiceTier != "default" {
		t.Fatalf("response=%+v", r)
	}
}

func TestOpenAIResponsesBlockingUsagePresence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage string
		known bool
		input int
	}{
		{"missing", "", false, 0},
		{"null", `,"usage":null`, false, 0},
		{"partial", `,"usage":{"input_tokens":7}`, false, 7},
		{"zero", `,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}`, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"id":"r1","status":"completed","output":[]%s}`, tc.usage)
			}))
			defer srv.Close()
			p, err := NewProvider(ProviderInput{Type: "openai", Model: "test", Protocol: "responses", BaseURL: srv.URL, RetryDisabled: true, DisableStream: true})
			if err != nil {
				t.Fatal(err)
			}
			out, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if out.UsageKnown != tc.known || out.UsageIncomplete == tc.known || out.InputTokens != tc.input || out.OutputTokens != 0 {
				t.Fatalf("response=%+v, want UsageKnown=%v, input=%d and zero output", out, tc.known, tc.input)
			}
		})
	}
}

func TestOpenAIResponsesBlockingContentFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"r1","status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}]}`)
	}))
	defer srv.Close()
	p, err := NewProvider(ProviderInput{Type: "openai", Model: "test", Protocol: "responses", BaseURL: srv.URL, RetryDisabled: true, DisableStream: true})
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "partial" || out.StopReason != "content_filter" {
		t.Fatalf("response=%+v", out)
	}
}
