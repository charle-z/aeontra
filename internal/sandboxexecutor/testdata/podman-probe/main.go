package main

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

func main() {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil || !strings.Contains(string(status), "CapEff:\t0000000000000000") || !strings.Contains(string(status), "NoNewPrivs:\t1") {
		fail("process security posture is invalid")
	}
	if connection, err := net.DialTimeout("tcp", "1.1.1.1:80", 250*time.Millisecond); err == nil {
		_ = connection.Close()
		fail("network=none allowed external traffic")
	}
	if err := os.WriteFile("/tmp/probe", []byte("tmp-write-ok\n"), 0o600); err != nil {
		fail("private tmpfs is not writable")
	}
	if err := os.WriteFile("/workspace/probe.txt", []byte("workspace-write-ok\n"), 0o600); err != nil {
		fail("workspace bind is not writable")
	}
	_, _ = fmt.Fprintln(os.Stdout, "probe-stdout")
	_, _ = fmt.Fprintln(os.Stderr, "probe-stderr")
}

func fail(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
