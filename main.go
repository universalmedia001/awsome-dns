package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/miekg/dns"
)

func main() {
	config := DefaultConfig()
	records, err := LoadDatabase(config.DatabasePath)
	if err != nil {
		log.Fatalf("load domain database: %v", err)
	}

	registry, err := NewRegistry(records)
	if err != nil {
		log.Fatalf("validate domain database: %v", err)
	}
	if _, exists := registry.Lookup(config.DefaultDomain); !exists {
		if err := registry.Set(Record{Domain: config.DefaultDomain, IPv4: config.DefaultIPv4}); err != nil {
			log.Fatalf("add default domain record: %v", err)
		}
		if err := SaveDatabase(config.DatabasePath, registry.Records()); err != nil {
			log.Fatalf("save domain database: %v", err)
		}
	}

	resolver := NewResolver(registry, config.TTL)
	servers, err := newDNSServers(config.ListenAddress, resolver)
	if err != nil {
		log.Fatalf("configure DNS server: %v", err)
	}

	errCh := make(chan error, len(servers))
	for _, server := range servers {
		go func(server *dns.Server) {
			log.Printf("starting %s DNS server on %s", server.Net, server.Addr)
			errCh <- server.ListenAndServe()
		}(server)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
		log.Print("shutting down DNS servers")
	case err := <-errCh:
		if err != nil {
			log.Printf("DNS server stopped: %v", err)
		}
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdownDNSServers(shutdownContext, servers); err != nil {
		log.Printf("shut down DNS servers: %v", err)
	}
}
