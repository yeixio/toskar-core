package automations

import "time"

// RunStatus is the lifecycle of one scheduled occurrence.
type RunStatus string

const (
	RunClaimed   RunStatus = "claimed"
	RunRunning   RunStatus = "running"
	RunRetrying  RunStatus = "retrying"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
)

// Run is the durable record for one occurrence of an automation.
type Run struct {
	ID               string     `json:"id"`
	AutomationID     string     `json:"automation_id"`
	OccurrenceAt     time.Time  `json:"occurrence_at"`
	Status           RunStatus  `json:"status"`
	ClaimedAt        *time.Time `json:"claimed_at,omitempty"`
	LeaseUntil       *time.Time `json:"lease_until,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	Result           string     `json:"result,omitempty"`
	Error            string     `json:"error,omitempty"`
	NotificationSent bool       `json:"notification_sent"`
	ModelID          string     `json:"model_id,omitempty"`
	NodeID           string     `json:"node_id,omitempty"`
	Attempt          int        `json:"attempt"`
	RetryAt          *time.Time `json:"retry_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// Detail is an automation and its run history, newest occurrence first.
type Detail struct {
	Automation
	History []Run `json:"history"`
}

// Execution is the outcome of running a scheduled prompt.
type Execution struct {
	Text    string
	ModelID string
	NodeID  string
	// Skipped lists tools the run reached that nobody approved for it.
	Skipped []string
}
