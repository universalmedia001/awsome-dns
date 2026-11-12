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
		HTTPListenAddress: defaultHTTPAddress(),
		DatabasePath:      defaultDatabasePath,
		DefaultDomain:     defaultDomain,
		DefaultIPv4:       defaultIPv4Address,
		TTL:               defaultTTL,
	}
}

func defaultHTTPAddress() string {
	if address := os.Getenv("AWSOME_DNS_HTTP_ADDR"); address != "" {
		return address
	}
	if port := os.Getenv("PORT"); port != "" {
		return "0.0.0.0:" + port
	}
	return defaultHTTPListenAddress
}
