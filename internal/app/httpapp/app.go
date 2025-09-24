package httpapp

import (
	"context"
	"fmt"
	"httpWorkerPool/internal/domain/task"
	"httpWorkerPool/internal/http/handlers"

	"httpWorkerPool/internal/config"
	"httpWorkerPool/internal/services/workerPool"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type WorkerPool interface {
	Submit(t *task.Task) error
	Stop() error
}

type app struct {
	log        *slog.Logger
	server     *http.Server
	port       int
	workerPool WorkerPool
	wg         sync.WaitGroup
}

// New создает новый экзепляр http приложения
func New(cfg *config.Config, log *slog.Logger) *app {
	mux := http.NewServeMux()

	// Создаем сервисный слой с воркером
	workerPool := workerPool.NewWorkerPool(cfg.WorkersNumber, cfg.QueueSize, log)
	if workerPool == nil {
		panic("can't create new workerPool")
	}

	// Создаем слой с хендлерами и регистрируем их для сервера
	handler := handlers.NewHandler(workerPool, log)

	mux.HandleFunc("/enqueue", handler.Submit)
	mux.HandleFunc("/healthz", handler.Healthz)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
	}

	return &app{
		log:        log,
		server:     server,
		port:       cfg.Port,
		workerPool: workerPool,
	}
}

// MustRun пытается запустить сервер, в случае неудачи вызывает панику
func (a *app) MustRun() {
	a.log.Info("starting server", slog.Int("port", a.port))

	if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(fmt.Sprintf("server error: %v", err))
	}
}

// Stop оповещает сначала воркер потом и сам сервер, что пора завершать свою работу, дожидается окончания обработки всех успевших попасть в очередь задач
func (a *app) Stop() {
	a.log.Info("shutting down server")

	// Останавливаем прием новых задач
	if err := a.workerPool.Stop(); err != nil {
		a.log.Error("failed to stop workerPool", slog.String("error", err.Error()))
	}

	// Даем 30 секунд на завершение, иначе завершаем аварийно
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := a.server.Shutdown(ctx); err != nil {
		a.log.Error("server shutdown error", slog.String("error", err.Error()))
	}

	a.log.Info("server stopped gracefully")
}
