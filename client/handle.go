package client

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/abcdlsj/gpipe/layer"
	"github.com/abcdlsj/gpipe/logger"
	"github.com/abcdlsj/gpipe/proxy"
)

type Config struct {
	ServerHost string    `toml:"server-host"`
	ServerPort int       `toml:"server-port"`
	Forwards   []Forward `toml:"forwards"`
}

type Forward struct {
	RemotePort int `toml:"remote-port"`
	LocalPort  int `toml:"local-port"`
}

type Client struct {
	cfg Config
}

func parseConfig(cfgFile string) Config {
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		logger.FatalF("Error reading config file: %v", err)
	}

	var cfg Config
	toml.Unmarshal(data, &cfg)

	return cfg
}

func newClient(cfg Config) *Client {
	return &Client{
		cfg: cfg,
	}
}

func (c *Client) Run() {
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	rConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.ServerHost, c.cfg.ServerPort))
	if err != nil {
		logger.FatalF("Error connecting to remote: %v", err)
	}

	if err := layer.RegisterForward.Send(rConn, c.cfg.Forwards[0].RemotePort); err != nil {
		logger.FatalF("Error writing to remote: %v", err)
	}

	go func() {
		<-sc
		c.CancelForward()
		os.Exit(1)
	}()

	for {
		_, buf, err := layer.Read(rConn)
		if err != nil {
			logger.WarnF("Error reading from connection: %v", err)
			return
		}
		if buf == nil {
			continue
		}

		logger.Debug("Receive req from server, start proxying")
		nRonn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.ServerHost, c.cfg.ServerPort))
		if err != nil {
			logger.FatalF("Error connecting to remote: %v", err)
		}

		lConn, err := net.Dial("tcp", fmt.Sprintf(":%d", c.cfg.Forwards[0].LocalPort))
		if err != nil {
			logger.FatalF("Error connecting to local: %v", err)
		}

		go func() {
			_, err := nRonn.Write(buf)
			logger.Debug("Write back buf to server")
			if err != nil {
				logger.InfoF("Error writing to remote: %v", err)
			}
			proxy.P(lConn, nRonn)
		}()
	}
}

func (c *Client) CancelForward() {
	nRonn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.cfg.ServerHost, c.cfg.ServerPort))
	if err != nil {
		logger.FatalF("Error connecting to remote: %v", err)
	}
	if err := layer.CancelForward.Send(nRonn, c.cfg.Forwards[0].RemotePort); err != nil {
		logger.FatalF("Error writing to remote: %v", err)
	}
	logger.InfoF("Close connection to server")
}
