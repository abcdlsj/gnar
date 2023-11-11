package server

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/abcdlsj/cr"
	"github.com/abcdlsj/pipe/logger"
)

var (
	caddyAddRouteF         = "{\"@id\":\"%s\",\"match\":[{\"host\":[\"%s\"]}],\"handle\":[{\"handler\":\"reverse_proxy\",\"upstreams\":[{\"dial\":\":%d\"}]}]}"
	caddyAddRouteUrl       = "http://127.0.0.1:2019/config/apps/http/servers/pipe/routes"
	caddyAddTlsSubjectsUrl = "http://127.0.0.1:2019/config/apps/tls/automation/policies/0/subjects"
)

func newHttpClient() *http.Client {
	return &http.Client{}
}

func addCaddyRouter(host string, port int) error {
	tunnelId := fmt.Sprintf("%s.%d", host, port)
	resp, err := http.Post(caddyAddRouteUrl, "application/json", bytes.NewBuffer([]byte(fmt.Sprintf(caddyAddRouteF, tunnelId, host, port))))
	if err != nil {
		logger.Errorf("Tunnel creation failed, err: %v", err)
		return err
	}
	defer resp.Body.Close()

	resp, err = http.Post(caddyAddTlsSubjectsUrl, "application/json", bytes.NewBuffer([]byte(fmt.Sprintf("\"%s\"", host))))
	if err != nil {
		logger.Errorf("Tunnel creation failed, err: %v", err)
		return err
	}
	defer resp.Body.Close()
	logger.Infof("Tunnel created successfully, id: %s, host: %s", tunnelId, cr.PWhiteUnderline(host))
	return nil
}

func delCaddyRouter(tunnelId string) error {
	logger.Infof("Cleaning up tunnel, id: %s", tunnelId)

	req, err := http.NewRequest("DELETE", fmt.Sprintf("http://127.0.0.1:2019/id/%s", tunnelId), nil)
	if err != nil {
		logger.Errorf("Tunnel deletion failed, err: %v", err)
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	_, err = newHttpClient().Do(req)
	if err != nil {
		logger.Errorf("Tunnel deletion failed, err: %v", err)
		return err
	}

	logger.Infof("Tunnel deleted successfully, id: %s", tunnelId)
	return nil
}
