package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestExternalDNSQueriesForwardOriginalWireToUpstream(t *testing.T) {
	queryWire := packedDNSQuery(t, "www.example.com.", dns.TypeAAAA)
	var forwarded atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		forwarded.Add(1)
		if request.Method != http.MethodPost {
			t.Errorf("upstream method = %s, want POST", request.Method)
		}
		if request.Header.Get("Content-Type") != dnsMessageMediaType {
			t.Errorf("upstream Content-Type = %q", request.Header.Get("Content-Type"))
		}
		if request.Header.Get("Accept") != dnsMessageMediaType {
			t.Errorf("upstream Accept = %q", request.Header.Get("Accept"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		if !bytes.Equal(body, queryWire) {
			t.Errorf("forwarded wire request differs from original")
		}

		var query dns.Msg
		if err := query.Unpack(body); err != nil {
			t.Errorf("unpack forwarded query: %v", err)
			http.Error(writer, "bad query", http.StatusBadRequest)
			return
		}
		answer := new(dns.Msg)
		answer.SetReply(&query)
		answer.Answer = []dns.RR{&dns.AAAA{
			Hdr:  dns.RR_Header{Name: query.Question[0].Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 120},
			AAAA: net.ParseIP("2001:db8::1"),
		}}
		responseWire, err := answer.Pack()
		if err != nil {
			t.Errorf("pack upstream response: %v", err)
			http.Error(writer, "bad response", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", dnsMessageMediaType)
		_, _ = writer.Write(responseWire)
	}))
	defer server.Close()

	registry, err := NewRegistry([]Record{{Domain: defaultDomain, IPv4: defaultIPv4Address}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	resolver := NewResolverWithForwarder(registry, defaultTTL, newDoHForwarder(server.URL, server.Client()))
	responseWire, err := resolver.ExchangeWire(context.Background(), queryWire)
	if err != nil {
		t.Fatalf("ExchangeWire() error = %v", err)
	}
	if forwarded.Load() != 1 {
		t.Fatalf("upstream request count = %d, want 1", forwarded.Load())
	}
	var response dns.Msg
	if err := response.Unpack(responseWire); err != nil {
		t.Fatalf("unpack forwarded response: %v", err)
	}
	if response.Rcode != dns.RcodeSuccess || len(response.Answer) != 1 {
		t.Fatalf("upstream DNS response = Rcode %d, %d answers", response.Rcode, len(response.Answer))
	}
	aaaa, ok := response.Answer[0].(*dns.AAAA)
	if !ok || aaaa.AAAA.String() != "2001:db8::1" {
		t.Fatalf("upstream answer = %v, want AAAA 2001:db8::1", response.Answer[0])
	}

	dnsWriter := &testResponseWriter{}
	resolver.ServeDNS(dnsWriter, mustUnpackQuery(t, queryWire))
	if dnsWriter.message == nil || dnsWriter.message.Rcode != dns.RcodeSuccess || len(dnsWriter.message.Answer) != 1 {
		t.Fatalf("ServeDNS() did not return upstream answer: %#v", dnsWriter.message)
	}

	request := httptest.NewRequest(http.MethodPost, dohPath, bytes.NewReader(queryWire))
	request.Header.Set("Content-Type", dnsMessageMediaType)
	httpResponse := httptest.NewRecorder()
	newDoHHandler(resolver).ServeHTTP(httpResponse, request)
	if httpResponse.Code != http.StatusOK {
		t.Fatalf("DoH HTTP status = %d, body %s", httpResponse.Code, httpResponse.Body.String())
	}
	var dohAnswer dns.Msg
	if err := dohAnswer.Unpack(httpResponse.Body.Bytes()); err != nil {
		t.Fatalf("unpack DoH forwarded response: %v", err)
	}
	if len(dohAnswer.Answer) != 1 {
		t.Fatalf("DoH answer count = %d, want 1", len(dohAnswer.Answer))
	}
	if _, ok := dohAnswer.Answer[0].(*dns.AAAA); !ok {
		t.Fatalf("DoH answer type = %T, want *dns.AAAA", dohAnswer.Answer[0])
	}

	getRequest := httptest.NewRequest(http.MethodGet, dohPath+"?dns="+base64.RawURLEncoding.EncodeToString(queryWire), nil)
	getResponse := httptest.NewRecorder()
	newDoHHandler(resolver).ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("DoH GET HTTP status = %d, body %s", getResponse.Code, getResponse.Body.String())
	}
	if forwarded.Load() != 4 {
		t.Fatalf("upstream request count = %d, want 4", forwarded.Load())
	}
}

func TestAWSOMEDomainsNeverForward(t *testing.T) {
	var forwarded atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		forwarded.Add(1)
	}))
	defer server.Close()

	registry, err := NewRegistry([]Record{{Domain: defaultDomain, IPv4: defaultIPv4Address}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	resolver := NewResolverWithForwarder(registry, defaultTTL, newDoHForwarder(server.URL, server.Client()))

	for _, test := range []struct {
		domain string
		rcode  int
	}{
		{domain: defaultDomain + ".", rcode: dns.RcodeSuccess},
		{domain: "missing.awsome.", rcode: dns.RcodeNameError},
		{domain: "awsome.", rcode: dns.RcodeNameError},
	} {
		response, err := resolver.ExchangeWire(context.Background(), packedDNSQuery(t, test.domain, dns.TypeA))
		if err != nil {
			t.Fatalf("ExchangeWire(%q) error = %v", test.domain, err)
		}
		var message dns.Msg
		if err := message.Unpack(response); err != nil {
			t.Fatalf("unpack response for %q: %v", test.domain, err)
		}
		if message.Rcode != test.rcode {
			t.Errorf("Rcode for %q = %d, want %d", test.domain, message.Rcode, test.rcode)
		}
	}
	if forwarded.Load() != 0 {
		t.Errorf("forwarded %d .awsome queries, want none", forwarded.Load())
	}
}

func TestDoHForwarderTimeoutAndMalformedResponse(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			select {
			case <-request.Context().Done():
			case <-time.After(100 * time.Millisecond):
				_, _ = io.WriteString(writer, "late")
			}
		}))
		defer server.Close()

		forwarder := newDoHForwarder(server.URL, &http.Client{Timeout: 20 * time.Millisecond})
		if _, err := forwarder.Forward(context.Background(), packedDNSQuery(t, "example.com.", dns.TypeA)); err == nil {
			t.Fatal("Forward() succeeded for a timed-out upstream")
		}
		handler := newDoHHandler(NewResolverWithForwarder(
			mustRegistry(t),
			defaultTTL,
			newDoHForwarder(server.URL, &http.Client{Timeout: 20 * time.Millisecond}),
		))
		request := httptest.NewRequest(http.MethodPost, dohPath, bytes.NewReader(packedDNSQuery(t, "example.com.", dns.TypeA)))
		request.Header.Set("Content-Type", dnsMessageMediaType)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "timeout") {
			t.Fatalf("DoH timeout response = %d %q, want generic HTTP 502", response.Code, response.Body.String())
		}
	})

	t.Run("malformed response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", dnsMessageMediaType)
			_, _ = io.WriteString(writer, "not dns wire data")
		}))
		defer server.Close()

		forwarder := newDoHForwarder(server.URL, server.Client())
		if _, err := forwarder.Forward(context.Background(), packedDNSQuery(t, "example.com.", dns.TypeA)); err == nil {
			t.Fatal("Forward() accepted malformed upstream DNS data")
		}
	})

	t.Run("mismatched transaction", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			var query dns.Msg
			_ = query.Unpack(body)
			answer := new(dns.Msg)
			answer.SetReply(&query)
			answer.Id++
			wire, _ := answer.Pack()
			writer.Header().Set("Content-Type", dnsMessageMediaType)
			_, _ = writer.Write(wire)
		}))
		defer server.Close()

		forwarder := newDoHForwarder(server.URL, server.Client())
		if _, err := forwarder.Forward(context.Background(), packedDNSQuery(t, "example.com.", dns.TypeA)); err == nil {
			t.Fatal("Forward() accepted mismatched DNS transaction ID")
		}
	})
}

func TestRequestRateLimiter(t *testing.T) {
	limiter := newRequestRateLimiter()
	for i := 0; i < dohRequestBurst; i++ {
		if !limiter.Allow("192.0.2.1:1234") {
			t.Fatalf("request %d was rejected before burst limit", i+1)
		}
	}
	if limiter.Allow("192.0.2.1:1234") {
		t.Fatal("request after burst limit was allowed")
	}
	if !limiter.Allow("192.0.2.2:1234") {
		t.Fatal("a separate client was rate limited")
	}
}

func mustUnpackQuery(t *testing.T, wire []byte) *dns.Msg {
	t.Helper()
	query := new(dns.Msg)
	if err := query.Unpack(wire); err != nil {
		t.Fatalf("unpack DNS query: %v", err)
	}
	return query
}

func mustRegistry(t *testing.T) *Registry {
	t.Helper()
	registry, err := NewRegistry([]Record{{Domain: defaultDomain, IPv4: defaultIPv4Address}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}
