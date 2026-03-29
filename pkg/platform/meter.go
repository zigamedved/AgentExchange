package platform

import (
	"sync"
	"time"
)

// CallRecord is a log entry for a single routed call.
type CallRecord struct {
	ID          string    `json:"id"`
	CallerOrgID string    `json:"caller_org_id"`
	CallerOrg   string    `json:"caller_org"`
	AgentID     string    `json:"agent_id"`
	AgentName   string    `json:"agent_name"`
	AgentOrg    string    `json:"agent_org"`
	Method      string    `json:"method"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	LatencyMS   int64     `json:"latency_ms,omitempty"`
	Status      string    `json:"status"` // "in_flight" | "success" | "error"
	PriceUSD    float64   `json:"price_usd"`
	ErrorMsg    string    `json:"error_msg,omitempty"`
}

// Meter records and queries call statistics.
type Meter struct {
	mu      sync.RWMutex
	records []*CallRecord
	spend   map[string]float64 // key: org ID
}

// NewMeter returns an empty Meter.
func NewMeter() *Meter {
	return &Meter{
		spend: make(map[string]float64),
	}
}

// Start records that a call has begun. Returns the call ID.
func (m *Meter) Start(rec *CallRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec.Status = "in_flight"
	rec.StartedAt = time.Now()
	m.records = append(m.records, rec)
}

// Complete marks a call as finished.
func (m *Meter) Complete(id string, success bool, errMsg string, priceUSD float64) *CallRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if r.ID == id {
			now := time.Now()
			r.CompletedAt = now
			r.LatencyMS = now.Sub(r.StartedAt).Milliseconds()
			r.PriceUSD = priceUSD
			if success {
				r.Status = "success"
			} else {
				r.Status = "error"
				r.ErrorMsg = errMsg
			}
			m.spend[r.CallerOrgID] += priceUSD
			return r
		}
	}
	return nil
}

// Recent returns the last n call records, newest first.
func (m *Meter) Recent(n int) []*CallRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if n > len(m.records) {
		n = len(m.records)
	}
	out := make([]*CallRecord, n)
	// copy from tail
	for i, j := len(m.records)-n, 0; i < len(m.records); i, j = i+1, j+1 {
		out[j] = m.records[i]
	}
	// reverse for newest-first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// SpendByOrg returns cumulative spend per organization.
func (m *Meter) SpendByOrg() map[string]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]float64, len(m.spend))
	for k, v := range m.spend {
		out[k] = v
	}
	return out
}
