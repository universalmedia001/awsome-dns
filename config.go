package main

const (
	defaultListenAddress = "127.0.0.1:8053"
	defaultDatabasePath  = "domains.json"
	defaultDomain        = "mrawsomeltd.awsome"
	defaultIPv4Address   = "192.0.2.10"
	defaultTTL           = 300
)

type Config struct {
	ListenAddress string
	DatabasePath  string
	DefaultDomain string
	DefaultIPv4   string
	TTL           uint32
}

func DefaultConfig() Config {
	return Config{
		ListenAddress: defaultListenAddress,
		DatabasePath:  defaultDatabasePath,
		DefaultDomain: defaultDomain,
		DefaultIPv4:   defaultIPv4Address,
		TTL:           defaultTTL,
	}
}
