package teldrive

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/object"
	"github.com/rclone/rclone/lib/dircache"
	"github.com/rclone/rclone/lib/pacer"
	"github.com/rclone/rclone/lib/rest"
)

func TestUploadMultipart_RetriesChunkOnTransientFailure(t *testing.T) {
	var postCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/api/uploads/") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		call := postCalls.Add(1)
		if call == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	f := &Fs{
		opt: Options{
			ChunkSize:       fs.SizeSuffix(64 << 20),
			ChannelID:       42,
			RandomChunkName: false,
		},
		pacer: fs.NewPacer(context.Background(), pacer.NewDefault()),
		srv:   rest.NewClient(server.Client()).SetRoot(server.URL),
	}
	f.dirCache = dircache.New("", "root-folder", f)
	f.rootFolderID = "root-folder"
	f.userId = 99

	o := &Object{
		fs:     f,
		remote: "file.bin",
	}
	payload := []byte("payload-for-retry-test")
	src := object.NewStaticObjectInfo("file.bin", time.Now(), int64(len(payload)), false, nil, f)

	uploadInfo, err := o.uploadMultipart(context.Background(), bytes.NewReader(payload), src)
	if err != nil {
		t.Fatalf("uploadMultipart failed: %v", err)
	}
	if uploadInfo == nil {
		t.Fatalf("expected upload info, got nil")
	}
	if got := postCalls.Load(); got != 2 {
		t.Fatalf("expected 2 POST attempts (retry), got %d", got)
	}
}

func TestCreateFile_ReturnsMetadataFromResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/files" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-1","name":"file.bin","mimeType":"application/octet-stream","size":12,"parentId":"root-folder","type":"file","updatedAt":"2025-01-01T00:00:00Z","hash":"abc"}`))
	}))
	defer server.Close()

	f := &Fs{
		pacer: fs.NewPacer(context.Background(), pacer.NewDefault()),
		srv:   rest.NewClient(server.Client()).SetRoot(server.URL),
	}

	o := &Object{fs: f}
	src := object.NewStaticObjectInfo("file.bin", time.Now(), 12, false, nil, f)

	info, err := o.createFile(context.Background(), src, &uploadInfo{
		fileName:    "file.bin",
		dir:         "root-folder",
		uploadID:    "upload-1",
		channelID:   42,
		encryptFile: true,
	})
	if err != nil {
		t.Fatalf("createFile failed: %v", err)
	}
	if info == nil {
		t.Fatalf("expected created metadata, got nil")
	}
	if info.Id != "file-1" {
		t.Fatalf("expected id file-1, got %q", info.Id)
	}
	if info.Size != 12 {
		t.Fatalf("expected size 12, got %d", info.Size)
	}
}

func TestPutUnchecked_UsesCreateResponseWithoutLookup(t *testing.T) {
	var postCalls atomic.Int32
	var getCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/files":
			postCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"file-1","name":"empty.txt","mimeType":"text/plain","size":0,"parentId":"root-folder","type":"file","updatedAt":"2025-01-01T00:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/files":
			getCalls.Add(1)
			t.Fatalf("unexpected metadata lookup after create")
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	f := &Fs{
		opt: Options{
			ChunkSize: fs.SizeSuffix(64 << 20),
		},
		pacer: fs.NewPacer(context.Background(), pacer.NewDefault()),
		srv:   rest.NewClient(server.Client()).SetRoot(server.URL),
	}
	f.dirCache = dircache.New("", "root-folder", f)
	f.rootFolderID = "root-folder"

	src := object.NewStaticObjectInfo("empty.txt", time.Now(), 0, false, nil, f)
	obj, err := f.PutUnchecked(context.Background(), bytes.NewReader(nil), src)
	if err != nil {
		t.Fatalf("PutUnchecked failed: %v", err)
	}
	if obj == nil {
		t.Fatalf("expected object, got nil")
	}

	if got := postCalls.Load(); got != 1 {
		t.Fatalf("expected one create request, got %d", got)
	}
	if got := getCalls.Load(); got != 0 {
		t.Fatalf("expected zero lookup requests, got %d", got)
	}
}
