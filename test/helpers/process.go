package helpers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

var fallbackPort int64 = 19080

func FreePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return int(atomic.AddInt64(&fallbackPort, 1))
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func StartProcess(t *testing.T, binary string, args ...string) context.CancelFunc {
	t.Helper()

	cmd := exec.Command(binary, args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %v: %v", args, err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}

		select {
		case <-done:
			return
		case <-time.After(3 * time.Second):
		}

		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out stopping %v", args)
		}
	}
}

func RunCommand(t *testing.T, binary string, args ...string) string {
	t.Helper()

	cmd := exec.Command(binary, args...)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %v: %v\n%s", args, err, string(output))
	}
	return string(output)
}

func RunCommandFailure(t *testing.T, binary string, args ...string) string {
	t.Helper()

	cmd := exec.Command(binary, args...)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected failure for %v\n%s", args, string(output))
	}
	return string(output)
}

func WaitForHTTP(t *testing.T, target string, host string) {
	t.Helper()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if host != "" {
			req.Host = host
		}

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", fmt.Sprintf("%s host=%s", target, host))
}
