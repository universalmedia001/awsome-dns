package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestLegacyARecordDatabaseFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "domains.json")
	legacy := []byte(`[{"domain":"mrawsomeltd.awsome","ipv4":"192.0.2.10"}]`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy database: %v", err)
	}

	records, err := LoadDatabase(path)
	if err != nil {
		t.Fatalf("LoadDatabase() error = %v", err)
	}
	if len(records) != 1 || records[0] != (Record{Domain: defaultDomain, IPv4: defaultIPv4Address}) {
		t.Fatalf("loaded records = %#v", records)
	}
	registry, err := NewRegistry(records)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	assertResolverRecord(t, NewResolver(registry, defaultTTL), defaultDomain+".", dns.TypeA, dns.TypeA, defaultIPv4Address)

	if err := SaveDatabase(path, registry.Records()); err != nil {
		t.Fatalf("SaveDatabase() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved database: %v", err)
	}
	var saved []map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("decode saved database: %v", err)
	}
	if len(saved) != 1 || saved[0]["ipv4"] == nil || saved[0]["target"] != nil {
		t.Fatalf("legacy A record JSON changed: %s", data)
	}
}

func TestCNAMECLIAndResolver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "domains.json")
	aRecord := Record{Domain: "quali-boost.awsome", IPv4: "76.76.21.21"}
	if err := SaveDatabase(path, []Record{
		{Domain: defaultDomain, IPv4: defaultIPv4Address},
		aRecord,
	}); err != nil {
		t.Fatalf("SaveDatabase() error = %v", err)
	}
	var output bytes.Buffer
	if err := runCLI([]string{"add-cname", "alias.awsome", "target.example.com"}, path, &output); err != nil {
		t.Fatalf("add-cname error = %v", err)
	}
	if !strings.Contains(output.String(), "Successfully added CNAME alias.awsome -> target.example.com.") {
		t.Errorf("add-cname output = %q", output.String())
	}

	output.Reset()
	if err := runCLI([]string{"list"}, path, &output); err != nil {
		t.Fatalf("list error = %v", err)
	}
	if !strings.Contains(output.String(), "alias.awsome\tCNAME\ttarget.example.com") {
		t.Errorf("list output did not describe CNAME: %q", output.String())
	}

	registry, err := loadRegistry(path)
	if err != nil {
		t.Fatalf("loadRegistry() error = %v", err)
	}
	resolver := NewResolverWithDatabase(registry, defaultTTL, path)
	assertResolverRecord(t, resolver, "alias.awsome.", dns.TypeA, dns.TypeCNAME, "target.example.com.")
	assertResolverRecord(t, resolver, "alias.awsome.", dns.TypeCNAME, dns.TypeCNAME, "target.example.com.")
	assertResolverRecord(t, resolver, defaultDomain+".", dns.TypeA, dns.TypeA, defaultIPv4Address)

	output.Reset()
	if err := runCLI([]string{"update-cname", "quali-boost.awsome", "quali-boost-28h4.vercel.app"}, path, &output); err != nil {
		t.Fatalf("A-to-CNAME update error = %v", err)
	}
	if !strings.Contains(output.String(), "Successfully updated CNAME quali-boost.awsome -> quali-boost-28h4.vercel.app.") {
		t.Errorf("A-to-CNAME update output = %q", output.String())
	}
	assertResolverRecord(t, resolver, "quali-boost.awsome.", dns.TypeA, dns.TypeCNAME, "quali-boost-28h4.vercel.app.")

	records, err := LoadDatabase(path)
	if err != nil {
		t.Fatalf("LoadDatabase() after conversion error = %v", err)
	}
	var converted Record
	for _, record := range records {
		if record.Domain == aRecord.Domain {
			converted = record
		}
	}
	if converted.Target != "quali-boost-28h4.vercel.app" || converted.IPv4 != "" {
		t.Fatalf("persisted converted record = %#v", converted)
	}

	output.Reset()
	if err := runCLI([]string{"update-cname", "alias.awsome", "new-target.example.com"}, path, &output); err != nil {
		t.Fatalf("update-cname error = %v", err)
	}
	if !strings.Contains(output.String(), "Successfully updated CNAME alias.awsome -> new-target.example.com.") {
		t.Errorf("update-cname output = %q", output.String())
	}
	assertResolverRecord(t, resolver, "alias.awsome.", dns.TypeA, dns.TypeCNAME, "new-target.example.com.")
	if err := runCLI([]string{"update-cname", "missing.awsome", "target.example.com"}, path, &output); err == nil {
		t.Fatal("update-cname succeeded for a missing domain")
	}
	if err := runCLI([]string{"update-cname", "quali-boost.awsome", "invalid-target"}, path, &output); err == nil {
		t.Fatal("update-cname succeeded for an invalid target")
	}
	assertResolverRecord(t, resolver, "quali-boost.awsome.", dns.TypeA, dns.TypeCNAME, "quali-boost-28h4.vercel.app.")

	if err := runCLI([]string{"delete", "quali-boost.awsome"}, path, &output); err != nil {
		t.Fatalf("delete CNAME error = %v", err)
	}
	assertResolverRcode(t, resolver, "quali-boost.awsome.", dns.TypeA, dns.RcodeNameError)
	if err := runCLI([]string{"delete", "alias.awsome"}, path, &output); err != nil {
		t.Fatalf("delete CNAME error = %v", err)
	}
}

