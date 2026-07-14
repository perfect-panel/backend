package email

import (
	"context"
	"sync"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/task"
	"github.com/perfect-panel/server/pkg/logger"
)

// TaskStore is the minimal task repository interface needed by the email worker.
type TaskStore interface {
	FindOne(ctx context.Context, id int64) (*task.Task, error)
	Update(ctx context.Context, data *task.Task) error
}

var (
	Manager *WorkerManager // 全局调度器实例
	once    sync.Once      // 确保 Scheduler 只被初始化一次
	limit   sync.RWMutex   // 控制并发限制
)

type WorkerManager struct {
	tasks   TaskStore                    // task repository
	sender  Sender                       // 邮件发送器接口
	mutex   sync.RWMutex                 // 读写互斥锁，确保线程安全
	workers map[int64]*Worker            // 存储所有 Worker 实例
	cancels map[int64]context.CancelFunc // 存储每个 Worker 的取消函数
}

func NewWorkerManager(tasks TaskStore, sender Sender) *WorkerManager {
	if Manager != nil {
		return Manager
	}
	once.Do(func() {
		Manager = &WorkerManager{
			tasks:   tasks,
			workers: make(map[int64]*Worker),
			cancels: make(map[int64]context.CancelFunc),
			sender:  sender,
		}
	})
	// 设置定时检查任务
	go func() {
		for {
			// 每隔5分钟检查一次
			select {
			case <-time.After(1 * time.Minute):
				checkWorker()
				continue
			}
		}
	}()
	return Manager
}

// AddWorker 添加一个新的 Worker 实例
func (m *WorkerManager) AddWorker(id int64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, exists := m.workers[id]; !exists {
		ctx, cancel := context.WithCancel(context.Background())
		worker := NewWorker(ctx, id, m.tasks, m.sender)
		m.workers[id] = worker
		m.cancels[id] = cancel
		go worker.Start()
		logger.Info("Batch Send Email",
			logger.Field("message", "Added new worker"),
			logger.Field("task_id", id),
		)
	} else {
		logger.Info("Batch Send Email",
			logger.Field("message", "Worker already exists"),
			logger.Field("task_id", id),
		)
	}

}

// GetWorker 获取指定任务的 Worker 实例
func (m *WorkerManager) GetWorker(id int64) *Worker {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if worker, exists := m.workers[id]; exists {
		return worker
	} else {
		logger.Error("Batch Send Email",
			logger.Field("message", "Worker not found"),
			logger.Field("task_id", id),
		)
		return nil
	}
}

// RemoveWorker 移除指定任务的 Worker 实例
func (m *WorkerManager) RemoveWorker(id int64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, exists := m.workers[id]; exists {
		delete(m.workers, id)
		if cancelFunc, ok := m.cancels[id]; ok {
			cancelFunc() // 调用取消函数
			delete(m.cancels, id)
		}
		logger.Info("Batch Send Email",
			logger.Field("message", "Removed worker"),
			logger.Field("task_id", id),
		)
	} else {
		logger.Error("Batch Send Email",
			logger.Field("message", "Worker not found for removal"),
			logger.Field("task_id", id),
		)
	}
}

func checkWorker() {
	if Manager == nil {
		// 如果 Manager 未初始化，直接返回
		return
	}
	Manager.mutex.Lock()
	defer Manager.mutex.Unlock()
	for id, worker := range Manager.workers {
		if worker.IsRunning() == 2 {
			// 如果Worker已完成，移除它
			delete(Manager.workers, id)
			if cancelFunc, ok := Manager.cancels[id]; ok {
				cancelFunc() // 调用取消函数
				delete(Manager.cancels, id)
			}
			logger.Info("Batch Send Email",
				logger.Field("message", "Removed completed worker"),
				logger.Field("task_id", id),
			)
		}
	}

}
