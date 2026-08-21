package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func client(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := New("somaz94/agent-fanout", "t0ken")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.BaseURL = srv.URL
	c.HTTP = srv.Client()
	return c
}

func TestNewRejectsAMalformedRepository(t *testing.T) {
	for _, in := range []string{"", "no-slash", "/name", "owner/"} {
		if _, err := New(in, "t"); err == nil {
			t.Errorf("New(%q) accepted a malformed repository", in)
		}
	}
}

// An empty token is refused up front. Left to the API it comes back as a 404 on
// a private repo, which reads as "the issue does not exist".
func TestNewRejectsAnEmptyToken(t *testing.T) {
	if _, err := New("a/b", "  "); err == nil {
		t.Fatal("New accepted an empty token")
	}
}

// A second run must UPDATE the comment it left, not append another table.
func TestUpsertUpdatesItsOwnPreviousComment(t *testing.T) {
	const marker = "<!-- somaz94/agent-fanout:issue-42 -->"
	var method, path string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]comment{
				{ID: 1, Body: "unrelated chatter"},
				{ID: 99, Body: marker + "\n\nold table"},
			})
			return
		}
		method, path = r.Method, r.URL.Path
		json.NewEncoder(w).Encode(map[string]string{"html_url": "https://example.test/c/99"})
	}))
	defer srv.Close()

	url, updated, err := client(t, srv).UpsertComment(context.Background(), 42, marker, "new table")
	if err != nil {
		t.Fatalf("UpsertComment: %v", err)
	}
	if !updated {
		t.Error("reported a new comment; it should have updated the existing one")
	}
	if method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", method)
	}
	if !strings.HasSuffix(path, "/issues/comments/99") {
		t.Errorf("path = %s, want the existing comment id", path)
	}
	if url == "" {
		t.Error("no comment url returned")
	}
}

// With no prior comment it posts a new one.
func TestUpsertPostsWhenNoPriorCommentExists(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]comment{{ID: 1, Body: "someone else's comment"}})
			return
		}
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"html_url": "https://example.test/c/1"})
	}))
	defer srv.Close()

	_, updated, err := client(t, srv).UpsertComment(context.Background(), 42, "<!-- m -->", "body")
	if err != nil {
		t.Fatalf("UpsertComment: %v", err)
	}
	if updated {
		t.Error("reported an update with no prior comment")
	}
	if method != http.MethodPost || !strings.HasSuffix(path, "/issues/42/comments") {
		t.Errorf("method/path = %s %s, want POST to the issue's comment list", method, path)
	}
}

// An issue with more than one page of comments is ordinary. Reading only the
// first page would miss the marker and append a duplicate table every run.
func TestUpsertScansBeyondTheFirstPageOfComments(t *testing.T) {
	const marker = "<!-- m -->"
	var patched bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			patched = r.Method == http.MethodPatch
			json.NewEncoder(w).Encode(map[string]string{})
			return
		}
		// Page 1 is full and carries no marker; the marker is on page 2.
		if r.URL.Query().Get("page") == "1" {
			full := make([]comment, perPage)
			for i := range full {
				full[i] = comment{ID: int64(i + 1), Body: "chatter"}
			}
			json.NewEncoder(w).Encode(full)
			return
		}
		json.NewEncoder(w).Encode([]comment{{ID: 777, Body: marker}})
	}))
	defer srv.Close()

	_, updated, err := client(t, srv).UpsertComment(context.Background(), 1, marker, "body")
	if err != nil {
		t.Fatalf("UpsertComment: %v", err)
	}
	if !updated || !patched {
		t.Fatal("marker on page 2 was not found; the scan stopped at page 1")
	}
}

// A non-2xx carries its body into the error. "422" alone does not say which
// field GitHub rejected.
func TestAPIErrorCarriesTheResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]comment{})
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"message":"Body is too long (maximum is 65536 characters)"}`)
	}))
	defer srv.Close()

	_, _, err := client(t, srv).UpsertComment(context.Background(), 1, "<!-- m -->", "body")
	if err == nil {
		t.Fatal("a 422 was reported as success")
	}
	if !strings.Contains(err.Error(), "Body is too long") {
		t.Fatalf("error does not carry the API message: %v", err)
	}
}

// A failed LIST must not be silently swallowed into "post a new one" — that
// would append a duplicate on every transient failure.
func TestListFailureIsReportedNotSwallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"Resource not accessible by integration"}`)
	}))
	defer srv.Close()

	if _, _, err := client(t, srv).UpsertComment(context.Background(), 1, "<!-- m -->", "b"); err == nil {
		t.Fatal("a 403 on the comment list was ignored")
	}
}

func TestUpsertRejectsAnInvalidIssueNumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	if _, _, err := client(t, srv).UpsertComment(context.Background(), 0, "m", "b"); err == nil {
		t.Fatal("issue 0 was accepted")
	}
}

// Every request carries auth and the pinned API version. Dropping the version
// header means a future default changes the response shape under the parser.
func TestRequestsCarryAuthAndTheApiVersion(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		json.NewEncoder(w).Encode([]comment{})
	}))
	defer srv.Close()

	c := client(t, srv)
	if _, err := c.findComment(context.Background(), 1, "m"); err != nil {
		t.Fatalf("findComment: %v", err)
	}
	if got.Get("Authorization") != "Bearer t0ken" {
		t.Errorf("Authorization = %q", got.Get("Authorization"))
	}
	if got.Get("X-GitHub-Api-Version") != apiVersion {
		t.Errorf("X-GitHub-Api-Version = %q, want %q", got.Get("X-GitHub-Api-Version"), apiVersion)
	}
}

// A write that succeeds but returns a body with no url is still a success:
// failing there would report a posted comment as a failed step.
func TestWriteSucceedsEvenWhenTheResponseCarriesNoURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]comment{})
			return
		}
		fmt.Fprint(w, `not json at all`)
	}))
	defer srv.Close()

	url, _, err := client(t, srv).UpsertComment(context.Background(), 1, "m", "b")
	if err != nil {
		t.Fatalf("UpsertComment: %v", err)
	}
	if url != "" {
		t.Errorf("url = %q, want empty", url)
	}
}

func TestTruncateKeepsTheHeadOfALongBody(t *testing.T) {
	long := strings.Repeat("x", 500)
	got := truncate(long)
	if len(got) > 320 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncate produced %d chars ending %q", len(got), got[len(got)-3:])
	}
	if got := truncate("  short  "); got != "short" {
		t.Errorf("truncate(short) = %q", got)
	}
}
