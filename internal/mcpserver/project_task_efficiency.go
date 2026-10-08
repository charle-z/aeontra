package mcpserver

import "context"

func (s *Server) attachProjectTaskEfficiency(ctx context.Context, view *projectTaskView) {
	for i := range view.Workers {
		worker := &view.Workers[i]
		worker.EfficiencyState = "unavailable"
		if s.modelTurns == nil || worker.RuntimeID == "" {
			continue
		}
		metrics, err := s.modelTurns.RuntimeEfficiency(ctx, worker.RuntimeID)
		if err != nil {
			continue
		}
		worker.Efficiency, worker.EfficiencyState = &metrics, "observed"
	}
}
