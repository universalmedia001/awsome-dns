package main

import (
	"log"
	"net"
	"os"
	"sync"

	"github.com/miekg/dns"
)

type Resolver struct {
	registry     *Registry
	ttl          uint32
	databasePath string
	databaseMu   sync.Mutex
	databaseInfo os.FileInfo
}

func NewResolver(registry *Registry, ttl uint32) *Resolver {
	return &Resolver{registry: registry, ttl: ttl}
}

func NewResolverWithDatabase(registry *Registry, ttl uint32, databasePath string) *Resolver {
	return &Resolver{
		registry:     registry,
		ttl:          ttl,
		databasePath: databasePath,
	}
}

func (resolver *Resolver) ServeDNS(writer dns.ResponseWriter, request *dns.Msg) {
	resolver.refreshDatabase()

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

func (resolver *Resolver) refreshDatabase() {
	if resolver.databasePath == "" {
		return
	}

	resolver.databaseMu.Lock()
	defer resolver.databaseMu.Unlock()

	info, err := os.Stat(resolver.databasePath)
	if err != nil {
		log.Printf("refresh DNS database: stat %q: %v", resolver.databasePath, err)
		return
	}
	if resolver.databaseInfo != nil &&
		os.SameFile(resolver.databaseInfo, info) &&
		resolver.databaseInfo.Size() == info.Size() &&
		resolver.databaseInfo.ModTime().Equal(info.ModTime()) {
		return
	}

	records, err := LoadDatabase(resolver.databasePath)
	if err != nil {
		log.Printf("refresh DNS database: %v", err)
		return
	}
	replacement, err := NewRegistry(records)
	if err != nil {
		log.Printf("refresh DNS database: validate records: %v", err)
		return
	}
	if err := resolver.registry.ReplaceRecords(replacement.Records()); err != nil {
		log.Printf("refresh DNS database: replace records: %v", err)
		return
	}
	resolver.databaseInfo = info
}
