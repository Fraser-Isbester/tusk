package connect

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/fraser-isbester/tusk/internal/config"
)

// openKubePortForward starts a `kubectl port-forward` to the profile's target
// and returns a DSN pointing at the local end of the forward.
func openKubePortForward(ctx context.Context, profile config.Profile) (string, func() error, error) {
	c := profile.Connect
	if c.Target == "" {
		return "", nil, fmt.Errorf("kube-port-forward requires a target (e.g. svc/postgres)")
	}
	remotePort := c.RemotePort
	if remotePort == 0 {
		remotePort = 5432
	}
	localPort := c.LocalPort
	if localPort == 0 {
		p, err := freePort()
		if err != nil {
			return "", nil, fmt.Errorf("allocating local port: %w", err)
		}
		localPort = p
	}

	args := kubectlArgs(c, localPort, remotePort)
	dsn := profile.DSN("127.0.0.1", localPort)
	target := fmt.Sprintf("%s:%d", c.Target, remotePort)
	return spawnTunnel(ctx, "kubectl", args, localPort, dsn, target)
}

// kubectlArgs builds the kubectl port-forward argument vector.
func kubectlArgs(c *config.ConnectConfig, localPort, remotePort int) []string {
	args := []string{"port-forward"}
	if c.Context != "" {
		args = append(args, "--context", c.Context)
	}
	if c.Namespace != "" {
		args = append(args, "-n", c.Namespace)
	}
	args = append(args, c.Target, fmt.Sprintf("%d:%d", localPort, remotePort))
	return args
}

// openExec runs an arbitrary tunnel command (SSH, cloud-sql-proxy, …) and
// returns a DSN pointing at the local port. The token {local_port} in the
// command is replaced with the chosen port.
func openExec(ctx context.Context, profile config.Profile) (string, func() error, error) {
	c := profile.Connect
	if len(c.Command) == 0 {
		return "", nil, fmt.Errorf("exec connect method requires a command")
	}
	localPort := c.LocalPort
	if localPort == 0 {
		p, err := freePort()
		if err != nil {
			return "", nil, fmt.Errorf("allocating local port: %w", err)
		}
		localPort = p
	}

	argv := substituteLocalPort(c.Command, localPort)
	dsn := profile.DSN("127.0.0.1", localPort)
	return spawnTunnel(ctx, argv[0], argv[1:], localPort, dsn, argv[0])
}

// substituteLocalPort replaces every {local_port} token in argv with port.
func substituteLocalPort(argv []string, port int) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = strings.ReplaceAll(a, "{local_port}", strconv.Itoa(port))
	}
	return out
}

// spawnTunnel starts cmd, waits for the local port to accept connections, and
// returns the DSN plus a closer that kills the process group. If the process
// exits or the port never opens within establishTimeout, it kills the process
// and returns an error that names the target.
func spawnTunnel(ctx context.Context, name string, args []string, localPort int, dsn, target string) (string, func() error, error) {
	cmd := exec.Command(name, args...) //nolint:gosec // command/args come from the user's own config
	// Run in its own process group so we can kill the whole tree on teardown
	// (kubectl and proxies spawn children).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", nil, fmt.Errorf("starting %s: %w", name, err)
	}

	kill := func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative PID signals the whole process group.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
		return nil
	}

	// Watch for early exit so waitForPort can fail fast with the reason.
	dead := make(chan error, 1)
	go func() { dead <- cmd.Wait() }()

	addr := fmt.Sprintf("127.0.0.1:%d", localPort)
	if err := waitForPort(ctx, addr, establishTimeout, dead); err != nil {
		_ = kill()
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", nil, fmt.Errorf("%s tunnel to %s failed: %w: %s", name, target, err, lastLine(msg))
		}
		return "", nil, fmt.Errorf("%s tunnel to %s failed: %w", name, target, err)
	}

	return dsn, kill, nil
}

// lastLine returns the last non-empty line of s, the most useful part of a
// tool's stderr for an error message.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}
