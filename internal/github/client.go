// Package github provides a client for the GitHub REST API v3, focused on
// fetching issues for synchronization with Plane.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Issue represents a GitHub issue with the fields relevant to sync.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Client defines the interface for interacting with the GitHub API.
type Client interface {
	// ListIssues fetches open issues for the given repository. If since is
	// non-zero, only issues updated after that time are returned. The
	// implementation handles pagination transparently.
	ListIssues(ctx context.Context, owner, repo string, since time.Time) ([]Issue, error)

	// GetIssue fetches a single issue by number, regardless of state.
	GetIssue(ctx context.Context, owner, repo string, number int) (Issue, error)

	// CloseIssue sets the issue state to "closed".
	CloseIssue(ctx context.Context, owner, repo string, number int) error

	// ReopenIssue sets the issue state to "open".
	ReopenIssue(ctx context.Context, owner, repo string, number int) error

	// CreateComment adds a comment to the given issue.
	CreateComment(ctx context.Context, owner, repo string, number int, body string) error
}

// HTTPClient implements Client using the GitHub REST API v3.
type HTTPClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewHTTPClient creates a new GitHub API client. The token is used for
// Bearer authentication. If baseURL is empty, https://api.github.com is used.
func NewHTTPClient(token, baseURL string) *HTTPClient {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &HTTPClient{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ListIssues fetches open issues for a GitHub repository, handling pagination.
// If since is non-zero, only issues updated at or after that time are returned.
func (c *HTTPClient) ListIssues(ctx context.Context, owner, repo string, since time.Time) ([]Issue, error) {
	var allIssues []Issue
	page := 1

	for {
		issues, nextPage, err := c.listIssuesPage(ctx, owner, repo, since, page)
		if err != nil {
			return nil, fmt.Errorf("fetching issues page %d for %s/%s: %w", page, owner, repo, err)
		}
		allIssues = append(allIssues, issues...)

		if nextPage == 0 {
			break
		}
		page = nextPage
	}

	return allIssues, nil
}

// listIssuesPage fetches a single page of issues and returns the next page
// number (0 if there is no next page).
func (c *HTTPClient) listIssuesPage(ctx context.Context, owner, repo string, since time.Time, page int) ([]Issue, int, error) {
	u, err := url.Parse(fmt.Sprintf("%s/repos/%s/%s/issues", c.baseURL, owner, repo))
	if err != nil {
		return nil, 0, fmt.Errorf("parsing URL: %w", err)
	}

	q := u.Query()
	q.Set("state", "open")
	q.Set("per_page", "100")
	q.Set("page", strconv.Itoa(page))
	q.Set("sort", "updated")
	q.Set("direction", "asc")
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var issues []Issue
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		return nil, 0, fmt.Errorf("decoding response: %w", err)
	}

	// Filter out pull requests (GitHub returns them in the issues endpoint).
	filtered := issues[:0]
	for _, issue := range issues {
		if !isPullRequest(issue) {
			filtered = append(filtered, issue)
		}
	}

	nextPage := parseNextPage(resp.Header.Get("Link"))
	return filtered, nextPage, nil
}

// GetIssue fetches a single issue by number, regardless of its state.
func (c *HTTPClient) GetIssue(ctx context.Context, owner, repo string, number int) (Issue, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/issues/%d", c.baseURL, owner, repo, number)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Issue{}, fmt.Errorf("creating request: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Issue{}, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Issue{}, fmt.Errorf("getting issue %s/%s#%d: unexpected status %d: %s", owner, repo, number, resp.StatusCode, string(body))
	}

	var issue Issue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return Issue{}, fmt.Errorf("decoding response: %w", err)
	}

	return issue, nil
}

// CloseIssue sets the issue state to "closed" via the GitHub API.
func (c *HTTPClient) CloseIssue(ctx context.Context, owner, repo string, number int) error {
	return c.patchIssueState(ctx, owner, repo, number, "closed")
}

// ReopenIssue sets the issue state to "open" via the GitHub API.
func (c *HTTPClient) ReopenIssue(ctx context.Context, owner, repo string, number int) error {
	return c.patchIssueState(ctx, owner, repo, number, "open")
}

// patchIssueState sends a PATCH request to update the issue state.
func (c *HTTPClient) patchIssueState(ctx context.Context, owner, repo string, number int, state string) error {
	u := fmt.Sprintf("%s/repos/%s/%s/issues/%d", c.baseURL, owner, repo, number)

	payload := fmt.Sprintf(`{"state":%q}`, state)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, u, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	c.setAuthHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("setting issue %s/%s#%d state to %s: unexpected status %d: %s", owner, repo, number, state, resp.StatusCode, string(body))
	}

	return nil
}

// CreateComment adds a comment to the given issue via the GitHub API.
func (c *HTTPClient) CreateComment(ctx context.Context, owner, repo string, number int, body string) error {
	u := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", c.baseURL, owner, repo, number)

	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("marshaling comment: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	c.setAuthHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("creating comment on %s/%s#%d: unexpected status %d: %s", owner, repo, number, resp.StatusCode, string(respBody))
	}

	return nil
}

// setAuthHeaders adds the standard GitHub API headers to a request.
func (c *HTTPClient) setAuthHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// isPullRequest checks whether a GitHub "issue" is actually a pull request.
// The issues endpoint includes PRs; they are identifiable by having a
// pull_request key in the JSON. Since we decode into our Issue struct, we
// check the HTML URL for the /pull/ path segment as a lightweight heuristic.
func isPullRequest(issue Issue) bool {
	return prURLPattern.MatchString(issue.HTMLURL)
}

var prURLPattern = regexp.MustCompile(`/pull/\d+$`)

// linkNextRe matches the "next" relation in a GitHub Link header.
var linkNextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// parseNextPage extracts the next page number from a GitHub Link header.
// Returns 0 if there is no next page.
func parseNextPage(linkHeader string) int {
	if linkHeader == "" {
		return 0
	}

	matches := linkNextRe.FindStringSubmatch(linkHeader)
	if len(matches) < 2 {
		return 0
	}

	u, err := url.Parse(matches[1])
	if err != nil {
		return 0
	}

	pageStr := u.Query().Get("page")
	if pageStr == "" {
		return 0
	}

	page, err := strconv.Atoi(pageStr)
	if err != nil {
		return 0
	}

	return page
}
