package llm

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type tierBenchmarkTransport struct{}

func (tierBenchmarkTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	_, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
}

func BenchmarkServiceTierTransport(b *testing.B) {
	body := `{"model":"test","input":"` + strings.Repeat("a", 1<<20) + `"}`
	for _, tier := range []string{"default", "priority"} {
		b.Run(tier, func(b *testing.B) {
			transport := &serviceTierTransport{base: tierBenchmarkTransport{}, tier: tier}
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				req, err := http.NewRequest("POST", "http://example.test/v1/responses", strings.NewReader(body))
				if err != nil {
					b.Fatal(err)
				}
				if _, err = transport.RoundTrip(req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
