package main

import (
	"path/filepath"
	"testing"
)

func TestValidateDomainName(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		{name: "valid", domain: "mrawsomeltd.awsome"},
		{name: "case insensitive", domain: "MrawSomeLtd.AWSOME."},
		{name: "outside namespace", domain: "example.com", wantErr: true},
		{name: "missing subdomain", domain: "awsome", wantErr: true},
		{name: "empty label", domain: "bad..awsome", wantErr: true},
		{name: "invalid label character", domain: "bad_name.awsome", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateDomainName(test.domain)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateDomainName(%q) error = %v, wantErr %v", test.domain, err, test.wantErr)
			}
		})
	}
}

func TestValidateIPv4Address(t *testing.T) {
	for _, test := range []struct {
		address string
		wantErr bool
	}{
		{address: "192.0.2.10"},
		{address: "2001:db8::1", wantErr: true},
		{address: "999.0.2.10", wantErr: true},
	} {
		err := ValidateIPv4Address(test.address)
		if (err != nil) != test.wantErr {
			t.Errorf("ValidateIPv4Address(%q) error = %v, wantErr %v", test.address, err, test.wantErr)
		}
	}
}

func TestDatabaseRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "domains.json")
	want := []Record{{Domain: "mrawsomeltd.awsome", IPv4: "192.0.2.10"}}

	records, err := LoadDatabase(path)
	if err != nil {
		t.Fatalf("LoadDatabase() for a missing file: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("LoadDatabase() for a missing file returned %d records, want 0", len(records))
	}
	if err := SaveDatabase(path, want); err != nil {
		t.Fatalf("SaveDatabase() error = %v", err)
	}
	got, err := LoadDatabase(path)
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("LoadDatabase() = %#v, want %#v", got, want)
	}
}
