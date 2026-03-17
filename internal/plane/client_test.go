package plane

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetIssue(t *testing.T) {
	tests := []struct {
		name      string
		issueID   string
		handler   http.HandlerFunc
		wantID    string
		wantState string
		wantErr   bool
	}{
		{
			name:    "fetches issue with state",
			issueID: "issue-uuid-1",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %s, want GET", r.Method)
				}
				if !strings.Contains(r.URL.Path, "issue-uuid-1") {
					t.Errorf("path = %s, missing issue ID", r.URL.Path)
				}
				if r.URL.Query().Get("expand") != "state" {
					t.Errorf("expand = %q, want %q", r.URL.Query().Get("expand"), "state")
				}
				if r.Header.Get("X-API-Key") != "test-key" {
					t.Errorf("X-API-Key = %q, want %q", r.Header.Get("X-API-Key"), "test-key")
				}

				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `{"id":"issue-uuid-1","name":"Test issue","description_html":"<p>Desc</p>","state_detail":{"name":"Done","group":"completed"}}`)
			},
			wantID:    "issue-uuid-1",
			wantState: "Done",
		},
		{
			name:    "returns error on non-200 status",
			issueID: "not-found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprint(w, `{"error":"not found"}`)
			},
			wantErr: true,
		},
		{
			name:    "returns error on invalid JSON",
			issueID: "bad-json",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `not json`)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := NewHTTPClient("test-key", srv.URL)
			issue, err := client.GetIssue(context.Background(), "ws", "proj-1", tc.issueID)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("GetIssue: %v", err)
			}
			if issue.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", issue.ID, tc.wantID)
			}
			if issue.State.Name != tc.wantState {
				t.Errorf("State.Name = %q, want %q", issue.State.Name, tc.wantState)
			}
		})
	}
}

func TestCreateIssue(t *testing.T) {
	tests := []struct {
		name    string
		req     CreateIssueRequest
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name: "creates issue successfully",
			req: CreateIssueRequest{
				Name:            "Test issue",
				DescriptionHTML: "<p>Description</p>",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/api/v1/workspaces/ws/projects/proj-1/issues/") {
					t.Errorf("path = %s, unexpected", r.URL.Path)
				}
				if r.Header.Get("X-API-Key") != "test-key" {
					t.Errorf("X-API-Key = %q, want %q", r.Header.Get("X-API-Key"), "test-key")
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
				}

				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("reading body: %v", err)
				}
				var req CreateIssueRequest
				if err := json.Unmarshal(body, &req); err != nil {
					t.Fatalf("unmarshaling body: %v", err)
				}
				if req.Name != "Test issue" {
					t.Errorf("name = %q, want %q", req.Name, "Test issue")
				}

				w.WriteHeader(http.StatusCreated)
				_, _ = fmt.Fprint(w, `{"id":"issue-uuid-1","name":"Test issue","description_html":"<p>Description</p>"}`)
			},
			wantID: "issue-uuid-1",
		},
		{
			name: "returns error on non-201 status",
			req:  CreateIssueRequest{Name: "fail"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(w, `{"error":"bad request"}`)
			},
			wantErr: true,
		},
		{
			name: "returns error on invalid JSON response",
			req:  CreateIssueRequest{Name: "bad json"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = fmt.Fprint(w, `not json`)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := NewHTTPClient("test-key", srv.URL)
			issue, err := client.CreateIssue(context.Background(), "ws", "proj-1", tc.req)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateIssue: %v", err)
			}
			if issue.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", issue.ID, tc.wantID)
			}
		})
	}
}

func TestCreateComment(t *testing.T) {
	tests := []struct {
		name    string
		issueID string
		req     CreateCommentRequest
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name:    "creates comment successfully",
			issueID: "issue-uuid-1",
			req: CreateCommentRequest{
				CommentHTML: "<p>Synced from GitHub</p>",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/api/v1/workspaces/ws/projects/proj-1/issues/issue-uuid-1/comments/") {
					t.Errorf("path = %s, unexpected", r.URL.Path)
				}
				if r.Header.Get("X-API-Key") != "test-key" {
					t.Errorf("X-API-Key = %q, want %q", r.Header.Get("X-API-Key"), "test-key")
				}

				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("reading body: %v", err)
				}
				var req CreateCommentRequest
				if err := json.Unmarshal(body, &req); err != nil {
					t.Fatalf("unmarshaling body: %v", err)
				}
				if req.CommentHTML != "<p>Synced from GitHub</p>" {
					t.Errorf("comment_html = %q, unexpected", req.CommentHTML)
				}

				w.WriteHeader(http.StatusCreated)
				_, _ = fmt.Fprint(w, `{"id":"comment-uuid-1"}`)
			},
			wantID: "comment-uuid-1",
		},
		{
			name:    "returns error on non-201 status",
			issueID: "issue-uuid-1",
			req:     CreateCommentRequest{CommentHTML: "<p>fail</p>"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(w, `{"error":"bad request"}`)
			},
			wantErr: true,
		},
		{
			name:    "returns error on invalid JSON response",
			issueID: "issue-uuid-1",
			req:     CreateCommentRequest{CommentHTML: "<p>bad json</p>"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = fmt.Fprint(w, `not json`)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := NewHTTPClient("test-key", srv.URL)
			comment, err := client.CreateComment(context.Background(), "ws", "proj-1", tc.issueID, tc.req)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateComment: %v", err)
			}
			if comment.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", comment.ID, tc.wantID)
			}
		})
	}
}

func TestUpdateIssue(t *testing.T) {
	tests := []struct {
		name    string
		issueID string
		req     UpdateIssueRequest
		handler http.HandlerFunc
		wantID  string
		wantErr bool
	}{
		{
			name:    "updates issue successfully",
			issueID: "issue-uuid-1",
			req: UpdateIssueRequest{
				Name:            "Updated title",
				DescriptionHTML: "<p>Updated</p>",
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch {
					t.Errorf("method = %s, want PATCH", r.Method)
				}
				if !strings.Contains(r.URL.Path, "issue-uuid-1") {
					t.Errorf("path = %s, missing issue ID", r.URL.Path)
				}

				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("reading body: %v", err)
				}
				var req UpdateIssueRequest
				if err := json.Unmarshal(body, &req); err != nil {
					t.Fatalf("unmarshaling body: %v", err)
				}
				if req.Name != "Updated title" {
					t.Errorf("name = %q, want %q", req.Name, "Updated title")
				}

				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `{"id":"issue-uuid-1","name":"Updated title","description_html":"<p>Updated</p>"}`)
			},
			wantID: "issue-uuid-1",
		},
		{
			name:    "returns error on non-200 status",
			issueID: "issue-uuid-1",
			req:     UpdateIssueRequest{Name: "fail"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprint(w, `{"error":"not found"}`)
			},
			wantErr: true,
		},
		{
			name:    "returns error on invalid JSON response",
			issueID: "issue-uuid-1",
			req:     UpdateIssueRequest{Name: "bad json"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `not json`)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			client := NewHTTPClient("test-key", srv.URL)
			issue, err := client.UpdateIssue(context.Background(), "ws", "proj-1", tc.issueID, tc.req)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateIssue: %v", err)
			}
			if issue.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", issue.ID, tc.wantID)
			}
		})
	}
}
