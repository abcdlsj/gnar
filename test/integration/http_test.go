package integration

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abcdlsj/gnar/test/helpers"
	"github.com/abcdlsj/gnar/test/unit"
)

func TestPathTunnel(t *testing.T) {
	binary := unit.BinaryPath(t)

	port := helpers.FreePort(t)
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	upstreamURL := serverURL + "/_gnar/debug/path-query"

	stopServer := helpers.StartProcess(t, binary, "server", "--listen", fmt.Sprintf(":%d", port), "--public-url", serverURL)
	defer stopServer()
	helpers.WaitForHTTP(t, serverURL+"/healthz", "")

	stopAgent := helpers.StartProcess(t, binary, "http", upstreamURL, "--server", serverURL, "--name", "demo")
	defer stopAgent()
	helpers.WaitForHTTP(t, serverURL+"/t/default/demo/ready", "")

	resp, err := http.Get(serverURL + "/t/default/demo/hello?x=1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, string(body))
	}

	if string(body) != "path=/hello query=x=1" {
		t.Fatalf("unexpected body: %s", string(body))
	}
}

func TestCustomDomainTunnel(t *testing.T) {
	binary := unit.BinaryPath(t)

	port := helpers.FreePort(t)
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	upstreamURL := serverURL + "/_gnar/debug/host-path"
	customDomain := "api.example.test"

	stopServer := helpers.StartProcess(t, binary, "server", "--listen", fmt.Sprintf(":%d", port), "--public-url", serverURL)
	defer stopServer()
	helpers.WaitForHTTP(t, serverURL+"/healthz", "")

	stopAgent := helpers.StartProcess(t, binary, "http", upstreamURL, "--server", serverURL, "--name", "demo", "--domain", customDomain)
	defer stopAgent()
	helpers.WaitForHTTP(t, serverURL+"/hello", customDomain)

	req, err := http.NewRequest(http.MethodGet, serverURL+"/hello", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = customDomain

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, string(body))
	}

	if string(body) != "host="+customDomain+" path=/hello" {
		t.Fatalf("unexpected body: %s", string(body))
	}
}

func TestManageCommands(t *testing.T) {
	binary := unit.BinaryPath(t)

	port := helpers.FreePort(t)
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	upstreamURL := serverURL + "/_gnar/debug/method-path"

	stopServer := helpers.StartProcess(t, binary, "server", "--listen", fmt.Sprintf(":%d", port), "--public-url", serverURL)
	defer stopServer()
	helpers.WaitForHTTP(t, serverURL+"/healthz", "")

	stopAgent := helpers.StartProcess(t, binary, "http", upstreamURL, "--server", serverURL, "--name", "demo")
	defer stopAgent()
	helpers.WaitForHTTP(t, serverURL+"/t/default/demo/ready", "")

	resp, err := http.Get(serverURL + "/t/default/demo/manage")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	listOutput := helpers.RunCommand(t, binary, "ls", "--server", serverURL)
	if !strings.Contains(listOutput, "default") || !strings.Contains(listOutput, serverURL+"/t/default/demo") {
		t.Fatalf("unexpected ls output: %s", listOutput)
	}

	inspectOutput := helpers.RunCommand(t, binary, "inspect", "demo", "--server", serverURL)
	if !strings.Contains(inspectOutput, "Tenant:  default") || !strings.Contains(inspectOutput, "Target:  "+upstreamURL) || !strings.Contains(inspectOutput, "Requests:2 total") {
		t.Fatalf("unexpected inspect output: %s", inspectOutput)
	}

	logsOutput := helpers.RunCommand(t, binary, "logs", "demo", "--server", serverURL, "--limit", "5")
	if !strings.Contains(logsOutput, "/manage") || !strings.Contains(logsOutput, "200") {
		t.Fatalf("unexpected logs output: %s", logsOutput)
	}

	doctorOutput := helpers.RunCommand(t, binary, "doctor", upstreamURL, "--server", serverURL)
	if !strings.Contains(doctorOutput, "server: ok") || !strings.Contains(doctorOutput, "local: ok") {
		t.Fatalf("unexpected doctor output: %s", doctorOutput)
	}
}

