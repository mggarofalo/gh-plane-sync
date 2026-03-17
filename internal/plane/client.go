// Package plane provides a client for the Plane API, focused on creating and
// updating work items for synchronization with GitHub issues.
package plane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Issue represents a Plane work item with the fields relevant to sync.
type Issue struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	DescriptionHTML string     `json:"description_html"`
	State           IssueState `json:"state_detail"`
}

// IssueState represents the workflow state of a Plane issue as returned
// by the API when using ?expand=state.
type IssueState struct {
	Name  string `json:"name"`
	Group string `json:"group"`
}

// CreateIssueRequest contains the fields needed to create a Plane work item.
type CreateIssueRequest struct {
	Name            string `json:"name"`
	DescriptionHTML string `json:"description_html,omitempty"`
}

// UpdateIssueRequest contains the fields to update on a Plane work item.
type UpdateIssueRequest struct {
	Name            string `json:"name,omitempty"`
	DescriptionHTML string `json:"description_html,omitempty"`
}

// Client defines the interface for interacting with the Plane API.
type Client interface {
	// GetIssue fetches a single work item by ID, including its current
	// workflow state (expanded via ?expand=state).
	GetIssue(ctx context.Context, workspaceSlug, projectID, issueID string) (Issue, error)

	// CreateIssue creates a new work item in the given workspace and project.
	CreateIssue(ctx context.Context, workspaceSlug, projectID string, req CreateIssueRequest) (Issue, error)

	// UpdateIssue updates an existing work item.
	UpdateIssue(ctx context.Context, workspaceSlug, projectID, issueID string, req UpdateIssueRequest) (Issue, error)
}

// HTTPClient implements Client using the Plane REST API.
type HTTPClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewHTTPClient creates a new Plane API client. The apiKey is used for
// X-API-Key authentication. The baseURL should be the Plane instance
// URL (e.g., "https://plane.example.com").
func NewHTTPClient(apiKey, baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// GetIssue fetches a single work item by ID, including its current workflow
// state via the ?expand=state query parameter.
func (c *HTTPClient) GetIssue(ctx context.Context, workspaceSlug, projectID, issueID string) (Issue, error) {
	reqURL := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/?expand=state", c.baseURL, workspaceSlug, projectID, issueID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return Issue{}, fmt.Errorf("creating HTTP request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Issue{}, fmt.Errorf("executing get request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return Issue{}, fmt.Errorf("getting issue %s: unexpected status %d: %s", issueID, resp.StatusCode, string(respBody))
	}

	var issue Issue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return Issue{}, fmt.Errorf("decoding get response: %w", err)
	}

	return issue, nil
}

// CreateIssue creates a work item via the Plane API.
func (c *HTTPClient) CreateIssue(ctx context.Context, workspaceSlug, projectID string, req CreateIssueRequest) (Issue, error) {
	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/", c.baseURL, workspaceSlug, projectID)

	body, err := json.Marshal(req)
	if err != nil {
		return Issue{}, fmt.Errorf("marshaling create request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Issue{}, fmt.Errorf("creating HTTP request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Issue{}, fmt.Errorf("executing create request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return Issue{}, fmt.Errorf("creating issue: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var issue Issue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return Issue{}, fmt.Errorf("decoding create response: %w", err)
	}

	return issue, nil
}

// UpdateIssue updates an existing work item via the Plane API.
func (c *HTTPClient) UpdateIssue(ctx context.Context, workspaceSlug, projectID, issueID string, req UpdateIssueRequest) (Issue, error) {
	url := fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s/issues/%s/", c.baseURL, workspaceSlug, projectID, issueID)

	body, err := json.Marshal(req)
	if err != nil {
		return Issue{}, fmt.Errorf("marshaling update request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return Issue{}, fmt.Errorf("creating HTTP request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Issue{}, fmt.Errorf("executing update request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return Issue{}, fmt.Errorf("updating issue %s: unexpected status %d: %s", issueID, resp.StatusCode, string(respBody))
	}

	var issue Issue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return Issue{}, fmt.Errorf("decoding update response: %w", err)
	}

	return issue, nil
}

// setHeaders adds the standard Plane API headers to a request.
func (c *HTTPClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
}
