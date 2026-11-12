package main

import (
	"net"

	"github.com/miekg/dns"
)

type Resolver struct {
	registry *Registry
	ttl      uint32
}

func NewResolver(registry *Registry, ttl uint32) *Resolver {
	return &Resolver{registry: registry, ttl: ttl}
}

func (resolver *Resolver) ServeDNS(writer dns.ResponseWriter, request *dns.Msg) {
	response := new(dns.Msg)
	response.SetReply(request)
	response.Authoritative = true

	if len(request.Question) != 1 {
		response.Rcode = dns.RcodeFormatError
		_ = writer.WriteMsg(response)
		return
	}

	question := request.Question[0]
	if question.Qclass != dns.ClassINET {
		response.Rcode = dns.RcodeRefused
		_ = writer.WriteMsg(response)
		return
	}

	domain := normalizeDomainName(question.Name)
	if ValidateDomainName(domain) != nil {
		response.Rcode = dns.RcodeNameError
		_ = writer.WriteMsg(response)
		return
	}
	record, exists := resolver.registry.Lookup(domain)
	if !exists {
		response.Rcode = dns.RcodeNameError
		_ = writer.WriteMsg(response)
		return
	}
	if question.Qtype != dns.TypeA {
		response.Rcode = dns.RcodeNotImplemented
		_ = writer.WriteMsg(response)
		return
	}

	response.Answer = append(response.Answer, &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(record.Domain),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    resolver.ttl,
		},
		A: net.ParseIP(record.IPv4).To4(),
	})
	_ = writer.WriteMsg(response)
}
