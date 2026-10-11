package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetReportsProgressAndStopsWhenStalled(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "3145728") // 3 MB promised
		_, _ = w.Write(make([]byte, 1<<20))         // 1 MB sent, then nothing
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	var last int64
	_, err := get(context.Background(), srv.URL, 100*time.Millisecond, func(done, total int64) {
		if total != 3<<20 {
			t.Errorf("total = %d", total)
		}
		last = done
	})
	if err == nil || err.Error() != "no data from GitHub for 100ms (1.0/3.0 MB downloaded)" {
		t.Fatalf("err = %v", err)
	}
	if last != 1<<20 {
		t.Fatalf("progress saw %d bytes", last)
	}
}

func TestGetSlowButSteadyFinishes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for range 5 { // 250ms in all, never 100ms without data
			_, _ = w.Write([]byte("chunk"))
			w.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer srv.Close()
	b, err := get(context.Background(), srv.URL, 100*time.Millisecond, nil)
	if err != nil || strings.Count(string(b), "chunk") != 5 {
		t.Fatalf("got %q, %v", b, err)
	}
}

func TestStepString(t *testing.T) {
	for _, c := range []struct {
		s    Step
		want string
	}{
		{Step{Done: 3250585, Total: 8808038}, "downloading 3.1/8.4 MB"},
		{Step{Done: 1 << 20}, "downloading 1.0 MB"},
		{Step{Verifying: true}, "checking checksum"},
	} {
		if got := c.s.String(); got != c.want {
			t.Errorf("%+v = %q, want %q", c.s, got, c.want)
		}
	}
}
