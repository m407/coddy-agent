package llm

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type tierRoundTripFunc func(*http.Request) (*http.Response, error)

func (f tierRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestServiceTierTransportPreservesRequest(t *testing.T) {
	for _, input := range []string{`{}`, ` { "model": "m", "input": "<hello>" } `, `{"x":1,"x":2}`, `{"service_tier":"old","x":2}`, `{"service\u005ftier":"old"}`, `{"nested":{"x":1,"x":2}}`} {
		t.Run(input, func(t *testing.T) {
			var original map[string]json.RawMessage
			if err := json.Unmarshal([]byte(input), &original); err != nil {
				t.Fatal(err)
			}
			calls := 0
			tr := serviceTierTransport{tier: "priority", base: tierRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Authorization") != "Bearer test" || r.Header.Get("X-Test") != "value" {
					t.Fatal("headers lost")
				}
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				if r.ContentLength != int64(len(data)) {
					t.Fatal("incorrect content length")
				}
				replay, err := r.GetBody()
				if err != nil {
					t.Fatal(err)
				}
				replayBytes, err := io.ReadAll(replay)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(data, replayBytes) {
					t.Fatal("replay differs")
				}
				var got map[string]json.RawMessage
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatal(err)
				}
				tier := `"priority"`
				if calls == 2 {
					tier = `"default"`
				}
				original["service_tier"] = json.RawMessage(tier)
				// Compare decoded values, including objects with repeated nested keys.
				for k, want := range original {
					var a, b any
					if json.Unmarshal(want, &a) != nil || json.Unmarshal(got[k], &b) != nil || !reflect.DeepEqual(a, b) {
						t.Fatalf("%s: got %s want %s", k, got[k], want)
					}
				}
				if len(got) != len(original) {
					t.Fatalf("extra fields: %s", data)
				}
				if calls == 1 {
					return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"service_tier_unavailable"}}`))}, nil
				}
				return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
			})}
			req, err := http.NewRequest("POST", "http://example.test/v1/responses", strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer test")
			req.Header.Set("X-Test", "value")
			if _, err = tr.RoundTrip(req); err != nil || calls != 2 {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}
