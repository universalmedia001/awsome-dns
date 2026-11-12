package main

import (
	"fmt"
	"net"
	"sort"
	"sync"
)

type Registry struct {
	mu      sync.RWMutex
	records map[string]Record
}

func NewRegistry(records []Record) (*Registry, error) {
	registry := &Registry{records: make(map[string]Record, len(records))}
	for _, record := range records {
		if err := registry.Set(record); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Set(record Record) error {
	if err := ValidateDomainName(record.Domain); err != nil {
		return fmt.Errorf("invalid record domain %q: %w", record.Domain, err)
	}
	if err := ValidateIPv4Address(record.IPv4); err != nil {
		return fmt.Errorf("invalid IPv4 address for %q: %w", record.Domain, err)
	}

	record.Domain = normalizeDomainName(record.Domain)
	record.IPv4 = net.ParseIP(record.IPv4).To4().String()
	r.mu.Lock()
	r.records[record.Domain] = record
	r.mu.Unlock()
	return nil
}

func (r *Registry) Lookup(domain string) (Record, bool) {
	name := normalizeDomainName(domain)
	r.mu.RLock()
	record, ok := r.records[name]
	r.mu.RUnlock()
	return record, ok
}

func (r *Registry) Records() []Record {
	r.mu.RLock()
	records := make([]Record, 0, len(r.records))
	for _, record := range r.records {
		records = append(records, record)
	}
	r.mu.RUnlock()

	sort.Slice(records, func(i, j int) bool {
		return records[i].Domain < records[j].Domain
	})
	return records
}
