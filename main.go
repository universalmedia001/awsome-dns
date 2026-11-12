package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
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

	if err := runServer(config); err != nil {
		log.Printf("DNS server stopped: %v", err)
		os.Exit(1)
	}
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

func runServer(config Config) error {
	records, err := LoadDatabase(config.DatabasePath)
	if err != nil {
		return fmt.Errorf("load domain database: %w", err)
	}

	registry, err := NewRegistry(records)
	if err != nil {
		return fmt.Errorf("validate domain database: %w", err)
	}
	if _, exists := registry.Lookup(config.DefaultDomain); !exists {
		if err := registry.Set(Record{Domain: config.DefaultDomain, IPv4: config.DefaultIPv4}); err != nil {
			return fmt.Errorf("add default domain record: %w", err)
		}
		if err := SaveDatabase(config.DatabasePath, registry.Records()); err != nil {
			return fmt.Errorf("save domain database: %w", err)
		}
	}

	resolver := NewResolverWithDatabase(registry, config.TTL, config.DatabasePath)
	servers, err := newDNSServers(config.ListenAddress, resolver)
	if err != nil {
		return fmt.Errorf("configure DNS server: %w", err)
	}

	udpConn, err := net.ListenPacket("udp", config.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen for UDP DNS on %s: %w", config.ListenAddress, err)
	}
	tcpListener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		_ = udpConn.Close()
		return fmt.Errorf("listen for TCP DNS on %s: %w", config.ListenAddress, err)
	}
	httpListener, err := net.Listen("tcp", config.HTTPListenAddress)
	if err != nil {
		_ = udpConn.Close()
		_ = tcpListener.Close()
		return fmt.Errorf("listen for DoH on %s: %w", config.HTTPListenAddress, err)
	}

	servers[0].PacketConn = udpConn
	servers[1].Listener = tcpListener
	httpServer := &http.Server{
		Handler:           newDoHHandler(resolver),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    96 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, len(servers)+1)
	for _, server := range servers {
		go func(server *dns.Server) {
			log.Printf("starting %s DNS server on %s", server.Net, server.Addr)
			serveErr := server.ActivateAndServe()
			if serveErr == nil {
				serveErr = fmt.Errorf("stopped unexpectedly")
			}
			errCh <- fmt.Errorf("%s DNS server: %w", server.Net, serveErr)
		}(server)
	}
	go func() {
		log.Printf("starting DoH HTTP server on %s%s (HTTPS must be provided by a reverse proxy)", config.HTTPListenAddress, dohPath)
		serveErr := httpServer.Serve(httpListener)
		if serveErr == nil {
			serveErr = fmt.Errorf("stopped unexpectedly")
		}
		errCh <- fmt.Errorf("DoH HTTP server: %w", serveErr)
	}()

	var serverErr error
	select {
	case <-ctx.Done():
		log.Print("shutting down DNS and DoH servers")
	case serverErr = <-errCh:
		log.Printf("server exited unexpectedly: %v", serverErr)
		stop()
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpErr := httpServer.Shutdown(shutdownContext)
	if err := shutdownDNSServers(shutdownContext, servers); err != nil {
		return fmt.Errorf("shut down DNS servers: %w", err)
	}
	if httpErr != nil {
		return fmt.Errorf("shut down DoH server: %w", httpErr)
	}
	return serverErr
}
