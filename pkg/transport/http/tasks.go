package axhttp

import (
	"sync"

	"github.com/google/uuid"
	"github.com/zigamedved/agent-exchange/pkg/protocol"
)

// taskStore is a thread-safe in-memory cache of tasks returned by the agent.
// The Server uses it to serve a2a_getTask and a2a_cancelTask.
type taskStore struct {
	mu    sync.RWMutex
	tasks map[string]*protocol.Task
}

func newTaskStore() *taskStore {
	return &taskStore{tasks: make(map[string]*protocol.Task)}
}

// Store saves a task. If the task has no ID, one is generated.
func (s *taskStore) Store(t *protocol.Task) {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t
}

// Get retrieves a task by ID.
func (s *taskStore) Get(id string) (*protocol.Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}
