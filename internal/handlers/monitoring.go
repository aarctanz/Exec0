package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/hibiken/asynq"
)

type MonitoringHandler struct {
	inspector *asynq.Inspector
}

func NewMonitoringHandler(redisAddr string) *MonitoringHandler {
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	return &MonitoringHandler{
		inspector: asynq.NewInspector(asynq.RedisClientOpt{Addr: redisAddr}),
	}
}

type queueStats struct {
	Queue          string `json:"queue"`
	Size           int    `json:"size"`
	Pending        int    `json:"pending"`
	Active         int    `json:"active"`
	Scheduled      int    `json:"scheduled"`
	Retry          int    `json:"retry"`
	Archived       int    `json:"archived"`
	Completed      int    `json:"completed"`
	Processed      int    `json:"processed"`
	Failed         int    `json:"failed"`
	ProcessedTotal int    `json:"processed_total"`
	FailedTotal    int    `json:"failed_total"`
	LatencyMs      int64  `json:"latency_ms"`
	MemoryUsage    int64  `json:"memory_usage"`
	Paused         bool   `json:"paused"`
}

// Queues returns queue info for all queues.
func (h *MonitoringHandler) Queues(w http.ResponseWriter, r *http.Request) {
	queues, err := h.inspector.Queues()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var stats []queueStats
	for _, q := range queues {
		info, err := h.inspector.GetQueueInfo(q)
		if err != nil {
			continue
		}
		stats = append(stats, queueStats{
			Queue:          info.Queue,
			Size:           info.Size,
			Pending:        info.Pending,
			Active:         info.Active,
			Scheduled:      info.Scheduled,
			Retry:          info.Retry,
			Archived:       info.Archived,
			Completed:      info.Completed,
			Processed:      info.Processed,
			Failed:         info.Failed,
			ProcessedTotal: info.ProcessedTotal,
			FailedTotal:    info.FailedTotal,
			LatencyMs:      info.Latency.Milliseconds(),
			MemoryUsage:    info.MemoryUsage,
			Paused:         info.Paused,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

type dailyStats struct {
	Date      string `json:"date"`
	Processed int    `json:"processed"`
	Failed    int    `json:"failed"`
}

// History returns daily stats for a queue (last 14 days).
func (h *MonitoringHandler) History(w http.ResponseWriter, r *http.Request) {
	queue := r.URL.Query().Get("queue")
	if queue == "" {
		queue = "default"
	}

	history, err := h.inspector.History(queue, 14)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var result []dailyStats
	for _, h := range history {
		result = append(result, dailyStats{
			Date:      h.Date.Format("2006-01-02"),
			Processed: h.Processed,
			Failed:    h.Failed,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// Close releases inspector resources.
func (h *MonitoringHandler) Close() error {
	return h.inspector.Close()
}
