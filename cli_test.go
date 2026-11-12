package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestDomainManagementCLIUpdatesResolver(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "domains.json")
	sample := Record{Domain: defaultDomain, IPv4: defaultIPv4Address}
	if err := SaveDatabase(databasePath, []Record{sample}); err != nil {
		t.Fatalf("SaveDatabase() error = %v", err)
	}

	var output bytes.Buffer
	if err := runCLI([]string{"add", "service.awsome", "8.8.8.8"}, databasePath, &output); err != nil {
		t.Fatalf("add command error = %v", err)
	}
	if !strings.Contains(output.String(), "Successfully added service.awsome -> 8.8.8.8.") {
		t.Errorf("add output = %q", output.String())
	}

	beforeDuplicate, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatalf("read database before duplicate add: %v", err)
	}
	if err := runCLI([]string{"add", "SERVICE.awsome.", "1.1.1.1"}, databasePath, &output); err == nil {
		t.Fatal("duplicate add succeeded, want an error")
	}
	afterDuplicate, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatalf("read database after duplicate add: %v", err)
	}
	if !bytes.Equal(beforeDuplicate, afterDuplicate) {
		t.Fatal("duplicate add changed the database")
	}

	output.Reset()
	if err := runCLI([]string{"list"}, databasePath, &output); err != nil {
		t.Fatalf("list command error = %v", err)
	}
	if !strings.Contains(output.String(), "mrawsomeltd.awsome") ||
		!strings.Contains(output.String(), "service.awsome\tA\t8.8.8.8") {
		t.Errorf("list output did not include both records: %q", output.String())
	}

	registry, err := loadRegistry(databasePath)
	if err != nil {
		t.Fatalf("loadRegistry() error = %v", err)
	}
	resolver := NewResolverWithDatabase(registry, 300, databasePath)
	assertDNSAnswer(t, resolver, "service.awsome.", dns.RcodeSuccess, "8.8.8.8")

	output.Reset()
	if err := runCLI([]string{"update", "service.awsome", "1.1.1.1"}, databasePath, &output); err != nil {
		t.Fatalf("update command error = %v", err)
	}
	if !strings.Contains(output.String(), "Successfully updated service.awsome -> 1.1.1.1.") {
		t.Errorf("update output = %q", output.String())
	}
	assertDNSAnswer(t, resolver, "service.awsome.", dns.RcodeSuccess, "1.1.1.1")

	output.Reset()
	if err := runCLI([]string{"delete", "service.awsome"}, databasePath, &output); err != nil {
		t.Fatalf("delete command error = %v", err)
	}
	if !strings.Contains(output.String(), "Successfully deleted service.awsome.") {
		t.Errorf("delete output = %q", output.String())
	}
	assertDNSAnswer(t, resolver, "service.awsome.", dns.RcodeNameError, "")

	remaining, err := LoadDatabase(databasePath)
	if err != nil {
		t.Fatalf("LoadDatabase() after delete error = %v", err)
	}
	if len(remaining) != 1 || remaining[0] != sample {
		t.Fatalf("records after delete = %#v, want preserved sample %#v", remaining, sample)
	}
}

func TestDomainManagementCLIRejectsInvalidInputWithoutChangingDatabase(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "domains.json")
	initial := []Record{{Domain: defaultDomain, IPv4: defaultIPv4Address}}
	if err := SaveDatabase(databasePath, initial); err != nil {
		t.Fatalf("SaveDatabase() error = %v", err)
	}
	before, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatalf("read database before invalid command: %v", err)
	}

	for _, args := range [][]string{
		{"add", "outside.example", "8.8.8.8"},
		{"add", "invalid.awsome", "999.1.1.1"},
		{"update", defaultDomain, "999.1.1.1"},
		{"update", "missing.awsome", "8.8.8.8"},
		{"delete", "missing.awsome"},
	} {
		if err := runCLI(args, databasePath, &bytes.Buffer{}); err == nil {
			t.Errorf("runCLI(%v) succeeded, want an error", args)
		}
	}

	after, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatalf("read database after invalid commands: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("invalid command changed the database")
	}
}

func assertDNSAnswer(t *testing.T, resolver *Resolver, domain string, wantRcode int, wantIP string) {
	t.Helper()
	request := new(dns.Msg)
	request.SetQuestion(domain, dns.TypeA)
	writer := &testResponseWriter{}
	resolver.ServeDNS(writer, request)
	if writer.message == nil {
		t.Fatal("resolver did not write a response")
	}
	if writer.message.Rcode != wantRcode {
		t.Fatalf("response Rcode = %d, want %d", writer.message.Rcode, wantRcode)
	}
	if wantIP == "" {
		if len(writer.message.Answer) != 0 {
			t.Fatalf("response has %d answers, want none", len(writer.message.Answer))
		}
		return
	}
	if len(writer.message.Answer) != 1 {
		t.Fatalf("response has %d answers, want 1", len(writer.message.Answer))
	}
	answer, ok := writer.message.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("answer type = %T, want *dns.A", writer.message.Answer[0])
	}
	if got := answer.A.String(); got != wantIP {
		t.Errorf("A record = %s, want %s", got, wantIP)
	}
}
