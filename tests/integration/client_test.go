package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/abcdlsj/gnar/tests/helpers"
	"github.com/abcdlsj/gnar/tests/testutils"
)

func TestSimpleClient(t *testing.T) {
	if err := testutils.BuildGnarBinary(); err != nil {
		t.Fatalf("Failed to build gnar binary: %v", err)
	}

	stopPython, pythonPort, err := helpers.StartPythonServer()
	if err != nil {
		t.Fatalf("Failed to start Python server: %v", err)
	}
	defer stopPython()

	stopServer, err := helpers.StartGnarServer("8910", false)
	if err != nil {
		t.Fatalf("Failed to start gnar server: %v", err)
	}
	defer stopServer()

	helpers.WaitForServer(time.Second)

	stopClient, err := helpers.StartGnarClient("127.0.0.1:8910", fmt.Sprintf("%s:10020", pythonPort), false)
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
