// Package github provides a client for the GitHub REST API v3, focused on
// fetching issues for synchronization with Plane.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

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
