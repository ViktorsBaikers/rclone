package teldrive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/pacer"
	"github.com/rclone/rclone/lib/rest"
)

func TestChangeNotify_StopsSSEWhenPollChannelClosed(t *testing.T) {
	connected := make(chan struct{}, 1)
	disconnected := make(chan struct{}, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/events/stream" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		select {
		case connected <- struct{}{}:
		default:
		}

		<-r.Context().Done()

		select {
		case disconnected <- struct{}{}:
		default:
		}
	}))
	defer server.Close()

	f := &Fs{
		srv:      rest.NewClient(server.Client()).SetRoot(server.URL),
		ssePacer: fs.NewPacer(context.Background(), pacer.NewDefault()),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pollIntervalChan := make(chan time.Duration)
	f.ChangeNotify(ctx, func(string, fs.EntryType) {}, pollIntervalChan)

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE connection")
	}

	close(pollIntervalChan)

	select {
	case <-disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE connection to close after poll channel close")
	}
}
