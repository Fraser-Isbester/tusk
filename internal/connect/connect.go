// Package connect turns a connection profile into a reachable PostgreSQL DSN.
//
// Tusk is a dev tool and the databases worth watching usually live inside a VPC
// with no public endpoint. A profile's connect block declares how to reach it —
// directly, or by having tusk establish a tunnel first (a kubectl port-forward
// into a cluster, or an arbitrary command such as an SSH tunnel or the Cloud SQL
// Auth Proxy). Open establishes the tunnel, returns a DSN pointing at the local
// endpoint, and hands back a closer that tears the tunnel down on exit.
//
// The package has no TUI dependency so both the tusk TUI and the tuskd daemon
// use it.
package connect

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/fraser-isbester/tusk/internal/config"
)

// establishTimeout bounds how long we wait for a tunnel's local port to start
// accepting connections before giving up.
const establishTimeout = 15 * time.Second

// Open resolves profile into a DSN that is reachable from localhost, starting a
// tunnel first when the profile's connect method requires one. The returned
// closer must be called when the connection is no longer needed; for the direct
// method it is a no-op.
func Open(ctx context.Context, profile config.Profile) (dsn string, closer func() error, err error) {
	method := "direct"
	if profile.Connect != nil && profile.Connect.Via != "" {
		method = profile.Connect.Via
	}

	switch method {
	case "direct":
		return profile.ConnectionString(), noopCloser, nil
	case "kube-port-forward":
		return openKubePortForward(ctx, profile)
	case "exec":
		return openExec(ctx, profile)
	default:
		return "", nil, fmt.Errorf("unknown connect method %q (want direct, kube-port-forward, or exec)", method)
	}
}

func noopCloser() error { return nil }

// freePort asks the OS for an unused TCP port by binding to :0 and releasing it.
// There is an inherent race between release and reuse, but it is the standard
// approach and the window is tiny for a short-lived local forward.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	if cerr := l.Close(); cerr != nil {
		return 0, cerr
	}
	return port, nil
}

// waitForPort blocks until a TCP connection to addr succeeds or the deadline
// passes. It also aborts if ctx is canceled or dead reports the tunnel process
// exited early (returning that as the error, so callers surface why).
func waitForPort(ctx context.Context, addr string, timeout time.Duration, dead <-chan error) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case derr := <-dead:
			if derr != nil {
				return fmt.Errorf("tunnel process exited: %w", derr)
			}
			return fmt.Errorf("tunnel process exited before %s became reachable", addr)
		case <-ticker.C:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for %s", timeout, addr)
		}
	}
}
