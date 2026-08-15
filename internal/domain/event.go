package domain

import "fmt"

type EventType string

const (
	EventViewed        EventType = "viewed"
	EventStarred       EventType = "starred"
	EventClickedReadme EventType = "clicked_readme"
)

func (e EventType) Valid() bool {
	switch e {
	case EventViewed, EventStarred, EventClickedReadme:
		return true
	default:
		return false
	}
}

type RepoEvent struct {
	EventID   string    `json:"event_id"`
	EventType EventType `json:"event_type"`
	UserID    int64    `json:"user_id"`
	RepoID    int64    `json:"repo_id"`
}

func (e RepoEvent) Validate() error {
	if e.EventID == "" {
		return fmt.Errorf("missing event_id")
	}

	if !e.EventType.Valid() {
		return fmt.Errorf("invalid event_type: %q", e.EventType)
	}

	if e.UserID <= 0 || e.RepoID <= 0 {
		return fmt.Errorf("invalid user_id or repo_id")
	}

	return nil
}