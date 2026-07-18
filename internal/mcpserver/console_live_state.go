package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/console"
	"github.com/charle-z/mcp-devbox/internal/edge"
)

type edgeConsoleRegistry interface {
	ActiveDevices() ([]edge.Device, error)
}

func (s *Server) WithConsoleStorageRoots(stateRoot, auditPath string) *Server {
	if s == nil {
		return s
	}
	s.stateRoot = strings.TrimSpace(stateRoot)
	s.auditPath = strings.TrimSpace(auditPath)
	return s
}

func (s *Server) consoleCurrentProjectID() string {
	if s == nil || s.svc == nil {
		return ""
	}
	index := s.svc.ConsoleCurrentProjectIndex()
	if index < 0 {
		return ""
	}
	return opaqueConsoleSelector("prj_", "project", strconv.Itoa(index))
}

func (s *Server) consoleProjects() []console.ProjectData {
	count := 0
	if s != nil && s.svc != nil {
		count = s.svc.ConsoleProjectCount()
	}
	projects := make([]console.ProjectData, 0, count)
	current := -1
	if s != nil && s.svc != nil {
		current = s.svc.ConsoleCurrentProjectIndex()
	}
	for index := 0; index < count; index++ {
		projects = append(projects, console.ProjectData{
			ID:      opaqueConsoleSelector("prj_", "project", strconv.Itoa(index)),
			Label:   "Configured project " + strconv.Itoa(index+1),
			Current: index == current,
		})
	}
	return projects
}

func (s *Server) consoleEdgeDevices() ([]console.EdgeDeviceData, error) {
	registry, ok := s.edgeDevices.(edgeConsoleRegistry)
	if !ok || registry == nil {
		return []console.EdgeDeviceData{}, nil
	}
	devices, err := registry.ActiveDevices()
	if err != nil {
		return nil, err
	}
	result := make([]console.EdgeDeviceData, 0, len(devices))
	for index, device := range devices {
		pairedAt := ""
		if !device.PairedAt.IsZero() {
			pairedAt = device.PairedAt.UTC().Format(time.RFC3339Nano)
		}
		result = append(result, console.EdgeDeviceData{
			ID:       s.consoleEdgeID(device.ID),
			Label:    "Paired Edge " + strconv.Itoa(index+1),
			PairedAt: pairedAt,
		})
	}
	return result, nil
}

func (s *Server) consoleEdgeID(deviceID string) string {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return ""
	}
	return opaqueConsoleSelector("edge_", "edge-device", deviceID)
}

func opaqueConsoleSelector(prefix, domain, value string) string {
	digest := sha256.Sum256([]byte("mcp-devbox:console-selector:v1\x00" + domain + "\x00" + value))
	return prefix + hex.EncodeToString(digest[:12])
}
