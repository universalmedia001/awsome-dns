package main

import (
	"fmt"
	"net"
	"strings"
)

const zoneSuffix = ".awsome"

type Record struct {
	Domain string `json:"domain"`
	IPv4   string `json:"ipv4,omitempty"`
	Target string `json:"target,omitempty"`
}

func ValidateDomainName(domain string) error {
	name := normalizeDomainName(domain)
	if len(name) == 0 || len(name) > 253 {
		return fmt.Errorf("domain name must be between 1 and 253 characters")
	}
	if !strings.HasSuffix(name, zoneSuffix) {
		return fmt.Errorf("domain name must be within the %q namespace", zoneSuffix)
	}

	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return fmt.Errorf("domain name must contain a label before %q", zoneSuffix)
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return fmt.Errorf("domain name contains an invalid label length")
		}
		if !isASCIIAlphaNumeric(label[0]) || !isASCIIAlphaNumeric(label[len(label)-1]) {
			return fmt.Errorf("domain labels must start and end with a letter or digit")
		}
		for i := 1; i < len(label)-1; i++ {
			if !isASCIIAlphaNumeric(label[i]) && label[i] != '-' {
				return fmt.Errorf("domain labels may contain only letters, digits, and hyphens")
			}
		}
	}
	return nil
}

func ValidateIPv4Address(address string) error {
	ip := net.ParseIP(address)
	if ip == nil || ip.To4() == nil || strings.Contains(address, ":") {
		return fmt.Errorf("%q is not a valid IPv4 address", address)
	}
	return nil
}

func ValidateHostname(hostname string) error {
	name := normalizeDomainName(hostname)
	if len(name) == 0 || len(name) > 253 {
		return fmt.Errorf("hostname must be between 1 and 253 characters")
	}
	if net.ParseIP(name) != nil {
		return fmt.Errorf("CNAME target must be a hostname, not an IP address")
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return fmt.Errorf("hostname must be a fully qualified name with at least two labels")
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return fmt.Errorf("hostname contains an invalid label length")
		}
		if !isASCIIAlphaNumeric(label[0]) || !isASCIIAlphaNumeric(label[len(label)-1]) {
			return fmt.Errorf("hostname labels must start and end with a letter or digit")
		}
		for i := 1; i < len(label)-1; i++ {
			if !isASCIIAlphaNumeric(label[i]) && label[i] != '-' {
				return fmt.Errorf("hostname labels may contain only letters, digits, and hyphens")
			}
		}
	}
	return nil
}

func normalizeDomainName(domain string) string {
	return strings.ToLower(strings.TrimSuffix(domain, "."))
}

func (record Record) Type() string {
	if record.Target != "" {
		return "CNAME"
	}
	return "A"
}

func (record Record) Value() string {
	if record.Target != "" {
		return record.Target
	}
	return record.IPv4
}

func isASCIIAlphaNumeric(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= '0' && char <= '9'
}
