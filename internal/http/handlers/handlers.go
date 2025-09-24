package handlers

import (
	"encoding/json"
	"fmt"
	"httpWorkerPool/internal/domain/task"
	"httpWorkerPool/internal/services/workerPool"
	"log/slog"
	"net/http"
	"time"
)

type WorkerPool interface {
	Submit(t *task.Task) error
	Stop() error
}
type Handler struct {
	workerPool WorkerPool
	log        *slog.Logger
}

func NewHandler(workerPool WorkerPool, log *slog.Logger) *Handler {
	return &Handler{
		workerPool: workerPool,
		log:        log,
	}
}

func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req task.TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := req.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	t := &task.Task{
		ID:         req.ID,
		Payload:    req.Payload,
		MaxRetries: req.MaxRetries,
		Attempts:   0,
		Status:     task.StatusPending,
	}

	if err := h.workerPool.Submit(t); err != nil {
		switch err {
		case workerPool.ErrPoolStopped:
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		case workerPool.ErrTaskIDAlreadyExists:
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	jsonData, err := json.Marshal(map[string]string{
		"status": "accepted",
		"id":     t.ID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, err = w.Write(jsonData)
	if err != nil {
		h.log.Error("failed to write response", slog.String("err", err.Error()))
	}
}

// Healthz создает тестовую задачу и пытается добавить её в очередь, если добавление успешно, возвращаем 200 OK.
// В противном случае сервер либо уже останавливается, либо сильно перегружен. Возвращаем 503
func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	timeout := 3 * time.Second
	healthCheckDone := make(chan bool, 1)

	go func() {
		testTask := &task.Task{
			ID:         fmt.Sprintf("health_check_%d", time.Now().UnixNano()),
			Payload:    "health_check_payload",
			MaxRetries: 0,
			Attempts:   0,
			Status:     task.StatusPending,
		}

		err := h.workerPool.Submit(testTask)
		if err != nil {
			h.log.Warn("health check: failed to submit test task",
				slog.String("error", err.Error()))
			healthCheckDone <- false
			return
		}

		healthCheckDone <- true
	}()

	select {
	case result := <-healthCheckDone:
		if result {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			response := map[string]string{
				"status": "ok",
			}
			if err := json.NewEncoder(w).Encode(response); err != nil {
				h.log.Error("failed to encode health check response",
					slog.String("error", err.Error()))
			}
		} else {
			h.log.Error("health check failed: cannot submit tasks")
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		}

	case <-time.After(timeout):
		h.log.Error("health check timeout: service is overloaded")
		http.Error(w, "Service Unavailable - Timeout", http.StatusServiceUnavailable)
	}
}
