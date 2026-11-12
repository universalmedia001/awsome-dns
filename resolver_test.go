package main

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

type testResponseWriter struct {
	message *dns.Msg
}

func (writer *testResponseWriter) LocalAddr() net.Addr       { return &net.UDPAddr{} }
func (writer *testResponseWriter) RemoteAddr() net.Addr      { return &net.UDPAddr{} }
func (writer *testResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (writer *testResponseWriter) Close() error              { return nil }
func (writer *testResponseWriter) TsigStatus() error         { return nil }
func (writer *testResponseWriter) TsigTimersOnly(bool)       {}
func (writer *testResponseWriter) Hijack()                   {}
func (writer *testResponseWriter) WriteMsg(message *dns.Msg) error {
	writer.message = message
	return nil
}

func TestResolverResponses(t *testing.T) {
	registry, err := NewRegistry([]Record{{Domain: "mrawsomeltd.awsome", IPv4: "192.0.2.10"}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	resolver := NewResolver(registry, 300)

	tests := []struct {
		name        string
		domain      string
		qtype       uint16
		wantRcode   int
		wantAnswers int
	}{
		{name: "known A record", domain: "mrawsomeltd.awsome.", qtype: dns.TypeA, wantAnswers: 1},
		{name: "unknown domain", domain: "unknown.awsome.", qtype: dns.TypeA, wantRcode: dns.RcodeNameError},
		{name: "unsupported type", domain: "mrawsomeltd.awsome.", qtype: dns.TypeAAAA, wantRcode: dns.RcodeNotImplemented},
		{name: "outside zone", domain: "example.com.", qtype: dns.TypeA, wantRcode: dns.RcodeNameError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := new(dns.Msg)
			request.SetQuestion(test.domain, test.qtype)
			writer := &testResponseWriter{}
			resolver.ServeDNS(writer, request)

			if writer.message == nil {
				t.Fatal("resolver did not write a response")
			}
			if writer.message.Rcode != test.wantRcode {
				t.Errorf("response Rcode = %d, want %d", writer.message.Rcode, test.wantRcode)
			}
			if len(writer.message.Answer) != test.wantAnswers {
				t.Fatalf("response has %d answers, want %d", len(writer.message.Answer), test.wantAnswers)
			}
			if test.wantAnswers == 1 {
				answer, ok := writer.message.Answer[0].(*dns.A)
				if !ok {
					t.Fatalf("answer type = %T, want *dns.A", writer.message.Answer[0])
				}
				if got := answer.A.String(); got != "192.0.2.10" {
					t.Errorf("A record = %s, want 192.0.2.10", got)
				}
			}
		})
	}
}
