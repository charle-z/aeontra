package tools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/resultstore"
)

var ErrResultStoreNotConfigured = errors.New("result store is not configured")

func (c *ResultCapability) acquire() (*resultstore.Store, func(), error) {
	if c == nil {
		return nil, func() {}, ErrResultStoreNotConfigured
	}
	c.mu.RLock()
	if c.store == nil {
		c.mu.RUnlock()
		return nil, func() {}, ErrResultStoreNotConfigured
	}
	return c.store, c.mu.RUnlock, nil
}

func finishResultSpan(span *audit.Span, operation string, err error) {
	decision := audit.Allow
	if err != nil {
		decision = audit.Error
		if errors.Is(err, ErrResultStoreNotConfigured) {
			decision = audit.Deny
		}
	}
	span.Finish(decision, operation, nil, err)
}

func encodeResultValue(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", errors.New("result encoding failed")
	}
	return string(encoded), nil
}

// ResultRead returns one bounded fragment by opaque reference. The reference is
// deliberately absent from audit arguments.
func (c *ResultCapability) ResultRead(ref string, offset int64, maxBytes int) (output string, err error) {
	span := c.log.Start("result_read")
	defer func() { finishResultSpan(span, fmt.Sprintf("offset=%d max_bytes=%d", offset, maxBytes), err) }()
	store, release, err := c.acquire()
	if err != nil {
		return "", err
	}
	defer release()
	value, err := store.Read(ref, offset, maxBytes)
	if err != nil {
		return "", err
	}
	return encodeResultValue(value)
}

// ResultFind performs bounded exact substring search without auditing the query.
func (c *ResultCapability) ResultFind(query string, limit int) (output string, err error) {
	span := c.log.Start("result_find")
	defer func() { finishResultSpan(span, fmt.Sprintf("limit=%d", limit), err) }()
	store, release, err := c.acquire()
	if err != nil {
		return "", err
	}
	defer release()
	value, err := store.FindExact(query, limit)
	if err != nil {
		return "", err
	}
	return encodeResultValue(value)
}

// ResultStage returns one bounded stage fragment by index and opaque reference.
func (c *ResultCapability) ResultStage(ref string, stage, maxBytes int) (output string, err error) {
	span := c.log.Start("result_stage")
	defer func() { finishResultSpan(span, fmt.Sprintf("stage=%d max_bytes=%d", stage, maxBytes), err) }()
	store, release, err := c.acquire()
	if err != nil {
		return "", err
	}
	defer release()
	value, err := store.ReadStage(ref, stage, maxBytes)
	if err != nil {
		return "", err
	}
	return encodeResultValue(value)
}

// StageToolResult persists a large already-redacted MCP result and returns compact
// metadata. It reports configured=false when compatibility mode has no store.
func (c *ResultCapability) StageToolResult(tool, content string, failed bool) (metadata string, configured bool, err error) {
	store, release, acquireErr := c.acquire()
	if acquireErr != nil {
		if errors.Is(acquireErr, ErrResultStoreNotConfigured) {
			return "", false, nil
		}
		return "", true, acquireErr
	}
	defer release()
	status := resultstore.StatusSuccess
	exitStatus := 0
	summary := "large tool result stored"
	if failed {
		status = resultstore.StatusFailure
		exitStatus = 1
		summary = "large failing tool result stored"
	}
	value, err := store.Put(resultstore.Input{
		Status: status, Summary: summary, ExitStatus: exitStatus, Content: content,
		Stages: []resultstore.StageInput{{Name: tool, Status: status}},
	})
	if err != nil {
		return "", true, err
	}
	encoded, err := encodeResultValue(value)
	return encoded, true, err
}
