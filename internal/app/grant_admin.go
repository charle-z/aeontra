package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charle-z/mcp-devbox/internal/grantadmin"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

type localGrantAdmin struct {
	BaseURL        string
	Token          string
	DescriptorPath string
	shutdown       func(context.Context) error
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
	baseURL := "http://" + addr
	descriptorPath, err := writeGrantAdminDescriptor(runtime.StateRoot, baseURL, token)
	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(ctx)
		return nil, fmt.Errorf("persisting private grant admin descriptor: %w", err)
	}
	admin := &localGrantAdmin{BaseURL: baseURL, Token: token, DescriptorPath: descriptorPath, shutdown: shutdown}
	runtime.Policy.AccessGrants().SetNotifier(func(req policy.AccessRequest) {
		rawFlag := ""
		if req.RawRequested {
			rawFlag = " --raw --confirm-raw"
		}
		fmt.Fprintf(output, "\nACCESS REQUIRED request_id=%s raw_requested=%t path=%s\n",
			req.ID, req.RawRequested, req.Path)
		fmt.Fprintf(output, "Approve from a local operator shell: mcp-devbox grant --admin-file %q --ttl 5m%s %s\n\n",
			admin.DescriptorPath, rawFlag, req.ID)
	})
	fmt.Fprintf(output, "Local grant admin channel ready: descriptor=%q (loopback only; bearer is not printed)\n", admin.DescriptorPath)
	return admin, nil
}

func (a *localGrantAdmin) Close() error {
	if a == nil || a.shutdown == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	shutdownErr := a.shutdown(ctx)
	removeErr := removeGrantAdminDescriptor(a.DescriptorPath, a.Token)
	return errors.Join(shutdownErr, removeErr)
}

func removeGrantAdminDescriptor(path, token string) error {
	descriptor, err := readGrantAdminDescriptor(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if descriptor.Token != token {
		return fmt.Errorf("grant admin descriptor identity changed")
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing grant admin descriptor: %w", err)
	}
	return nil
}

func randomHexToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
