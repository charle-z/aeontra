package observability

import (
	"sort"
)

const maxRouteDurationSamples = 256

type RouteSummary struct {
	Route     Route
	Requests  uint64
	Client4XX uint64
	Server5XX uint64
	P95MS     int64
}

type Summary struct {
	Enabled  bool
	Failures uint64
	Routes   []RouteSummary
}

type routeAccumulator struct {
	requests  uint64
	client4XX uint64
	server5XX uint64
	durations []int64
	next      int
}

var summaryRouteOrder = []Route{RouteMCP, RouteConsole, RouteOAuth, RouteHealth, RouteVersion, RouteOther}

func (l *Logger) observeSummary(event Event) {
	if l == nil || event.Name != EventHTTPRequest || event.Route == "" {
		return
	}
	l.summaryMu.Lock()
	defer l.summaryMu.Unlock()
	if l.routeSummary == nil {
		l.routeSummary = make(map[Route]*routeAccumulator, len(summaryRouteOrder))
	}
	accumulator := l.routeSummary[event.Route]
	if accumulator == nil {
		accumulator = &routeAccumulator{durations: make([]int64, 0, maxRouteDurationSamples)}
		l.routeSummary[event.Route] = accumulator
	}
	accumulator.requests++
	if event.StatusCode >= 400 && event.StatusCode < 500 {
		accumulator.client4XX++
	}
	if event.StatusCode >= 500 && event.StatusCode < 600 {
		accumulator.server5XX++
	}
	if len(accumulator.durations) < maxRouteDurationSamples {
		accumulator.durations = append(accumulator.durations, event.DurationMS)
	} else {
		accumulator.durations[accumulator.next] = event.DurationMS
		accumulator.next = (accumulator.next + 1) % maxRouteDurationSamples
	}
}

func (l *Logger) Summary() Summary {
	if l == nil {
		return Summary{Routes: []RouteSummary{}}
	}
	result := Summary{Enabled: l.Enabled(), Failures: l.Failures(), Routes: make([]RouteSummary, 0, len(summaryRouteOrder))}
	l.summaryMu.Lock()
	defer l.summaryMu.Unlock()
	for _, route := range summaryRouteOrder {
		accumulator := l.routeSummary[route]
		if accumulator == nil || accumulator.requests == 0 {
			continue
		}
		durations := append([]int64(nil), accumulator.durations...)
		sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
		p95 := int64(0)
		if len(durations) > 0 {
			index := (95*len(durations) + 99) / 100
			if index < 1 {
				index = 1
			}
			p95 = durations[index-1]
		}
		result.Routes = append(result.Routes, RouteSummary{
			Route: route, Requests: accumulator.requests,
			Client4XX: accumulator.client4XX, Server5XX: accumulator.server5XX, P95MS: p95,
		})
	}
	return result
}
