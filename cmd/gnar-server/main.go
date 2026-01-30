package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/abcdlsj/gnar/internal/output"
	"github.com/abcdlsj/gnar/pkg/tunnel"
)

func main() {
	var (
		addr     = flag.String("addr", ":8443", "Listen address")
		domain   = flag.String("domain", "", "Base domain (e.g., gnar.example.com)")
		certDir  = flag.String("cert-dir", "./certs", "Certificate directory for autocert")
		autoCert = flag.Bool("autocert", true, "Enable Let's Encrypt autocert")
	)
	flag.Parse()

	cfg := tunnel.ServerConfig{
		ListenAddr: *addr,
		QUIC: tunnel.QUICConfig{
			Port: 0, // Auto-assign
		},
		HTTPS: tunnel.HTTPSConfig{
			Enabled:  *domain != "",
			AutoCert: *autoCert && *domain != "",
			CertDir:  *certDir,
		},
		Domain: tunnel.DomainConfig{
			BaseDomain: *domain,
			RandomLen:  8,
		},
	}

	server, err := tunnel.NewServer(cfg)
	if err != nil {
		output.Fatal("Failed to create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		output.Line()
		output.Info("Shutting down...")
		cancel()
	}()

	output.Title("Gnar Server")
	output.Pair("Listen:", *addr)
	if *domain != "" {
		output.Pair("Domain:", *domain)
		output.Muted("Autocert enabled")
	} else {
		output.Warning("Running without domain - HTTPS and subdomain features disabled")
	}
	output.Line()

	if err := server.Run(ctx); err != nil {
		output.Fatal("Server error: %v", err)
	}
}
