package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.github.com"

// Client talks to the GitHub API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Issue holds issue data.
type Issue struct {
	Number int
	State  string
	Title  string
	Body   string
	Labels []string
	Author string
}

// Comment holds a GitHub comment.
type Comment struct {
	User string
	Body string
}

// Repo holds repository data.
type Repo struct {
	DefaultBranch string
	CloneURL      string
}

// PR holds pull request data.
type PR struct {
	Number  int
	Title   string
	Body    string
	State   string
	HeadRef string
	BaseRef string
	URL     string
}

// PRRequest is used to create a PR.
type PRRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

// NewClient creates a GitHub API client.
func NewClient(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// CreateIssueComment posts a comment to an issue.
func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	payload := map[string]string{"body": body}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	return c.doRequest(ctx, http.MethodPost, path, payload, nil)
}

// GetIssue retrieves issue details.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (Issue, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	var resp struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return Issue{}, err
	}
	labels := make([]string, 0, len(resp.Labels))
	for _, label := range resp.Labels {
		labels = append(labels, label.Name)
	}
	return Issue{
		Number: resp.Number,
		State:  resp.State,
		Title:  resp.Title,
		Body:   resp.Body,
		Labels: labels,
		Author: resp.User.Login,
	}, nil
}

// ListIssueComments lists issue comments.
func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]Comment, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	var resp []struct {
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Comment, 0, len(resp))
	for _, item := range resp {
		out = append(out, Comment{User: item.User.Login, Body: item.Body})
	}
	return out, nil
}

// SetIssueLabels replaces issue labels.
func (c *Client) SetIssueLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", owner, repo, number)
	payload := map[string][]string{"labels": labels}
	return c.doRequest(ctx, http.MethodPut, path, payload, nil)
}

// CreatePR creates a pull request.
func (c *Client) CreatePR(ctx context.Context, owner, repo string, req PRRequest) (PR, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)
	var resp struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		Head    struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := c.doRequest(ctx, http.MethodPost, path, req, &resp); err != nil {
		return PR{}, err
	}
	return PR{
		Number:  resp.Number,
		URL:     resp.HTMLURL,
		Title:   resp.Title,
		Body:    resp.Body,
		State:   resp.State,
		HeadRef: resp.Head.Ref,
		BaseRef: resp.Base.Ref,
	}, nil
}

// UpdatePRBody updates a PR body.
func (c *Client) UpdatePRBody(ctx context.Context, owner, repo string, number int, body string) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	payload := map[string]string{"body": body}
	return c.doRequest(ctx, http.MethodPatch, path, payload, nil)
}

// AddAssignees assigns users to an issue/PR.
func (c *Client) AddAssignees(ctx context.Context, owner, repo string, number int, assignees []string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/assignees", owner, repo, number)
	payload := map[string][]string{"assignees": assignees}
	return c.doRequest(ctx, http.MethodPost, path, payload, nil)
}

// GetRepo retrieves repository info.
func (c *Client) GetRepo(ctx context.Context, owner, repo string) (Repo, error) {
	path := fmt.Sprintf("/repos/%s/%s", owner, repo)
	var resp struct {
		DefaultBranch string `json:"default_branch"`
		CloneURL      string `json:"clone_url"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return Repo{}, err
	}
	return Repo{DefaultBranch: resp.DefaultBranch, CloneURL: resp.CloneURL}, nil
}

// GetPR retrieves PR details.
func (c *Client) GetPR(ctx context.Context, owner, repo string, number int) (PR, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	var resp struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
		Head    struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return PR{}, err
	}
	return PR{Number: resp.Number, Title: resp.Title, Body: resp.Body, State: resp.State, URL: resp.HTMLURL, HeadRef: resp.Head.Ref, BaseRef: resp.Base.Ref}, nil
}

func (c *Client) doRequest(ctx context.Context, method, requestPath string, payload any, out any) error {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return err
	}
	base.Path = path.Join(base.Path, requestPath)
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, base.String(), body)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error: %s", string(data))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
