package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/abcdlsj/gnar/test/helpers"
	"github.com/abcdlsj/gnar/test/unit"
)

func TestSimpleMux(t *testing.T) {
	if err := unit.BuildGnarBinary(); err != nil {
		t.Fatalf("Failed to build gnar binary: %v", err)
	}

	stopPython, pythonPort, err := helpers.StartPythonServer()
	if err != nil {
		t.Fatalf("Failed to start Python server: %v", err)
	}
	defer stopPython()

	stopServer, err := helpers.StartGnarServer("8910", true)
	if err != nil {
		t.Fatalf("Failed to start gnar server: %v", err)
	}
	defer stopServer()

	helpers.WaitForServer(time.Second)

	stopClient, err := helpers.StartGnarClient("127.0.0.1:8910", fmt.Sprintf("%s:10020", pythonPort), true)
	if err != nil {
		t.Fatalf("Failed to start gnar client: %v", err)
	}
	defer stopClient()

	helpers.WaitForServer(time.Second)

	err = helpers.CheckHTTPResponse("http://127.0.0.1:10020", 200)
	if err != nil {
		t.Fatalf("HTTP check failed: %v", err)
	}
}
