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
	if err := registry.ReplaceRecords(records); err != nil {
		return nil, err
	}
	return registry, nil
}

func (r *Registry) Set(record Record) error {
	record, err := normalizeRecord(record)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	replacement := cloneRecords(r.records)
	replacement[record.Domain] = record
	if err := validateCNAMELoops(replacement); err != nil {
		return err
	}
	r.records = replacement
	return nil
}

func (r *Registry) Add(record Record) error {
	record, err := normalizeRecord(record)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.records[record.Domain]; exists {
		return fmt.Errorf("domain %q already exists", record.Domain)
	}
	replacement := cloneRecords(r.records)
	replacement[record.Domain] = record
	if err := validateCNAMELoops(replacement); err != nil {
		return err
	}
	r.records = replacement
	return nil
}

func (r *Registry) Update(record Record) error {
	return r.update(record, false)
}

func (r *Registry) UpdateCNAME(record Record) error {
	if record.Target == "" {
		return fmt.Errorf("CNAME update requires a target hostname")
	}
	return r.update(record, true)
}

func (r *Registry) update(record Record, allowTypeChange bool) error {
	record, err := normalizeRecord(record)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, exists := r.records[record.Domain]
	if !exists {
		return fmt.Errorf("domain %q does not exist", record.Domain)
	}
	if !allowTypeChange && existing.Type() != record.Type() {
		return fmt.Errorf("domain %q is a %s record; use the matching update command", record.Domain, existing.Type())
	}
	replacement := cloneRecords(r.records)
	replacement[record.Domain] = record
	if err := validateCNAMELoops(replacement); err != nil {
		return err
	}
	r.records = replacement
	return nil
}

func (r *Registry) Delete(domain string) error {
	if err := ValidateDomainName(domain); err != nil {
		return fmt.Errorf("invalid record domain %q: %w", domain, err)
	}
	name := normalizeDomainName(domain)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.records[name]; !exists {
		return fmt.Errorf("domain %q does not exist", name)
	}
	delete(r.records, name)
	return nil
}

func (r *Registry) ReplaceRecords(records []Record) error {
	replacement := make(map[string]Record, len(records))
	for _, record := range records {
		normalized, err := normalizeRecord(record)
		if err != nil {
			return err
		}
		if _, exists := replacement[normalized.Domain]; exists {
			return fmt.Errorf("domain %q appears more than once", normalized.Domain)
		}
		replacement[normalized.Domain] = normalized
	}
	if err := validateCNAMELoops(replacement); err != nil {
		return err
	}

	r.mu.Lock()
	r.records = replacement
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

func normalizeRecord(record Record) (Record, error) {
	if err := ValidateDomainName(record.Domain); err != nil {
		return Record{}, fmt.Errorf("invalid record domain %q: %w", record.Domain, err)
	}
	if (record.IPv4 == "") == (record.Target == "") {
		return Record{}, fmt.Errorf("record %q must have exactly one of ipv4 or target", record.Domain)
	}
	if record.IPv4 != "" {
		if err := ValidateIPv4Address(record.IPv4); err != nil {
			return Record{}, fmt.Errorf("invalid IPv4 address for %q: %w", record.Domain, err)
		}
		record.IPv4 = net.ParseIP(record.IPv4).To4().String()
	} else {
		if err := ValidateHostname(record.Target); err != nil {
			return Record{}, fmt.Errorf("invalid CNAME target for %q: %w", record.Domain, err)
		}
		record.Target = normalizeDomainName(record.Target)
	}
	record.Domain = normalizeDomainName(record.Domain)
	return record, nil
}

func cloneRecords(records map[string]Record) map[string]Record {
	clone := make(map[string]Record, len(records)+1)
	for domain, record := range records {
		clone[domain] = record
	}
	return clone
}

func validateCNAMELoops(records map[string]Record) error {
	for domain, record := range records {
		if record.Target == "" {
			continue
		}
		visited := map[string]struct{}{domain: {}}
		target := normalizeDomainName(record.Target)
		for {
			if _, exists := visited[target]; exists {
				return fmt.Errorf("CNAME loop detected starting at %q", domain)
			}
			visited[target] = struct{}{}
			next, exists := records[target]
			if !exists || next.Target == "" {
				break
			}
			target = normalizeDomainName(next.Target)
		}
	}
	return nil
}
