package workerPool

import (
	"errors"
	"httpWorkerPool/internal/domain/task"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

var (
	ErrPoolStopped         = errors.New("pool is stopped")
	ErrPoolAlreadyStopped  = errors.New("pool is already stopped")
	ErrTaskIDAlreadyExists = errors.New("task ID already exists")
	ErrTaskNotFound        = errors.New("task not found")
)

type workerPool struct {
	taskQueue         chan *task.Task
	workersNumber     int
	tasks             map[string]*task.Task
	stopChan          chan struct{}
	wg                sync.WaitGroup
	mtx               sync.RWMutex
	closeTaskQueueMtx sync.RWMutex
	isStopped         bool
	log               *slog.Logger
}

// NewWorkerPool создает новый экзепляр workerPool.
// workerPool запускается уже во время создания
func NewWorkerPool(workersNumber int, queueSize int, log *slog.Logger) *workerPool {

	// Проверяем на всякий случай корректность параметров
	if workersNumber <= 0 {
		return nil
	}
	if queueSize <= 0 {
		return nil
	}

	wp := &workerPool{
		taskQueue:     make(chan *task.Task, queueSize),
		workersNumber: workersNumber,
		tasks:         make(map[string]*task.Task),
		stopChan:      make(chan struct{}),
		log:           log,
	}

	// Запускаем воркеры
	for i := 0; i < wp.workersNumber; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
	wp.log.Info("worker pool started", slog.Int("workersNumber", wp.workersNumber))

	return wp
}

// worker будет работать пока не закроется канал taskQueue
func (wp *workerPool) worker(workerID int) {
	defer wp.wg.Done()

	for task := range wp.taskQueue {
		wp.processTask(task, workerID)
	}
}

// Submit пытается добавить таску в очередь, ожидает до тех пор пока буффер задач не осводобится и задача не будет добавлена,
// либо до момента пока не придет сигнал об остановке пула
func (wp *workerPool) Submit(t *task.Task) error {
	// проверяем не остановлен ли пул
	wp.mtx.RLock()
	if wp.isStopped {
		return ErrPoolStopped
	}
	wp.mtx.RUnlock()

	// Проверяем наличие задачи с таким же ID
	wp.mtx.Lock()
	if _, ok := wp.tasks[t.ID]; ok {
		wp.mtx.Unlock()
		return ErrTaskIDAlreadyExists
	}
	wp.tasks[t.ID] = t
	wp.mtx.Unlock()

	// closeTaskQueueMtx.Lock() может взять только Stop(), чтобы выполнить закрытие taskQueue, поэтому чтобы не нарваться на панику при чтении из wp.taskQueue <- t:,
	// сначала проверим что закрыт stopChan
	wp.closeTaskQueueMtx.RLock()
	defer wp.closeTaskQueueMtx.RUnlock()
	select {
	case <-wp.stopChan:
		wp.mtx.Lock()
		delete(wp.tasks, t.ID)
		wp.mtx.Unlock()
		return ErrPoolStopped
	default:
		select {
		case wp.taskQueue <- t:
			wp.mtx.Lock()
			t.Status = task.StatusQueued
			wp.mtx.Unlock()
			return nil
		case <-wp.stopChan:
			wp.mtx.Lock()
			delete(wp.tasks, t.ID)
			wp.mtx.Unlock()
			return ErrPoolStopped
		}
	}
}

// Stop плавно завершает работу, посылает сигнал о завершении работы, затем проверяет чтобы никто прямо сейчас не ждал записи в канал из-за переполенного буфера,
// и если убеждается что никого нету, берет блокировку и закрывает канал очереди
func (wp *workerPool) Stop() error {
	// Проверяем что пул не был остановлен до этого
	wp.mtx.Lock()
	if wp.isStopped {
		wp.mtx.Unlock()
		return ErrPoolAlreadyStopped
	}
	wp.isStopped = true
	close(wp.stopChan)
	wp.mtx.Unlock()

	// Позволяем закрыть очередь только если больше никакие Submit не находтся на строчке case wp.taskQueue <- task:(не пытаются записать в канал)
	wp.closeTaskQueueMtx.Lock()
	close(wp.taskQueue)
	wp.closeTaskQueueMtx.Unlock()

	wp.wg.Wait()
	wp.log.Info("worker pool stopped")
	return nil
}

// processTask симулируем выполнение задачи, в 20% случаев будет давать сбой для обработки бэкоффа
func (wp *workerPool) processTask(t *task.Task, workerID int) {
	wp.mtx.Lock()
	t.Status = task.StatusRunning
	t.Attempts++
	wp.mtx.Unlock()

	wp.log.Debug("processing task",
		slog.String("task_id", t.ID),
		slog.Int("worker_id", workerID),
		slog.Int("attempt", t.Attempts))

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	// Симуляция обработки (100-500 мс)
	processingTime := time.Duration(100+r.Intn(400)) * time.Millisecond
	time.Sleep(processingTime)

	// 20% вероятность ошибки
	if r.Float32() < 0.2 {
		wp.handleTaskFailure(t, workerID)
	} else {
		wp.mtx.Lock()
		t.Status = task.StatusDone
		wp.mtx.Unlock()
		wp.log.Info("task completed successfully",
			slog.String("task_id", t.ID),
			slog.Int("worker", workerID))
	}
}

// handleTaskFailure проверяет не превысила ли задача максимальное количество повторов, если нет, то пытается закинуть задачу снова в очердь через заданое время бэкоффа
func (wp *workerPool) handleTaskFailure(t *task.Task, workerID int) {
	if t.Attempts <= t.MaxRetries {
		// Экспоненциальный бэкофф с джиттером
		backoff := calculateBackoff(t.Attempts)

		wp.log.Warn("task failed, retrying",
			slog.String("task_id", t.ID),
			slog.Int("worker", workerID),
			slog.Int("attempt", t.Attempts),
			slog.Duration("backoff", backoff))

		// Ожидаем нужное время и заново пытаемся закинуть задачу в очередь
		time.AfterFunc(backoff, func() {
			wp.mtx.Lock()
			t.Status = task.StatusWaitingRetry
			wp.mtx.Unlock()
			wp.closeTaskQueueMtx.RLock()
			defer wp.closeTaskQueueMtx.RUnlock()
			select {
			case <-wp.stopChan:
				wp.mtx.Lock()
				wp.log.Debug("skip requeue, shutdown in progress",
					slog.String("task_id", t.ID))
				t.Status = task.StatusFailed
				wp.mtx.Unlock()
			default:
				select {
				case wp.taskQueue <- t:
					wp.mtx.RLock()
					wp.log.Debug("task requeued for retry",
						slog.String("task_id", t.ID))
					wp.mtx.RUnlock()
				case <-wp.stopChan:
					wp.mtx.Lock()
					wp.log.Debug("skip requeue, shutdown in progress",
						slog.String("task_id", t.ID))

					t.Status = task.StatusFailed
					wp.mtx.Unlock()
				}
			}
		})
	} else {
		wp.mtx.Lock()
		t.Status = task.StatusFailed
		wp.log.Error("task failed after max retries",
			slog.String("task_id", t.ID),
			slog.Int("worker", workerID),
			slog.Int("attempts", t.Attempts))
		wp.mtx.Unlock()
	}
}

// Экпоненциально увеличиваем бэкофф
func calculateBackoff(attempt int) time.Duration {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	base := time.Second
	backoff := base * (1 << attempt)
	jitter := time.Duration(r.Int63n(int64(base)))
	return backoff + jitter
}
