package helpers

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/abcdlsj/gnar/test/common"
)

func StartProcess(name string, args ...string) (func() error, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return func() error {
		return cmd.Process.Kill()
	}, nil
}

func StartGnarServer(port string, mux bool) (func() error, error) {
	args := []string{"server", port}
	if mux {
		args = append(args, "-m")
	}
	println("Starting gnar server with path:", common.GnarPath)
	return StartProcess(common.GnarPath, args...)
}

func StartGnarClient(serverAddr, portMapping string, mux bool) (func() error, error) {
	args := []string{"client", serverAddr, portMapping}
	if mux {
		args = append(args, "-m")
	}
	println("Starting gnar client with path:", common.GnarPath)
	return StartProcess(common.GnarPath, args...)
}

func StartPythonServer() (func() error, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	stopFunc, err := StartProcess("python3", "-m", "http.server", fmt.Sprintf("%d", port))
	if err != nil {
		return nil, "", err
	}

	return stopFunc, fmt.Sprintf("%d", port), nil
}

func WaitForServer(duration time.Duration) {
	time.Sleep(duration)
}
