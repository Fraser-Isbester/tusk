package connect

import (
	"context"
	"net"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fraser-isbester/tusk/internal/config"
)

var _ = Describe("Open", func() {
	It("returns the profile DSN unchanged for the direct method", func() {
		p := config.Profile{URL: "postgres://u:p@localhost:5432/db"} //nolint:gosec // test fixture
		dsn, closer, err := Open(context.Background(), p)
		Expect(err).NotTo(HaveOccurred())
		Expect(dsn).To(Equal("postgres://u:p@localhost:5432/db"))
		Expect(closer()).To(Succeed())
	})

	It("treats an empty connect block as direct", func() {
		p := config.Profile{URL: "postgres://localhost/db", Connect: &config.ConnectConfig{}}
		dsn, _, err := Open(context.Background(), p)
		Expect(err).NotTo(HaveOccurred())
		Expect(dsn).To(Equal("postgres://localhost/db"))
	})

	It("rejects an unknown method", func() {
		p := config.Profile{Connect: &config.ConnectConfig{Via: "carrier-pigeon"}}
		_, _, err := Open(context.Background(), p)
		Expect(err).To(MatchError(ContainSubstring("unknown connect method")))
	})

	It("errors when kube-port-forward has no target", func() {
		p := config.Profile{Connect: &config.ConnectConfig{Via: "kube-port-forward"}}
		_, _, err := Open(context.Background(), p)
		Expect(err).To(MatchError(ContainSubstring("requires a target")))
	})

	It("errors when exec has no command", func() {
		p := config.Profile{Connect: &config.ConnectConfig{Via: "exec"}}
		_, _, err := Open(context.Background(), p)
		Expect(err).To(MatchError(ContainSubstring("requires a command")))
	})

	Describe("exec method", func() {
		It("connects once the local port is reachable and tears the process down", func() {
			// Pre-open a listener to stand in for the far end of a tunnel, and
			// use a long-lived `sleep` as the tunnel process. spawnTunnel should
			// see the port is reachable and return a DSN + working closer.
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = ln.Close() }()
			port := ln.Addr().(*net.TCPAddr).Port

			p := config.Profile{
				User: "readonly", Database: "appdb",
				Connect: &config.ConnectConfig{Via: "exec", LocalPort: port, Command: []string{"sleep", "30"}},
			}
			dsn, closer, err := Open(context.Background(), p)
			Expect(err).NotTo(HaveOccurred())
			Expect(dsn).To(ContainSubstring("readonly@127.0.0.1:"))
			Expect(dsn).To(ContainSubstring("/appdb?sslmode=disable"))
			Expect(closer()).To(Succeed())
		})

		It("fails fast with the tunnel's exit when the command dies early", func() {
			p := config.Profile{
				Connect: &config.ConnectConfig{Via: "exec", Command: []string{"sh", "-c", "exit 3"}},
			}
			_, _, err := Open(context.Background(), p)
			Expect(err).To(MatchError(ContainSubstring("tunnel process exited")))
		})
	})
})

var _ = Describe("helpers", func() {
	Describe("kubectlArgs", func() {
		It("includes context and namespace when set", func() {
			c := &config.ConnectConfig{Context: "ctx", Namespace: "ns", Target: "svc/pg"}
			Expect(kubectlArgs(c, 5000, 5432)).To(Equal(
				[]string{"port-forward", "--context", "ctx", "-n", "ns", "svc/pg", "5000:5432"}))
		})
		It("omits context and namespace when empty", func() {
			c := &config.ConnectConfig{Target: "pod/pg"}
			Expect(kubectlArgs(c, 6000, 5432)).To(Equal(
				[]string{"port-forward", "pod/pg", "6000:5432"}))
		})
	})

	Describe("substituteLocalPort", func() {
		It("replaces the token everywhere", func() {
			out := substituteLocalPort([]string{"ssh", "-L", "{local_port}:db:5432", "x{local_port}"}, 42)
			Expect(out).To(Equal([]string{"ssh", "-L", "42:db:5432", "x42"}))
		})
	})

	Describe("freePort", func() {
		It("returns a bindable port", func() {
			port, err := freePort()
			Expect(err).NotTo(HaveOccurred())
			Expect(port).To(BeNumerically(">", 0))
			ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			Expect(err).NotTo(HaveOccurred())
			_ = ln.Close()
		})
	})

	Describe("waitForPort", func() {
		It("returns nil once the address is reachable", func() {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = ln.Close() }()
			err = waitForPort(context.Background(), ln.Addr().String(), 2*time.Second, make(chan error))
			Expect(err).NotTo(HaveOccurred())
		})

		It("times out when nothing is listening", func() {
			err := waitForPort(context.Background(), "127.0.0.1:1", 300*time.Millisecond, make(chan error))
			Expect(err).To(MatchError(ContainSubstring("timed out")))
		})

		It("fails fast when the tunnel process dies", func() {
			dead := make(chan error, 1)
			dead <- nil
			err := waitForPort(context.Background(), "127.0.0.1:1", 5*time.Second, dead)
			Expect(err).To(MatchError(ContainSubstring("exited")))
		})
	})
})
