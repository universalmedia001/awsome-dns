package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/miekg/dns"
)

type Resolver struct {
	registry     *Registry
	ttl          uint32
	databasePath string
	databaseMu   sync.Mutex
	databaseInfo os.FileInfo
	forwarder    dnsForwarder
}

func NewResolver(registry *Registry, ttl uint32) *Resolver {
	return NewResolverWithForwarder(registry, ttl, newCloudflareForwarder())
}

func NewResolverWithForwarder(registry *Registry, ttl uint32, forwarder dnsForwarder) *Resolver {
	return &Resolver{registry: registry, ttl: ttl, forwarder: forwarder}
}

func NewResolverWithDatabase(registry *Registry, ttl uint32, databasePath string) *Resolver {
	return NewResolverWithDatabaseAndForwarder(registry, ttl, databasePath, newCloudflareForwarder())
}

func NewResolverWithDatabaseAndForwarder(registry *Registry, ttl uint32, databasePath string, forwarder dnsForwarder) *Resolver {
	return &Resolver{
		registry:     registry,
		ttl:          ttl,
		databasePath: databasePath,
		forwarder:    forwarder,
	}
}

func (resolver *Resolver) ServeDNS(writer dns.ResponseWriter, request *dns.Msg) {
	if len(request.Question) == 1 && !isAWSOMEDomain(request.Question[0].Name) {
		queryWire, err := request.Pack()
		if err == nil {
			responseWire, forwardErr := resolver.forwarder.Forward(context.Background(), queryWire)
			if forwardErr == nil {
				_, _ = writer.Write(responseWire)
				return
			}
		}
		response := new(dns.Msg)
		response.SetReply(request)
		response.Authoritative = false
		response.Rcode = dns.RcodeServerFailure
		_ = writer.WriteMsg(response)
		return
	}

	response := resolver.resolveAuthoritative(request)
	_ = writer.WriteMsg(response)
}

func (resolver *Resolver) ExchangeWire(ctx context.Context, queryWire []byte) ([]byte, error) {
	if len(queryWire) == 0 || len(queryWire) > maxDNSMessageSize {
		return nil, fmt.Errorf("invalid DNS message size")
	}
	var request dns.Msg
	if err := request.Unpack(queryWire); err != nil || request.Response {
		return nil, fmt.Errorf("invalid DNS query")
	}
	if len(request.Question) == 1 && !isAWSOMEDomain(request.Question[0].Name) {
		return resolver.forwarder.Forward(ctx, queryWire)
	}
	response := resolver.resolveAuthoritative(&request)
	return response.Pack()
}

func isAWSOMEDomain(name string) bool {
	name = normalizeDomainName(name)
	return name == "awsome" || strings.HasSuffix(name, zoneSuffix)
}

func (resolver *Resolver) resolveAuthoritative(request *dns.Msg) *dns.Msg {
	resolver.refreshDatabase()
	response := new(dns.Msg)
	response.SetReply(request)
	response.Authoritative = true

	if len(request.Question) != 1 {
		response.Rcode = dns.RcodeFormatError
		return response
	}

	question := request.Question[0]
	if question.Qclass != dns.ClassINET {
		response.Rcode = dns.RcodeRefused
		return response
	}

	domain := normalizeDomainName(question.Name)
	if ValidateDomainName(domain) != nil {
		response.Rcode = dns.RcodeNameError
		return response
	}
	record, exists := resolver.registry.Lookup(domain)
	if !exists {
		response.Rcode = dns.RcodeNameError
		return response
	}
	if record.Target != "" {
		if question.Qtype == dns.TypeA || question.Qtype == dns.TypeCNAME {
			response.Answer = append(response.Answer, &dns.CNAME{
				Hdr: dns.RR_Header{
					Name:   dns.Fqdn(record.Domain),
					Rrtype: dns.TypeCNAME,
					Class:  dns.ClassINET,
					Ttl:    resolver.ttl,
				},
				Target: dns.Fqdn(record.Target),
			})
		}
	} else if question.Qtype == dns.TypeA {
		response.Answer = append(response.Answer, &dns.A{
			Hdr: dns.RR_Header{
				Name:   dns.Fqdn(record.Domain),
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    resolver.ttl,
			},
			A: net.ParseIP(record.IPv4).To4(),
		})
	}
	return response
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