func TestCNAMEValidationConflictsAndLoops(t *testing.T) {
	registry, err := NewRegistry([]Record{{Domain: defaultDomain, IPv4: defaultIPv4Address}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	for _, invalid := range []Record{
		{Domain: "bad.awsome", Target: "not-a-fqdn"},
		{Domain: "bad.awsome", Target: "bad..example.com"},
		{Domain: "bad.awsome", IPv4: defaultIPv4Address, Target: "target.example.com"},
	} {
		if err := registry.Add(invalid); err == nil {
			t.Errorf("Add(%#v) succeeded, want validation error", invalid)
		}
	}

	cname := Record{Domain: "alias.awsome", Target: "target.example.com"}
	if err := registry.Add(cname); err != nil {
		t.Fatalf("add CNAME: %v", err)
	}
	if err := registry.Add(Record{Domain: "ALIAS.AWSOME.", IPv4: "192.0.2.11"}); err == nil {
		t.Fatal("duplicate A/CNAME owner was accepted")
	}
	if err := registry.Add(Record{Domain: "first.awsome", Target: "second.awsome"}); err != nil {
		t.Fatalf("add first CNAME: %v", err)
	}
	if err := registry.Add(Record{Domain: "second.awsome", Target: "first.awsome"}); err == nil {
		t.Fatal("CNAME loop was accepted")
	}
	if err := registry.Update(Record{Domain: "alias.awsome", IPv4: "192.0.2.12"}); err == nil {
		t.Fatal("update changed CNAME owner into an A record")
	}
	if _, err := NewRegistry([]Record{
		{Domain: "duplicate.awsome", IPv4: "192.0.2.1"},
		{Domain: "DUPLICATE.AWSOME.", Target: "target.example.com"},
	}); err == nil {
		t.Fatal("database containing duplicate A/CNAME owners was accepted")
	}
}

func TestDoHCNAMEQueries(t *testing.T) {
	registry, err := NewRegistry([]Record{{Domain: "alias.awsome", Target: "target.example.com"}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	handler := newDoHHandler(NewResolver(registry, defaultTTL))

	for _, test := range []struct {
		name  string
		qtype uint16
	}{
		{name: "A query receives CNAME", qtype: dns.TypeA},
		{name: "CNAME query", qtype: dns.TypeCNAME},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, dohPath, bytes.NewReader(packedDNSQuery(t, "alias.awsome.", test.qtype)))
			request.Header.Set("Content-Type", dnsMessageMediaType)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("HTTP status = %d, body %s", response.Code, response.Body.String())
			}
			var message dns.Msg
			if err := message.Unpack(response.Body.Bytes()); err != nil {
				t.Fatalf("unpack response: %v", err)
			}
			if len(message.Answer) != 1 {
				t.Fatalf("answer count = %d, want 1", len(message.Answer))
			}
			answer, ok := message.Answer[0].(*dns.CNAME)
			if !ok || answer.Target != "target.example.com." {
				t.Fatalf("answer = %v, want CNAME target.example.com.", message.Answer[0])
			}
		})
	}
}

func assertResolverRecord(t *testing.T, resolver *Resolver, domain string, qtype, wantType uint16, wantValue string) {
	t.Helper()
	request := new(dns.Msg)
	request.SetQuestion(domain, qtype)
	writer := &testResponseWriter{}
	resolver.ServeDNS(writer, request)
	if writer.message == nil || writer.message.Rcode != dns.RcodeSuccess {
		t.Fatalf("response = %#v, want NOERROR", writer.message)
	}
	if len(writer.message.Answer) != 1 {
		t.Fatalf("answer count = %d, want 1", len(writer.message.Answer))
	}
	switch answer := writer.message.Answer[0].(type) {
	case *dns.A:
		if wantType != dns.TypeA || answer.A.String() != wantValue {
			t.Errorf("A answer = %s, expected type %d value %s", answer.A, wantType, wantValue)
		}
	case *dns.CNAME:
		if wantType != dns.TypeCNAME || answer.Target != wantValue {
			t.Errorf("CNAME answer = %s, expected type %d value %s", answer.Target, wantType, wantValue)
		}
	default:
		t.Fatalf("answer type = %T", writer.message.Answer[0])
	}
}

func assertResolverRcode(t *testing.T, resolver *Resolver, domain string, qtype uint16, wantRcode int) {
	t.Helper()
	request := new(dns.Msg)
	request.SetQuestion(domain, qtype)
	writer := &testResponseWriter{}
	resolver.ServeDNS(writer, request)
	if writer.message == nil || writer.message.Rcode != wantRcode {
		t.Fatalf("response = %#v, want Rcode %d", writer.message, wantRcode)
	}
}
