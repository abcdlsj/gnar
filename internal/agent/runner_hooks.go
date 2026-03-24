package agent

import "github.com/abcdlsj/gnar/pkg/api"

type RunnerHooks struct {
	OnRegistered func(*api.RegisterTunnelResponse)
	OnPollError  func(error)
	OnStopped    func(error)
}
