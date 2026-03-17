package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListIssues(t *testing.T) {
	tests := []struct {
		name      string
		since     time.Time
		handler   http.HandlerFunc
		wantCount int
		wantErr   bool
	}{
		{
			name: "returns issues",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `[
					{"number":1,"title":"Bug report","body":"Fix it","state":"open","html_url":"https://github.com/o/r/issues/1","updated_at":"2025-01-01T00:00:00Z"},
					{"number":2,"title":"Feature request","body":"Add it","state":"open","html_url":"https://github.com/o/r/issues/2","updated_at":"2025-01-02T00:00:00Z"}
				]`)
			},
			wantCount: 2,
		},
		{
			name: "filters pull requests",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `[
					{"number":1,"title":"Issue","body":"","state":"open","html_url":"https://github.com/o/r/issues/1","updated_at":"2025-01-01T00:00:00Z"},
					{"number":2,"title":"PR","body":"","state":"open","html_url":"https://github.com/o/r/pull/2","updated_at":"2025-01-01T00:00:00Z"}
				]`)
			},
			wantCount: 1,
		},
		{
			name:  "passes since parameter",
			since: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			handler: func(w http.ResponseWriter, r *http.Request) {
				since := r.URL.Query().Get("since")
				if since != "2025-06-01T00:00:00Z" {
					t.Errorf("since = %q, want %q", since, "2025-06-01T00:00:00Z")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `[]`)
			},
			wantCount: 0,
		},
		{
			name:      "handles pagination",
			handler:   paginatedHandler(t),
			wantCount: 2,
		},
		{
			name: "returns error on non-200 status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprint(w, `{"message":"rate limited"}`)
			},
			wantErr: true,
		},
		{
			name: "returns error on invalid JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `not json`)
			},
			wantErr: true,
		},
		{
			name: "empty response",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `[]`)
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := NewHTTPClient("test-token", srv.URL)
			issues, err := client.ListIssues(context.Background(), "owner", "repo", tc.since)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ListIssues: %v", err)
			}
			if len(issues) != tc.wantCount {
				t.Errorf("got %d issues, want %d", len(issues), tc.wantCount)
			}
		})
	}
}

// paginatedHandler returns a handler that serves two pages of results.
func paginatedHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch page {
		case "", "1":
			// Include Link header pointing to page 2.
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/owner/repo/issues?page=2>; rel="next"`, "http://"+r.Host))
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `[{"number":1,"title":"Issue 1","body":"","state":"open","html_url":"https://github.com/o/r/issues/1","updated_at":"2025-01-01T00:00:00Z"}]`)
		case "2":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `[{"number":2,"title":"Issue 2","body":"","state":"open","html_url":"https://github.com/o/r/issues/2","updated_at":"2025-01-02T00:00:00Z"}]`)
		default:
			t.Errorf("unexpected page: %s", page)
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}

func TestListIssuesAuth(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantAuth string
	}{
		{
			name:     "includes bearer token",
			token:    "ghp_abc123",
			wantAuth: "Bearer ghp_abc123",
		},
		{
			name:     "no auth header when token empty",
			token:    "",
			wantAuth: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := r.Header.Get("Authorization")
				if got != tc.wantAuth {
					t.Errorf("Authorization = %q, want %q", got, tc.wantAuth)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `[]`)
			}))
			defer srv.Close()

			client := NewHTTPClient(tc.token, srv.URL)
			_, err := client.ListIssues(context.Background(), "o", "r", time.Time{})
			if err != nil {
				t.Fatalf("ListIssues: %v", err)
			}
		})
	}
}

func TestParseNextPage(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   int
	}{
		{
			name:   "empty header",
			header: "",
			want:   0,
		},
		{
			name:   "single next link",
			header: `<https://api.github.com/repos/o/r/issues?page=3>; rel="next"`,
			want:   3,
		},
		{
			name:   "next and last links",
			header: `<https://api.github.com/repos/o/r/issues?page=2>; rel="next", <https://api.github.com/repos/o/r/issues?page=5>; rel="last"`,
			want:   2,
		},
		{
			name:   "only last link",
			header: `<https://api.github.com/repos/o/r/issues?page=5>; rel="last"`,
			want:   0,
		},
		{
			name:   "no page param",
			header: `<https://api.github.com/repos/o/r/issues>; rel="next"`,
			want:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseNextPage(tc.header)
			if got != tc.want {
				t.Errorf("parseNextPage(%q) = %d, want %d", tc.header, got, tc.want)
			}
		})
	}
}

