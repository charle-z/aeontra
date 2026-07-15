package mcpserver

import "sync/atomic"

type payloadSnapshot struct {
	RequestCount uint64
	InputBytes   uint64
	OutputBytes  uint64
}

type payloadCounters struct {
	requestCount atomic.Uint64
	inputBytes   atomic.Uint64
	outputBytes  atomic.Uint64
}

func (c *payloadCounters) record(inputBytes, outputBytes int) {
	if c == nil {
		return
	}
	c.requestCount.Add(1)
	if inputBytes > 0 {
		c.inputBytes.Add(uint64(inputBytes))
	}
	if outputBytes > 0 {
		c.outputBytes.Add(uint64(outputBytes))
	}
}

func (c *payloadCounters) snapshot() payloadSnapshot {
	if c == nil {
		return payloadSnapshot{}
	}
	return payloadSnapshot{
		RequestCount: c.requestCount.Load(),
		InputBytes:   c.inputBytes.Load(),
		OutputBytes:  c.outputBytes.Load(),
	}
}
