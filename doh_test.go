package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDoHValidGETAndPOST(t *testing.T) {
	handler := newDoHHandler(testResolver(t))
	query := packedDNSQuery(t, "mrawsomeltd.awsome.", dns.TypeA)

	getRequest := httptest.NewRequest(http.MethodGet, dohPath+"?dns="+base64.RawURLEncoding.EncodeToString(query), nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	assertDoHResponse(t, getResponse, dns.RcodeSuccess, "192.0.2.10")

	postRequest := httptest.NewRequest(http.MethodPost, dohPath, bytes.NewReader(query))
	postRequest.Header.Set("Content-Type", dnsMessageMediaType)
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, postRequest)
	assertDoHResponse(t, postResponse, dns.RcodeSuccess, "192.0.2.10")
}

func TestHealthEndpoint(t *testing.T) {
	handler := newDoHHandler(testResolver(t))
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.String() != "ok\n" {
		t.Fatalf("health body = %q, want %q", response.Body.String(), "ok\n")
	}
	if strings.Contains(response.Body.String(), defaultDomain) {
		t.Fatal("health response exposed domain records")
	}

	request = httptest.NewRequest(http.MethodPost, "/health", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST health status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestDoHRejectsMalformedRequests(t *testing.T) {
	handler := newDoHHandler(testResolver(t))
	tests := []struct {
		name        string
		method      string
		target      string
		body        string
		contentType string
		wantStatus  int
	}{
		{name: "malformed base64url", method: http.MethodGet, target: dohPath + "?dns=***", wantStatus: http.StatusBadRequest},
		{name: "invalid DNS wire format", method: http.MethodPost, target: dohPath, body: "bad", contentType: dnsMessageMediaType, wantStatus: http.StatusBadRequest},
		{name: "unsupported method", method: http.MethodPut, target: dohPath, wantStatus: http.StatusMethodNotAllowed},
		{name: "oversized POST", method: http.MethodPost, target: dohPath, body: strings.Repeat("x", maxDNSMessageSize+1), contentType: dnsMessageMediaType, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "wrong content type", method: http.MethodPost, target: dohPath, body: "body", contentType: "application/json", wantStatus: http.StatusUnsupportedMediaType},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Errorf("HTTP status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantStatus == http.StatusMethodNotAllowed && response.Header().Get("Allow") != "GET, POST" {
				t.Errorf("Allow header = %q, want GET, POST", response.Header().Get("Allow"))
			}
		})
	}
}

func TestDoHUnknownAndUpdatedDomains(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "domains.json")
	if err := SaveDatabase(databasePath, []Record{{Domain: defaultDomain, IPv4: defaultIPv4Address}}); err != nil {
		t.Fatalf("SaveDatabase() error = %v", err)
	}
	registry, err := loadRegistry(databasePath)
	if err != nil {
		t.Fatalf("loadRegistry() error = %v", err)
	}
	resolver := NewResolverWithDatabase(registry, defaultTTL, databasePath)
	handler := newDoHHandler(resolver)

	assertDoHQuery(t, handler, "missing.awsome.", dns.RcodeNameError, "")

	if err := runCLI([]string{"add", "service.awsome", "8.8.8.8"}, databasePath, &bytes.Buffer{}); err != nil {
		t.Fatalf("add command error = %v", err)
	}
	assertDoHQuery(t, handler, "service.awsome.", dns.RcodeSuccess, "8.8.8.8")

	if err := runCLI([]string{"update", "service.awsome", "1.1.1.1"}, databasePath, &bytes.Buffer{}); err != nil {
		t.Fatalf("update command error = %v", err)
	}
	assertDoHQuery(t, handler, "service.awsome.", dns.RcodeSuccess, "1.1.1.1")
}

func TestExistingUDPAndTCPDNSListeners(t *testing.T) {
	resolver := testResolver(t)
	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = udpConn.Close()
		t.Fatalf("listen TCP: %v", err)
	}

	udpServer := &dns.Server{PacketConn: udpConn, Net: "udp", Handler: resolver}
	tcpServer := &dns.Server{Listener: tcpListener, Net: "tcp", Handler: resolver}
	go func() { _ = udpServer.ActivateAndServe() }()
	go func() { _ = tcpServer.ActivateAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = udpServer.ShutdownContext(ctx)
		_ = tcpServer.ShutdownContext(ctx)
	})

	query := new(dns.Msg)
	query.SetQuestion("mrawsomeltd.awsome.", dns.TypeA)
	for _, transport := range []string{"udp", "tcp"} {
		t.Run(transport, func(t *testing.T) {
			client := &dns.Client{Net: transport, Timeout: 2 * time.Second}
			address := udpConn.LocalAddr().String()
			if transport == "tcp" {
				address = tcpListener.Addr().String()
			}
			response, _, err := client.Exchange(query, address)
			if err != nil {
				t.Fatalf("DNS exchange over %s: %v", transport, err)
			}
			if response.Rcode != dns.RcodeSuccess || len(response.Answer) != 1 {
				t.Fatalf("DNS response over %s = Rcode %d, %d answers", transport, response.Rcode, len(response.Answer))
			}
			answer, ok := response.Answer[0].(*dns.A)
			if !ok || answer.A.String() != defaultIPv4Address {
				t.Fatalf("DNS answer over %s = %v, want %s", transport, response.Answer[0], defaultIPv4Address)
			}
		})
	}
}

func testResolver(t *testing.T) *Resolver {
	t.Helper()
	registry, err := NewRegistry([]Record{{Domain: defaultDomain, IPv4: defaultIPv4Address}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return NewResolver(registry, defaultTTL)
}

func packedDNSQuery(t *testing.T, domain string, qtype uint16) []byte {
	t.Helper()
	query := new(dns.Msg)
	query.SetQuestion(domain, qtype)
	packed, err := query.Pack()
	if err != nil {
		t.Fatalf("pack DNS query: %v", err)
	}
	return packed
}

func assertDoHQuery(t *testing.T, handler http.Handler, domain string, wantRcode int, wantIP string) {
	t.Helper()
	encoded := base64.RawURLEncoding.EncodeToString(packedDNSQuery(t, domain, dns.TypeA))
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s?dns=%s", dohPath, encoded), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertDoHResponse(t, response, wantRcode, wantIP)
}

func assertDoHResponse(t *testing.T, response *httptest.ResponseRecorder, wantRcode int, wantIP string) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != dnsMessageMediaType {
		t.Fatalf("Content-Type = %q, want %q", got, dnsMessageMediaType)
	}
	var message dns.Msg
	if err := message.Unpack(response.Body.Bytes()); err != nil {
		t.Fatalf("unpack DNS response: %v", err)
	}
	if message.Rcode != wantRcode {
		t.Fatalf("DNS Rcode = %d, want %d", message.Rcode, wantRcode)
	}
	if wantIP == "" {
		if len(message.Answer) != 0 {
			t.Fatalf("DNS response has %d answers, want none", len(message.Answer))
		}
		return
	}
	if len(message.Answer) != 1 {
		t.Fatalf("DNS response has %d answers, want 1", len(message.Answer))
	}
	answer, ok := message.Answer[0].(*dns.A)
	if !ok || answer.A.String() != wantIP {
		t.Fatalf("DNS answer = %v, want A %s", message.Answer[0], wantIP)
	}
}
