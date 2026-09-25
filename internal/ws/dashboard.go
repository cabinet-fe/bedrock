package ws

import "encoding/json"

const (
	ChannelDashboardRuns         = "dashboard:runs"
	ChannelDashboardSystemStatus = "dashboard:system-status"
)

// BroadcastRunChanged broadcasts lightweight run status changes to the dashboard run channel.
func (h *Hub) BroadcastRunChanged(runType string, runID uint, status string) {
	if h == nil {
		return
	}
	payload, err := json.Marshal(map[string]interface{}{
		"type":     "run_changed",
		"run_type": runType,
		"run_id":   runID,
		"status":   status,
	})
	if err != nil {
		return
	}
	h.BroadcastToChannel(ChannelDashboardRuns, payload)
}
