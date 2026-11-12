package main

import "os"

const (
	defaultListenAddress     = "127.0.0.1:8053"
	defaultHTTPListenAddress = "127.0.0.1:8054"
	defaultDatabasePath      = "domains.json"
	defaultDomain            = "mrawsomeltd.awsome"
	defaultIPv4Address       = "192.0.2.10"
	defaultTTL               = 300
	dohPath                  = "/dns-query"
)

type Config struct {
	ListenAddress     string
	HTTPListenAddress string
	DatabasePath      string
	DefaultDomain     string
	DefaultIPv4       string
	TTL               uint32
}

func DefaultConfig() Config {
	return Config{
		ListenAddress:     defaultListenAddress,
		HTTPListenAddress: configuredValue("AWSOME_DNS_HTTP_ADDR", defaultHTTPListenAddress),
		DatabasePath:      defaultDatabasePath,
		DefaultDomain:     defaultDomain,
		DefaultIPv4:       defaultIPv4Address,
		TTL:               defaultTTL,
	}
}

func configuredValue(environmentVariable, fallback string) string {
	if value := os.Getenv(environmentVariable); value != "" {
		return value
	}
	return fallback
}
