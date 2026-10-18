package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/charle-z/mcp-devbox/internal/grantadmin"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

type localGrantAdmin struct {
	BaseURL  string
	Token    string
	shutdown func(context.Context) error
}

func startLocalGrantAdmin(runtime *appRuntime, output io.Writer) (*localGrantAdmin, error) {
	token, err := randomHexToken()
	if err != nil {
		return nil, fmt.Errorf("creating admin token: %w", err)
	}
	addr, shutdown, err := grantadmin.Start("127.0.0.1:0", token, runtime.Policy, runtime.Logger)
	if err != nil {
		return nil, fmt.Errorf("starting local grant admin channel: %w", err)
	}
	admin := &localGrantAdmin{BaseURL: "http://" + addr, Token: token, shutdown: shutdown}
	runtime.Policy.AccessGrants().SetNotifier(func(req policy.AccessRequest) {
		rawFlag := ""
		if req.RawRequested {
			rawFlag = " --raw --confirm-raw"
		}
		fmt.Fprintf(output, "\nACCESS REQUIRED request_id=%s raw_requested=%t path=%s\n",
			req.ID, req.RawRequested, req.Path)
		fmt.Fprintf(output, "Approve locally: mcp-devbox grant --admin %s --admin-token %s --ttl 5m%s %s\n\n",
			admin.BaseURL, admin.Token, rawFlag, req.ID)
	})
	fmt.Fprintf(output, "Local grant admin channel: %s (loopback only; token printed for local human approval)\n", admin.BaseURL)
	return admin, nil
}

func (a *localGrantAdmin) Close() error {
	if a == nil || a.shutdown == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return a.shutdown(ctx)
}

func randomHexToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
