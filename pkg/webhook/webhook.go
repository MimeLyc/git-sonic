package webhook

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// EventType is the GitHub event type.
type EventType string

const (
	EventIssues       EventType = "issues"
	EventIssueComment EventType = "issue_comment"
	EventPRComment    EventType = "pull_request_review_comment"
)

// Repository holds repository metadata.
type Repository struct {
	FullName      string
	CloneURL      string
	DefaultBranch string
}

// Issue holds issue fields used by the service.
type Issue struct {
	Number int
	State  string
	Title  string
	Body   string
	Labels []string
}

// PullRequest holds PR fields used by the service.
type PullRequest struct {
	Number  int
	State   string
	Title   string
	Body    string
	HeadRef string
	BaseRef string
}

// Event represents a parsed GitHub webhook event.
type Event struct {
	Type        EventType
	Action      string
	DeliveryID  string
	Repository  Repository
	Issue       *Issue
	PullRequest *PullRequest
	Label       string
	CommentBody string
	Sender      string
}

// ParseEvent parses an HTTP request into a webhook Event.
func ParseEvent(r *http.Request) (Event, error) {
	eventType := EventType(r.Header.Get("X-GitHub-Event"))
	if eventType == "" {
		return Event{}, errors.New("missing X-GitHub-Event header")
	}
	if eventType != EventIssues && eventType != EventIssueComment && eventType != EventPRComment {
		return Event{}, errors.New("unsupported event type")
	}
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		return Event{}, err
	}

	var raw struct {
		Action string `json:"action"`
		Label  struct {
			Name string `json:"name"`
		} `json:"label"`
		Issue struct {
			Number int    `json:"number"`
			State  string `json:"state"`
			Title  string `json:"title"`
			Body   string `json:"body"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"issue"`
		PullRequest struct {
			Number int    `json:"number"`
			State  string `json:"state"`
			Title  string `json:"title"`
			Body   string `json:"body"`
			Head   struct {
				Ref string `json:"ref"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
		} `json:"pull_request"`
		Comment struct {
			Body string `json:"body"`
		} `json:"comment"`
		Repository struct {
			FullName      string `json:"full_name"`
			CloneURL      string `json:"clone_url"`
			DefaultBranch string `json:"default_branch"`
		} `json:"repository"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
	}

	if err := json.Unmarshal(payload, &raw); err != nil {
		return Event{}, err
	}

	event := Event{
		Type:       eventType,
		Action:     raw.Action,
		DeliveryID: r.Header.Get("X-GitHub-Delivery"),
		Repository: Repository{
			FullName:      raw.Repository.FullName,
			CloneURL:      raw.Repository.CloneURL,
			DefaultBranch: raw.Repository.DefaultBranch,
		},
		Label:       raw.Label.Name,
		CommentBody: raw.Comment.Body,
		Sender:      raw.Sender.Login,
	}

	if raw.Issue.Number != 0 {
		labels := make([]string, 0, len(raw.Issue.Labels))
		for _, label := range raw.Issue.Labels {
			labels = append(labels, label.Name)
		}
		event.Issue = &Issue{
			Number: raw.Issue.Number,
			State:  raw.Issue.State,
			Title:  raw.Issue.Title,
			Body:   raw.Issue.Body,
			Labels: labels,
		}
	}

	if raw.PullRequest.Number != 0 {
		event.PullRequest = &PullRequest{
			Number:  raw.PullRequest.Number,
			State:   raw.PullRequest.State,
			Title:   raw.PullRequest.Title,
			Body:    raw.PullRequest.Body,
			HeadRef: raw.PullRequest.Head.Ref,
			BaseRef: raw.PullRequest.Base.Ref,
		}
	}

	return event, nil
}