func TestGetIssue(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantState string
		wantErr   bool
	}{
		{
			name: "fetches open issue",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %s, want GET", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/repos/owner/repo/issues/42") {
					t.Errorf("path = %s, unexpected", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"number":42,"title":"Bug","body":"Fix it","state":"open","html_url":"https://github.com/owner/repo/issues/42","updated_at":"2025-01-01T00:00:00Z"}`)
			},
			wantState: "open",
		},
		{
			name: "fetches closed issue",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"number":42,"title":"Bug","body":"Fix it","state":"closed","html_url":"https://github.com/owner/repo/issues/42","updated_at":"2025-01-01T00:00:00Z"}`)
			},
			wantState: "closed",
		},
		{
			name: "returns error on non-200 status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := NewHTTPClient("test-token", srv.URL)
			issue, err := client.GetIssue(context.Background(), "owner", "repo", 42)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("GetIssue: %v", err)
			}
			if issue.State != tc.wantState {
				t.Errorf("State = %q, want %q", issue.State, tc.wantState)
			}
		})
	}
}

func TestCloseIssue(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "closes issue successfully",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch {
					t.Errorf("method = %s, want PATCH", r.Method)
				}
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), `"state":"closed"`) {
					t.Errorf("body = %s, want state:closed", string(body))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"number":1,"state":"closed"}`)
			},
		},
		{
			name: "returns error on non-200 status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = fmt.Fprint(w, `{"message":"Validation Failed"}`)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := NewHTTPClient("test-token", srv.URL)
			err := client.CloseIssue(context.Background(), "owner", "repo", 1)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("CloseIssue: %v", err)
			}
		})
	}
}

func TestReopenIssue(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "reopens issue successfully",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch {
					t.Errorf("method = %s, want PATCH", r.Method)
				}
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), `"state":"open"`) {
					t.Errorf("body = %s, want state:open", string(body))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"number":1,"state":"open"}`)
			},
		},
		{
			name: "returns error on non-200 status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = fmt.Fprint(w, `{"message":"Validation Failed"}`)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := NewHTTPClient("test-token", srv.URL)
			err := client.ReopenIssue(context.Background(), "owner", "repo", 1)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ReopenIssue: %v", err)
			}
		})
	}
}

func TestCreateComment(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "creates comment successfully",
			body: "Closed by Plane workflow",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/repos/owner/repo/issues/1/comments") {
					t.Errorf("path = %s, unexpected", r.URL.Path)
				}

				reqBody, _ := io.ReadAll(r.Body)
				var payload map[string]string
				if err := json.Unmarshal(reqBody, &payload); err != nil {
					t.Fatalf("unmarshaling body: %v", err)
				}
				if payload["body"] != "Closed by Plane workflow" {
					t.Errorf("body = %q, want %q", payload["body"], "Closed by Plane workflow")
				}

				w.WriteHeader(http.StatusCreated)
				_, _ = fmt.Fprint(w, `{"id":1,"body":"Closed by Plane workflow"}`)
			},
		},
		{
			name: "returns error on non-201 status",
			body: "fail",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := NewHTTPClient("test-token", srv.URL)
			err := client.CreateComment(context.Background(), "owner", "repo", 1, tc.body)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateComment: %v", err)
			}
		})
	}
}

func TestIsPullRequest(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "issue URL",
			url:  "https://github.com/owner/repo/issues/42",
			want: false,
		},
		{
			name: "pull request URL",
			url:  "https://github.com/owner/repo/pull/42",
			want: true,
		},
		{
			name: "empty URL",
			url:  "",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			issue := Issue{HTMLURL: tc.url}
			got := isPullRequest(issue)
			if got != tc.want {
				t.Errorf("isPullRequest(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}
