package emailworker

import (
	"context"
	"sync"

	"github.com/perfect-panel/server/internal/module/platform/entity/task"
	emailpkg "github.com/perfect-panel/server/pkg/email"
	"github.com/perfect-panel/server/pkg/logger"
)

// TaskStore is the minimal task repository interface needed by the batch-email worker.
type TaskStore interface {
	FindOneByType(ctx context.Context, id int64, typ task.Type) (*task.Task, error)
	UpdateActive(ctx context.Context, data *task.Task) (bool, error)
}

var (
	Manager *WorkerManager
	once    sync.Once
	sendOne = make(chan struct{}, 1)
)

// WorkerManager owns asynchronous batch-email workers. It is an application
// worker, so it belongs in internal rather than the reusable email package.
type WorkerManager struct {
	mutex   sync.RWMutex
	workers map[int64]*Worker
	cancels map[int64]context.CancelFunc
}

func NewWorkerManager() *WorkerManager {
	if Manager != nil {
		return Manager
	}
	once.Do(func() {
		Manager = &WorkerManager{
			workers: make(map[int64]*Worker),
			cancels: make(map[int64]context.CancelFunc),
		}
	})
	return Manager
}

// RunWorker keeps queue acknowledgement coupled to the actual campaign run.
// A duplicate delivery in the same process is harmless: the existing worker
// remains authoritative for that database task.
func (m *WorkerManager) RunWorker(ctx context.Context, id int64, tasks TaskStore, sender emailpkg.Sender) error {
	m.mutex.Lock()
	if _, exists := m.workers[id]; exists {
		m.mutex.Unlock()
		logger.Info("Batch Send Email", logger.Field("message", "Worker already exists"), logger.Field("task_id", id))
		return nil
	}
	workerCtx, cancel := context.WithCancel(ctx)
	worker := NewWorker(workerCtx, id, tasks, sender)
	m.workers[id] = worker
	m.cancels[id] = cancel
	m.mutex.Unlock()

	logger.Info("Batch Send Email", logger.Field("message", "Added new worker"), logger.Field("task_id", id))
	err := worker.Start()

	m.mutex.Lock()
	delete(m.workers, id)
	delete(m.cancels, id)
	m.mutex.Unlock()
	cancel()
	return err
}

// GetWorker returns the worker currently assigned to id.
func (m *WorkerManager) GetWorker(id int64) *Worker {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if worker, exists := m.workers[id]; exists {
		return worker
	}
	logger.Error("Batch Send Email",
		logger.Field("message", "Worker not found"),
		logger.Field("task_id", id),
	)
	return nil
}

// RemoveWorker cancels and removes the worker assigned to id.
func (m *WorkerManager) RemoveWorker(id int64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, exists := m.workers[id]; exists {
		delete(m.workers, id)
		if cancelFunc, ok := m.cancels[id]; ok {
			cancelFunc()
			delete(m.cancels, id)
		}
		logger.Info("Batch Send Email",
			logger.Field("message", "Removed worker"),
			logger.Field("task_id", id),
		)
		return
	}
	logger.Error("Batch Send Email",
		logger.Field("message", "Worker not found for removal"),
		logger.Field("task_id", id),
	)
}
