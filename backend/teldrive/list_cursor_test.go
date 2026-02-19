package teldrive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/dircache"
	"github.com/rclone/rclone/lib/pacer"
	"github.com/rclone/rclone/lib/rest"
)

func TestList_UsesCursorPaginationUntilShortPage(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/files" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		q := r.URL.Query()
		if q.Get("operation") != "list" {
			t.Fatalf("expected operation=list, got %q", q.Get("operation"))
		}
		if q.Get("parentId") != "root-folder" {
			t.Fatalf("expected parentId=root-folder, got %q", q.Get("parentId"))
		}
		if q.Get("limit") != "2" {
			t.Fatalf("expected limit=2, got %q", q.Get("limit"))
		}
		if q.Get("sort") != "id" {
			t.Fatalf("expected sort=id, got %q", q.Get("sort"))
		}
		if q.Get("order") != "asc" {
			t.Fatalf("expected order=asc, got %q", q.Get("order"))
		}
		if _, ok := q["cursor"]; !ok {
			t.Fatalf("expected cursor query param to be present")
		}

		call := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")

		switch call {
		case 1:
			if q.Get("cursor") != "" {
				t.Fatalf("expected empty cursor on first request, got %q", q.Get("cursor"))
			}
			if q.Get("page") != "1" {
				t.Fatalf("expected page=1 on first request, got %q", q.Get("page"))
			}
			_, _ = w.Write([]byte(`{
				"items": [
					{"id":"1","name":"a.txt","mimeType":"text/plain","size":1,"parentId":"root-folder","type":"file","updatedAt":"2026-01-01T00:00:00Z","hash":"h1"},
					{"id":"2","name":"b.txt","mimeType":"text/plain","size":1,"parentId":"root-folder","type":"file","updatedAt":"2026-01-01T00:00:00Z","hash":"h2"}
				],
				"meta": {"count": 999, "totalPages": 999, "currentPage": 1}
			}`))
		case 2:
			if q.Get("cursor") != "2" {
				t.Fatalf("expected cursor=2 on second request, got %q", q.Get("cursor"))
			}
			if q.Get("page") != "2" {
				t.Fatalf("expected page=2 on second request, got %q", q.Get("page"))
			}
			_, _ = w.Write([]byte(`{
				"items": [
					{"id":"3","name":"c.txt","mimeType":"text/plain","size":1,"parentId":"root-folder","type":"file","updatedAt":"2026-01-01T00:00:00Z","hash":"h3"}
				],
				"meta": {"count": 999, "totalPages": 999, "currentPage": 2}
			}`))
		default:
			t.Fatalf("unexpected extra list request %d", call)
		}
	}))
	defer server.Close()

	f := &Fs{
		opt: Options{
			PageSize: 2,
		},
		pacer: fs.NewPacer(context.Background(), pacer.NewDefault()),
		srv:   rest.NewClient(server.Client()).SetRoot(server.URL),
	}
	f.rootFolderID = "root-folder"
	f.dirCache = dircache.New("", f.rootFolderID, f)

	entries, err := f.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if got := len(entries); got != 3 {
		t.Fatalf("expected 3 entries, got %d", got)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 list requests, got %d", got)
	}
}
