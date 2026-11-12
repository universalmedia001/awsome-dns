package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/miekg/dns"
)

func main() {
	config := DefaultConfig()
	if len(os.Args) > 1 {
		if err := runCLI(os.Args[1:], config.DatabasePath, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	runServer(config)
}

func runCLI(args []string, databasePath string, output io.Writer) error {
	if len(args) == 1 && args[0] == "list" {
		registry, err := loadRegistry(databasePath)
		if err != nil {
			return err
		}
		records := registry.Records()
		if len(records) == 0 {
			_, err := fmt.Fprintln(output, "No domains registered.")
			return err
		}
		if _, err := fmt.Fprintln(output, "DOMAIN\tIPv4"); err != nil {
			return err
		}
		for _, record := range records {
			if _, err := fmt.Fprintf(output, "%s\t%s\n", record.Domain, record.IPv4); err != nil {
				return err
			}
		}
		return nil
	}

	if len(args) == 3 && (args[0] == "add" || args[0] == "update") {
		registry, err := loadRegistry(databasePath)
		if err != nil {
			return err
		}
		record := Record{Domain: args[1], IPv4: args[2]}
		switch args[0] {
		case "add":
			err = registry.Add(record)
		case "update":
			err = registry.Update(record)
		}
		if err != nil {
			return err
		}
		if err := SaveDatabase(databasePath, registry.Records()); err != nil {
			return err
		}
		saved, exists := registry.Lookup(args[1])
		if !exists {
			return fmt.Errorf("domain %q was not present after the operation", normalizeDomainName(args[1]))
		}
		if args[0] == "add" {
			_, err = fmt.Fprintf(output, "Successfully added %s -> %s.\n", saved.Domain, saved.IPv4)
		} else {
			_, err = fmt.Fprintf(output, "Successfully updated %s -> %s.\n", saved.Domain, saved.IPv4)
		}
		return err
	}

	if len(args) == 2 && args[0] == "delete" {
		registry, err := loadRegistry(databasePath)
		if err != nil {
			return err
		}
		if err := registry.Delete(args[1]); err != nil {
			return err
		}
		if err := SaveDatabase(databasePath, registry.Records()); err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "Successfully deleted %s.\n", normalizeDomainName(args[1]))
		return err
	}

	return fmt.Errorf("usage: awsome-dns add <domain.awsome> <IPv4> | list | update <domain.awsome> <IPv4> | delete <domain.awsome>")
}

func loadRegistry(databasePath string) (*Registry, error) {
	records, err := LoadDatabase(databasePath)
	if err != nil {
		return nil, err
	}
	registry, err := NewRegistry(records)
	if err != nil {
		return nil, fmt.Errorf("validate domain database: %w", err)
	}
	return registry, nil
}

func runServer(config Config) {
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

	resolver := NewResolverWithDatabase(registry, config.TTL, config.DatabasePath)
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
