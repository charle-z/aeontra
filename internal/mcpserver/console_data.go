package mcpserver

import (
	"bufio"
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/console"
	"github.com/charle-z/mcp-devbox/internal/oauth"
	"github.com/charle-z/mcp-devbox/internal/telemetry"
)

func (s *Server) consoleDataProvider(staticToken string, oauthProvider *oauth.Provider, edgeState func() string) console.DataProvider {
	return func(ctx context.Context) (console.DataSnapshot, error) {
		payload := s.payload.snapshot()
		snapshot := console.DataSnapshot{
			System: readSystemData(),
			Payload: console.PayloadData{
				ProcessStartedAt:       s.startedAt.Format(time.RFC3339),
				RequestCount:           payload.RequestCount,
				ToolCallCount:          payload.ToolCalls,
				InputBytes:             payload.InputBytes,
				OutputBytes:            payload.OutputBytes,
				EstimatedPayloadTokens: (payload.InputBytes + payload.OutputBytes) / 4,
				InputTokensEstimate:    payload.InputBytes / 4,
				OutputTokensEstimate:   payload.OutputBytes / 4,
				Formula:                "bytes / 4 (estimate)",
				Warning:                "estimate, not provider billing",
			},
			Security: console.SecurityData{
				OAuthEnabled:     oauthProvider != nil,
				BearerRecovery:   staticToken != "",
				QueryAuth:        "rejected",
				FreeShell:        "absent",
				Cookie:           "Secure; HttpOnly; SameSite=Strict",
				ConsoleAuthority: "presentation-only",
			},
			Projects:      s.consoleProjects(),
			Storage:       readConsoleStorageBudget(s.stateRoot, s.auditPath),
			Edge:          console.EdgeData{State: "not_paired", Devices: []console.EdgeDeviceData{}},
			Brain:         console.BrainData{Nodes: []console.BrainNode{}, Edges: []console.BrainEdge{}},
			Observability: console.ObservabilityData{Routes: []console.ObservabilityRoute{}},
		}
		controllers, runtimes, err := s.consoleAgentState(ctx)
		if err != nil {
			return console.DataSnapshot{}, err
		}
		snapshot.Controllers = controllers
		snapshot.Runtimes = runtimes

		if s.telemetry != nil {
			activity, err := s.telemetry.Activity()
			if err != nil {
				return console.DataSnapshot{}, err
			}
			snapshot.DurableActivity = console.DurableActivityData{
				Last24Hours: activityWindowData(activity.Last24Hours), Last7Days: activityWindowData(activity.Last7Days),
				Last30Days: activityWindowData(activity.Last30Days), Last90Days: activityWindowData(activity.Last90Days),
				Lifetime: activityWindowData(activity.Lifetime),
			}
		}
		if edgeState != nil {
			switch state := edgeState(); state {
			case "paired", "not_paired", "unavailable":
				snapshot.Edge.State = state
			}
		}
		devices, edgeErr := s.consoleEdgeDevices()
		if edgeErr != nil {
			snapshot.Edge.State = "unavailable"
		} else {
			snapshot.Edge.Devices = devices
		}

		if s.observer != nil {
			summary := s.observer.Summary()
			snapshot.Observability.Enabled = summary.Enabled
			snapshot.Observability.Failures = summary.Failures
			for _, route := range summary.Routes {
				snapshot.Observability.Routes = append(snapshot.Observability.Routes, console.ObservabilityRoute{
					Route: string(route.Route), Requests: route.Requests,
					Client4XX: route.Client4XX, Server5XX: route.Server5XX, P95MS: route.P95MS,
				})
			}
		}

		if s.svc != nil && s.svc.BrainCapability != nil && s.svc.BrainCapability.Available() {
			snapshot.Brain.Available = true
			brainSnapshot, err := s.svc.BrainCapability.BrainConsoleSnapshot(ctx)
			if err == nil {
				status := brainSnapshot.Status
				snapshot.Brain.Ready = status.Ready
				snapshot.Brain.SchemaVersion = status.SchemaVersion
				snapshot.Brain.NoteCount = status.NoteCount
				snapshot.Brain.SourceBytes = status.SourceBytes
				snapshot.Brain.LinkCount = status.LinkCount
				snapshot.Brain.BrokenLinkCount = status.BrokenLinkCount
				snapshot.Brain.IndexedAt = status.IndexedAt
				snapshot.Brain.GraphTruncated = brainSnapshot.GraphTruncated
				for _, node := range brainSnapshot.Nodes {
					snapshot.Brain.Nodes = append(snapshot.Brain.Nodes, console.BrainNode{
						ID: node.ID, ConsoleLabel: node.ConsoleLabel, Title: node.Title, Summary: node.Summary,
						Trust: string(node.Trust), Degree: node.Degree,
					})
				}
				for _, edge := range brainSnapshot.Edges {
					snapshot.Brain.Edges = append(snapshot.Brain.Edges, console.BrainEdge{
						Source: edge.Source, Target: edge.Target,
					})
				}
			}
		}
		return snapshot, nil
	}
}

func activityWindowData(snapshot telemetry.Snapshot) console.ActivityWindowData {
	updatedAt := ""
	if snapshot.UpdatedAt > 0 {
		updatedAt = time.Unix(snapshot.UpdatedAt, 0).UTC().Format(time.RFC3339)
	}
	return console.ActivityWindowData{
		Requests: snapshot.RequestCount, ToolCalls: snapshot.ToolCallCount,
		InputBytes: snapshot.InputBytes, OutputBytes: snapshot.OutputBytes,
		EstimatedPayloadTokens: (snapshot.InputBytes + snapshot.OutputBytes) / 4,
		ClientErrors:           snapshot.ClientErrors, ServerErrors: snapshot.ServerErrors,
		ExternalWaitMS: snapshot.ExternalWaitMS, UpdatedAt: updatedAt,
	}
}

func readSystemData() console.SystemData {
	data := console.SystemData{CPUCount: runtime.NumCPU()}
	memoryOK := readMemory(&data)
	loadOK := readLoad(&data)
	diskOK := readDisk(&data)
	data.Available = data.CPUCount > 0 && memoryOK && loadOK && diskOK
	return data
}

func readMemory(data *console.SystemData) bool {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return false
	}
	defer file.Close()
	values := map[string]uint64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSuffix(fields[0], ":")
		if name != "MemTotal" && name != "MemAvailable" {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			return false
		}
		values[name] = value * 1024
	}
	if scanner.Err() != nil || values["MemTotal"] == 0 {
		return false
	}
	data.MemoryTotalBytes = values["MemTotal"]
	data.MemoryAvailableBytes = values["MemAvailable"]
	return true
}

func readLoad(data *console.SystemData) bool {
	body, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return false
	}
	fields := strings.Fields(string(body))
	if len(fields) < 3 {
		return false
	}
	values := []*float64{&data.Load1, &data.Load5, &data.Load15}
	for index := range values {
		parsed, parseErr := strconv.ParseFloat(fields[index], 64)
		if parseErr != nil {
			return false
		}
		*values[index] = parsed
	}
	return true
}
