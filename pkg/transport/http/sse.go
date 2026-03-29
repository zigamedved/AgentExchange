package faxphttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/zigamedved/faxp/pkg/protocol"
)

// SSEWriter writes Server-Sent Events to an HTTP response stream.
// Use it inside StreamingAgent.HandleMessageStream implementations.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	taskID  string
}

func (s *SSEWriter) ensureTaskID() string {
	if s.taskID == "" {
		s.taskID = uuid.New().String()
	}
	return s.taskID
}

// TaskID returns (or generates) the task ID for this stream.
func (s *SSEWriter) TaskID() string {
	return s.ensureTaskID()
}

// WriteStatus emits a TaskStatusUpdateEvent.
func (s *SSEWriter) WriteStatus(state protocol.TaskState, final bool) error {
	event := protocol.TaskStatusUpdateEvent{
		Kind:   "status",
		TaskID: s.ensureTaskID(),
		Status: protocol.TaskStatus{
			State:     state,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Final: final,
	}
	return s.writeEvent(event)
}

// WriteTextChunk emits a TaskArtifactUpdateEvent with a text chunk.
func (s *SSEWriter) WriteTextChunk(text string, last bool) error {
	lastChunk := last
	event := protocol.TaskArtifactUpdateEvent{
		Kind:   "artifact",
		ID:     uuid.New().String(),
		TaskID: s.ensureTaskID(),
		Artifact: protocol.Artifact{
			Parts:     []protocol.Part{{Kind: "text", Text: text}},
			LastChunk: &lastChunk,
		},
	}
	return s.writeEvent(event)
}

// WriteDataChunk emits a TaskArtifactUpdateEvent with a structured data chunk.
func (s *SSEWriter) WriteDataChunk(data any, last bool) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal data chunk: %w", err)
	}
	lastChunk := last
	event := protocol.TaskArtifactUpdateEvent{
		Kind:   "artifact",
		ID:     uuid.New().String(),
		TaskID: s.ensureTaskID(),
		Artifact: protocol.Artifact{
			Parts:     []protocol.Part{{Kind: "data", Data: raw}},
			LastChunk: &lastChunk,
		},
	}
	return s.writeEvent(event)
}

func (s *SSEWriter) writeEvent(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal sse event: %w", err)
	}
	_, err = fmt.Fprintf(s.w, "data: %s\n\n", data)
	if err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
