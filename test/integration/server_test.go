package integration

import (
	"testing"
	"time"

	"github.com/abcdlsj/gnar/test/common"
	"github.com/abcdlsj/gnar/test/helpers"
	"github.com/abcdlsj/gnar/test/unit"
)

func TestSimpleServer(t *testing.T) {
	if err := unit.BuildGnarBinary(); err != nil {
		t.Fatalf("Failed to build gnar binary: %v", err)
	}

	t.Logf("Starting gnar server test with binary path: %s", common.GnarPath)
	stopServer, err := helpers.StartGnarServer("8910", false)
	if err != nil {
		t.Fatalf("Failed to start gnar server: %v", err)
	}
	defer stopServer()

	helpers.WaitForServer(time.Second)

	// 这里我们只测试服务器是否成功启动
	// 实际上，我们可能需要更复杂的逻辑来验证服务器的功能
	t.Log("Gnar server started successfully")
}
