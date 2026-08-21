// Package gh is a minimal GitHub REST client covering the one write this
// action performs: posting or updating the comparison comment.
//
// It uses net/http directly rather than a client library because that is the
// only call it makes, and a dependency here would be the module's first.
package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultAPI = "https://api.github.com"
	apiVersion = "2022-11-28"
	// A comment write is a single small request; a run that hangs on it burns
	// a runner minute per attempt for no reason.
	timeout = 30 * time.Second
	// GitHub caps per_page at 100 and an issue with more comments than one
	// page is ordinary, so the comment scan paginates rather than assuming.
	perPage  = 100
	maxPages = 20
)

// Client talks to one repository.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	Token   string
	Owner   string
	Repo    string
}

// New builds a client for "owner/name". It returns an error rather than
// panicking on a malformed repository, because GITHUB_REPOSITORY being unset in
// a local run is a normal thing to hit.
func New(repository, token string) (*Client, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(repository), "/")
	if !ok || owner == "" || repo == "" {
		return nil, fmt.Errorf("repository %q is not in owner/name form", repository)
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("no github token supplied")
	}
	return &Client{
		HTTP:    &http.Client{Timeout: timeout},
		BaseURL: defaultAPI,
		Token:   token,
		Owner:   owner,
		Repo:    repo,
	}, nil
}

type comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// UpsertComment posts body to the issue, replacing the action's own previous
// comment when one carrying marker is found.
//
// It returns the comment's HTML URL and whether an existing comment was
// updated. A failure to LIST is FATAL and deliberately so: falling through to
// "post a new one" would append a duplicate table on every transient failure,
// which is the pile-up the marker exists to prevent. An earlier version of this
// comment claimed the opposite of the code — worse than a typo, because it
// reads as permission to revert the behaviour the tests pin.
func (c *Client) UpsertComment(ctx context.Context, issue int, marker, body string) (string, bool, error) {
	if issue <= 0 {
		return "", false, fmt.Errorf("issue number %d is not valid", issue)
	}

	existing, err := c.findComment(ctx, issue, marker)
	if err != nil {
		return "", false, err
	}

	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return "", false, fmt.Errorf("encode comment: %w", err)
	}

	if existing != 0 {
		endpoint := fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d", c.BaseURL, c.Owner, c.Repo, existing)
		htmlURL, err := c.write(ctx, http.MethodPatch, endpoint, payload)
		return htmlURL, true, err
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", c.BaseURL, c.Owner, c.Repo, issue)
	htmlURL, err := c.write(ctx, http.MethodPost, endpoint, payload)
	return htmlURL, false, err
}

// findComment returns the id of the newest comment containing marker, or 0.
func (c *Client) findComment(ctx context.Context, issue int, marker string) (int64, error) {
	var match int64
	for page := 1; page <= maxPages; page++ {
		endpoint := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments?per_page=%d&page=%d",
			c.BaseURL, c.Owner, c.Repo, issue, perPage, page)

		req, err := c.request(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return 0, err
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return 0, fmt.Errorf("list comments: %w", err)
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return 0, fmt.Errorf("read comment list: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return 0, fmt.Errorf("list comments: %s: %s", resp.Status, truncate(string(raw)))
		}

		var batch []comment
		if err := json.Unmarshal(raw, &batch); err != nil {
			return 0, fmt.Errorf("parse comment list: %w", err)
		}
		for _, cm := range batch {
			if strings.Contains(cm.Body, marker) {
				match = cm.ID
			}
		}
		if len(batch) < perPage {
			break
		}
	}
	return match, nil
}

func (c *Client) write(ctx context.Context, method, endpoint string, payload []byte) (string, error) {
	req, err := c.request(ctx, method, endpoint, payload)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s comment: %w", strings.ToLower(method), err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read comment response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s comment: %s: %s", strings.ToLower(method), resp.Status, truncate(string(raw)))
	}

	var out struct {
		HTMLURL string `json:"html_url"`
	}
	// A body that parses to no URL is not an error: the write succeeded, and
	// failing here would report a success as a failure.
	_ = json.Unmarshal(raw, &out)
	return out.HTMLURL, nil
}

func (c *Client) request(ctx context.Context, method, endpoint string, payload []byte) (*http.Request, error) {
	if _, err := url.Parse(endpoint); err != nil {
		return nil, fmt.Errorf("bad endpoint %q: %w", endpoint, err)
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "somaz94-agent-fanout")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// truncate keeps an API error body from flooding the log while still carrying
// the part that names the cause.
func truncate(s string) string {
	const max = 300
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
