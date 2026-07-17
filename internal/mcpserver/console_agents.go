package mcpserver

import (
	"context"
	"sort"
	"time"

	"github.com/charle-z/mcp-devbox/internal/console"
)

const (
	controllerConnectedWindow = 45 * time.Second
	controllerStaleWindow     = 2 * time.Minute
)

type controllerAccumulator struct {
	data console.ControllerData
	seen time.Time
}

func (s *Server) consoleAgentState(ctx context.Context) ([]console.ControllerData, []console.RuntimeData, error) {
	controllers := make(map[string]*controllerAccumulator)
	if s.journal != nil {
		activity, err := s.journal.ControllerActivity()
		if err != nil {
			return nil, nil, err
		}
		for _, item := range activity {
			controllers[item.Controller] = &controllerAccumulator{
				data: console.ControllerData{Kind: item.Controller, ActiveOperations: item.ActiveOperations},
				seen: item.LastSeenAt,
			}
		}
	}

	runtimes := make([]console.RuntimeData, 0)
	if s.modelTurns != nil {
		items, err := s.modelTurns.ConsoleRuntimes(ctx, 50)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range items {
			controller := string(item.Controller)
			entry := controllers[controller]
			if entry == nil {
				entry = &controllerAccumulator{data: console.ControllerData{Kind: controller}}
				controllers[controller] = entry
			}
			if item.LastActivity.After(entry.seen) {
				entry.seen = item.LastActivity
			}
			if item.Active {
				entry.data.ActiveRuntimes++
			}
			runtimes = append(runtimes, console.RuntimeData{
				RuntimeID: item.RuntimeID, State: string(item.State), Controller: controller,
				LastActivity: item.LastActivity.Format(time.RFC3339),
			})
		}
	}

	now := time.Now().UTC()
	result := make([]console.ControllerData, 0, len(controllers))
	for _, entry := range controllers {
		entry.data.State = deriveControllerState(now, entry.seen, entry.data.ActiveOperations+entry.data.ActiveRuntimes)
		if !entry.seen.IsZero() {
			entry.data.LastSeenAt = entry.seen.Format(time.RFC3339)
		}
		result = append(result, entry.data)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Kind < result[j].Kind })
	sort.Slice(runtimes, func(i, j int) bool {
		if runtimes[i].LastActivity == runtimes[j].LastActivity {
			return runtimes[i].RuntimeID > runtimes[j].RuntimeID
		}
		return runtimes[i].LastActivity > runtimes[j].LastActivity
	})
	return result, runtimes, nil
}

func deriveControllerState(now, lastSeen time.Time, active int64) string {
	if lastSeen.IsZero() {
		return "disconnected"
	}
	age := now.Sub(lastSeen)
	if age < 0 {
		age = 0
	}
	if age <= controllerConnectedWindow && active > 0 {
		return "connected"
	}
	if age <= controllerStaleWindow {
		return "stale"
	}
	return "disconnected"
}
