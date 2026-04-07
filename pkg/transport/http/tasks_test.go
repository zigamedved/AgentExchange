package axhttp

import (
	"testing"

	"github.com/zigamedved/agent-exchange/pkg/protocol"
)

func TestTaskStore_StoreAndGet(t *testing.T) {
	s := newTaskStore()

	task := &protocol.Task{
		ID: "task-123",
		Status: protocol.TaskStatus{State: protocol.TaskStateCompleted},
	}
	s.Store(task)

	got, ok := s.Get("task-123")
	if !ok {
		t.Fatal("expected task, got not found")
	}
	if got.ID != "task-123" {
		t.Errorf("unexpected task ID: %s", got.ID)
	}
	if got.Status.State != protocol.TaskStateCompleted {
		t.Errorf("unexpected state: %s", got.Status.State)
	}
}

func TestTaskStore_Store_GeneratesID(t *testing.T) {
	s := newTaskStore()

	task := &protocol.Task{} // no ID
	s.Store(task)

	if task.ID == "" {
		t.Error("expected ID to be generated")
	}

	_, ok := s.Get(task.ID)
	if !ok {
		t.Error("expected task to be retrievable by generated ID")
	}
}

func TestTaskStore_Get_NotFound(t *testing.T) {
	s := newTaskStore()

	if _, ok := s.Get("nonexistent"); ok {
		t.Error("expected not found for unknown task ID")
	}
}

func TestTaskStore_Overwrite(t *testing.T) {
	s := newTaskStore()

	s.Store(&protocol.Task{ID: "t1", Status: protocol.TaskStatus{State: protocol.TaskStateWorking}})
	s.Store(&protocol.Task{ID: "t1", Status: protocol.TaskStatus{State: protocol.TaskStateCompleted}})

	got, _ := s.Get("t1")
	if got.Status.State != protocol.TaskStateCompleted {
		t.Errorf("expected updated state, got %s", got.Status.State)
	}
}
