package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

func TestProjectDevelopmentCancellationObservesFailedStopWithoutReplaying(t *testing.T) {
	for _, mode := range []string{"stopped", "running-then-stopped", "lost-status-ack", "recovered-start", "pending", "unknown-exit", "failed-status", "foreign-status", "foreign-stop"} {
		t.Run(mode, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			if mode == "recovered-start" {
				edges.interruptedStart = true
			}
			statusCount := 0
			observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
				switch op.Kind {
				case edge.OperationProjectProcessStop:
					op.State = edge.OperationFailed
					op.SafeCode = "project_process_failed"
					op.Result = edge.OperationResult{}
					if mode == "foreign-stop" {
						op.Request.Alias = "foreign"
					}
				case edge.OperationProjectProcessStatus:
					statusCount++
					op.Result.BackgroundProcessState = "stopped"
					op.Result.BackgroundExitKnown = true
					op.Result.BackgroundExitCode = 137
					switch mode {
					case "running-then-stopped":
						if statusCount == 1 {
							op.Result.BackgroundProcessState = "running"
							op.Result.BackgroundExitKnown = false
						}
					case "pending":
						op.State = edge.OperationQueued
					case "unknown-exit":
						op.Result.BackgroundExitKnown = false
					case "failed-status":
						op.State = edge.OperationFailed
						op.Result = edge.OperationResult{}
					case "foreign-status":
						op.Result.BackgroundProcessID = "pr_" + strings.Repeat("f", 32)
					}
				}
				return op
			}}
			server.WithEdgeStore(observer)
			view := developmentStart(t, server, "cancel-observe-001")
			developmentRounds(t, server, 2)
			if mode == "lost-status-ack" {
				observer.lostACK = edge.OperationProjectProcessStatus
			}
			if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + view.RequestID + `"}`)); err != nil {
				t.Fatal(err)
			}
			for range 8 {
				_ = server.reconcileDevelopmentRequestsOnce(context.Background())
			}
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			want := workqueue.DevelopmentRequestCancelling
			if mode == "stopped" || mode == "running-then-stopped" || mode == "lost-status-ack" || mode == "recovered-start" {
				want = workqueue.DevelopmentRequestCancelled
			}
			if request.State != want {
				t.Fatalf("state=%s want=%s status_reads=%d", request.State, want, statusCount)
			}
			stops := 0
			for _, op := range edges.operations {
				if op.Kind == edge.OperationProjectProcessStop {
					stops++
				}
			}
			wantReads := 1
			if mode == "running-then-stopped" {
				wantReads = 2
			} else if mode == "foreign-stop" {
				wantReads = 0
			}
			if edges.starts != 1 || stops != 1 || statusCount != wantReads {
				t.Fatalf("starts=%d stops=%d status_reads=%d want_reads=%d", edges.starts, stops, statusCount, wantReads)
			}
		})
	}
}

func TestProjectDevelopmentCancellationDoesNotMistakeOrdinaryStatusForStop(t *testing.T) {
	server, edges, _ := developmentServer(t)
	observer := &developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
		if op.Kind == edge.OperationProjectProcessStatus {
			op.State = edge.OperationQueued
		}
		return op
	}}
	server.WithEdgeStore(observer)
	view := developmentStart(t, server, "cancel-existing-read-001")
	developmentRounds(t, server, 3)
	if _, err := server.handleProjectDevelopmentCancel(json.RawMessage(`{"request_id":"` + view.RequestID + `"}`)); err != nil {
		t.Fatal(err)
	}
	developmentRounds(t, server, 2)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	if request.State != workqueue.DevelopmentRequestCancelled {
		t.Fatalf("ordinary pending read prevented cancellation: %+v", request)
	}
	stops := 0
	for _, op := range edges.operations {
		if op.Kind == edge.OperationProjectProcessStop {
			stops++
		}
	}
	if edges.starts != 1 || stops != 1 {
		t.Fatalf("starts=%d stops=%d", edges.starts, stops)
	}
}