func TestDetachedTunnelLifecycle(t *testing.T) {
	binary := unit.BinaryPath(t)

	serverPort := helpers.FreePort(t)
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", serverPort)
	upstreamURL := serverURL + "/_gnar/debug/prefix-path?value=detached="
	daemonPort := helpers.FreePort(t)
	daemonListen := fmt.Sprintf(":%d", daemonPort)
	daemonURL := fmt.Sprintf("http://127.0.0.1:%d", daemonPort)
	statePath := filepath.Join(t.TempDir(), "agent-state.json")

	stopServer := helpers.StartProcess(t, binary, "server", "--listen", fmt.Sprintf(":%d", serverPort), "--public-url", serverURL)
	defer stopServer()
	helpers.WaitForHTTP(t, serverURL+"/healthz", "")

	stopDaemon := helpers.StartProcess(t, binary, "agent", "serve", "--listen", daemonListen, "--state-file", statePath)
	defer stopDaemon()
	helpers.WaitForHTTP(t, daemonURL+"/healthz", "")

	output := helpers.RunCommand(t, binary, "http", upstreamURL, "--server", serverURL, "--agent-url", daemonURL, "--detach", "--name", "detached")
	if !strings.Contains(output, "Tunnel: default/detached") || !strings.Contains(output, "State:  connected") || !strings.Contains(output, serverURL+"/t/default/detached") {
		t.Fatalf("unexpected detach output: %s", output)
	}

	helpers.WaitForHTTP(t, serverURL+"/t/default/detached/ready", "")

	agentList := helpers.RunCommand(t, binary, "agent", "ls", "--url", daemonURL)
	if !strings.Contains(agentList, "detached") || !strings.Contains(agentList, "connected") {
		t.Fatalf("unexpected agent ls output: %s", agentList)
	}

	resp, err := http.Get(serverURL + "/t/default/detached/demo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "detached=/demo" {
		t.Fatalf("unexpected response: status=%d body=%s", resp.StatusCode, string(body))
	}

	stopOutput := helpers.RunCommand(t, binary, "stop", "detached", "--agent-url", daemonURL)
	if !strings.Contains(stopOutput, "stopped default/detached") {
		t.Fatalf("unexpected stop output: %s", stopOutput)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(serverURL + "/t/default/detached/demo")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("detached tunnel still reachable after stop")
}

func TestDetachedTunnelRestoresAfterDaemonRestart(t *testing.T) {
	binary := unit.BinaryPath(t)

	serverPort := helpers.FreePort(t)
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", serverPort)
	upstreamURL := serverURL + "/_gnar/debug/prefix-path?value=restore="
	daemonPort := helpers.FreePort(t)
	daemonListen := fmt.Sprintf(":%d", daemonPort)
	daemonURL := fmt.Sprintf("http://127.0.0.1:%d", daemonPort)
	statePath := filepath.Join(t.TempDir(), "daemon-state.json")

	stopServer := helpers.StartProcess(t, binary, "server", "--listen", fmt.Sprintf(":%d", serverPort), "--public-url", serverURL)
	defer stopServer()
	helpers.WaitForHTTP(t, serverURL+"/healthz", "")

	stopDaemon := helpers.StartProcess(t, binary, "agent", "serve", "--listen", daemonListen, "--state-file", statePath)
	helpers.WaitForHTTP(t, daemonURL+"/healthz", "")

	helpers.RunCommand(t, binary, "http", upstreamURL, "--server", serverURL, "--agent-url", daemonURL, "--detach", "--name", "restore")
	helpers.WaitForHTTP(t, serverURL+"/t/default/restore/ready", "")

	stopDaemon()
	time.Sleep(500 * time.Millisecond)

	stopDaemon = helpers.StartProcess(t, binary, "agent", "serve", "--listen", daemonListen, "--state-file", statePath)
	defer stopDaemon()
	helpers.WaitForHTTP(t, daemonURL+"/healthz", "")
	helpers.WaitForHTTP(t, serverURL+"/t/default/restore/ready", "")

	resp, err := http.Get(serverURL + "/t/default/restore/check")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "restore=/check" {
		t.Fatalf("unexpected response: status=%d body=%s", resp.StatusCode, string(body))
	}
}

func TestSecurityBoundaries(t *testing.T) {
	binary := unit.BinaryPath(t)

	port := helpers.FreePort(t)
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	upstreamURL := serverURL + "/_gnar/debug/static?value=secure"

	stopServer := helpers.StartProcess(
		t,
		binary,
		"server",
		"--listen", fmt.Sprintf(":%d", port),
		"--public-url", serverURL,
		"--agent-credential", "default=agent-secret",
		"--manage-token", "manage-secret",
		"--allow-domain-suffix", "example.test",
	)
	defer stopServer()
	helpers.WaitForHTTP(t, serverURL+"/healthz", "")

	failedManage := helpers.RunCommandFailure(t, binary, "ls", "--server", serverURL)
	if !strings.Contains(failedManage, "invalid token") {
		t.Fatalf("unexpected manage failure: %s", failedManage)
	}

	failedAgent := helpers.RunCommandFailure(t, binary, "http", upstreamURL, "--server", serverURL, "--name", "secure")
	if !strings.Contains(failedAgent, "invalid token") {
		t.Fatalf("unexpected agent failure: %s", failedAgent)
	}

	failedDomain := helpers.RunCommandFailure(
		t,
		binary,
		"http", upstreamURL,
		"--server", serverURL,
		"--name", "bad-domain",
		"--token", "agent-secret",
		"--domain", "bad.invalid.test",
	)
	if !strings.Contains(failedDomain, "domain not allowed") {
		t.Fatalf("unexpected domain failure: %s", failedDomain)
	}

	stopAgent := helpers.StartProcess(
		t,
		binary,
		"http", upstreamURL,
		"--server", serverURL,
		"--name", "secure",
		"--token", "agent-secret",
		"--domain", "secure.example.test",
	)
	defer stopAgent()
	helpers.WaitForHTTP(t, serverURL+"/t/default/secure/ready", "")

	req, err := http.NewRequest(http.MethodGet, serverURL+"/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "secure.example.test"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "secure" {
		t.Fatalf("unexpected secure response: status=%d body=%s", resp.StatusCode, string(body))
	}

	listOutput := helpers.RunCommand(t, binary, "ls", "--server", serverURL, "--token", "manage-secret")
	if !strings.Contains(listOutput, "secure.example.test") || !strings.Contains(listOutput, "default") {
		t.Fatalf("unexpected authorized ls output: %s", listOutput)
	}
}
