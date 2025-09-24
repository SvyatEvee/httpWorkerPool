package task

import (
	"errors"
)

type TaskStatus string

const (
	StatusPending      TaskStatus = "pending"
	StatusQueued       TaskStatus = "queued"
	StatusRunning      TaskStatus = "running"
	StatusWaitingRetry TaskStatus = "waiting retry"
	StatusDone         TaskStatus = "done"
	StatusFailed       TaskStatus = "failed"
)

type Task struct {
	ID         string     `json:"id"`
	Payload    string     `json:"payload"`
	MaxRetries int        `json:"max_retries"`
	Attempts   int        `json:"attempts"`
	Status     TaskStatus `json:"status"`
}

type TaskRequest struct {
	ID         string `json:"id"`
	Payload    string `json:"payload"`
	MaxRetries int    `json:"max_retries"`
}

func (t *TaskRequest) Validate() error {
	if t.ID == "" {
		return errors.New("id is required")
	}
	if t.MaxRetries < 0 {
		return errors.New("max_retries must be >= 0")
	}
	return nil
}
