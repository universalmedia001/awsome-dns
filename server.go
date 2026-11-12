package main

import (
	"context"
	"fmt"
	"net"

	"github.com/miekg/dns"
)

func newDNSServers(address string, handler dns.Handler) ([]*dns.Server, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid DNS listen address %q: %w", address, err)
	}
	if net.ParseIP(host) == nil {
		return nil, fmt.Errorf("DNS listen host %q must be an IP address", host)
	}
	if port == "" {
		return nil, fmt.Errorf("DNS listen address must include a port")
	}

	return []*dns.Server{
		{Addr: address, Net: "udp", Handler: handler},
		{Addr: address, Net: "tcp", Handler: handler},
	}, nil
}

func shutdownDNSServers(ctx context.Context, servers []*dns.Server) error {
	var firstErr error
	for _, server := range servers {
		if err := server.ShutdownContext(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
