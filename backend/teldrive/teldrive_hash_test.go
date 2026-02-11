package teldrive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/lib/pacer"
	"github.com/rclone/rclone/lib/rest"
)

func newTestObject(t *testing.T, handler http.HandlerFunc) (*Object, func()) {
	t.Helper()
	server := httptest.NewServer(handler)

	o := &Object{
		id: "file-id",
		fs: &Fs{
			pacer: fs.NewPacer(context.Background(), pacer.NewDefault()),
			srv:   rest.NewClient(server.Client()).SetRoot(server.URL),
		},
	}

	return o, server.Close
}

func TestObjectHashUnsupportedType(t *testing.T) {
	o, cleanup := newTestObject(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected API call: %s %s", r.Method, r.URL.Path)
	})
	defer cleanup()

	_, err := o.Hash(context.Background(), hash.MD5)
	if err != hash.ErrUnsupported {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}

func TestObjectHashEmptyFailsWhenMetadataUnavailable(t *testing.T) {
	o, cleanup := newTestObject(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/files/file-id" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-id","hash":""}`))
	})
	defer cleanup()

	_, err := o.Hash(context.Background(), telDriveHash)
	if err == nil {
		t.Fatalf("expected error for unavailable hash metadata")
	}
	if !strings.Contains(err.Error(), "hash metadata unavailable") {
		t.Fatalf("expected unavailable-hash error, got %v", err)
	}
}

func TestObjectHashCachesNonEmptyValue(t *testing.T) {
	var requests atomic.Int32
	o, cleanup := newTestObject(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-id","hash":"abc123"}`))
	})
	defer cleanup()

	sum, err := o.Hash(context.Background(), telDriveHash)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if sum != "abc123" {
		t.Fatalf("expected hash abc123, got %q", sum)
	}

	sum, err = o.Hash(context.Background(), telDriveHash)
	if err != nil {
		t.Fatalf("expected nil error on cached call, got %v", err)
	}
	if sum != "abc123" {
		t.Fatalf("expected cached hash abc123, got %q", sum)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("expected 1 API request, got %d", got)
	}
}
