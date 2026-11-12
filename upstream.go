package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/miekg/dns"
)

const (
	defaultUpstreamDoHEndpoint = "https://cloudflare-dns.com/dns-query"
	upstreamTimeout            = 5 * time.Second
	maxUpstreamConcurrency     = 64
)

type dnsForwarder interface {
	Forward(context.Context, []byte) ([]byte, error)
}

type dohForwarder struct {
	endpoint string
	client   *http.Client
	limit    chan struct{}
}

func newCloudflareForwarder() *dohForwarder {
	transport := &http.Transport{
		ResponseHeaderTimeout: 4 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		MaxConnsPerHost:       maxUpstreamConcurrency,
		MaxIdleConnsPerHost:   16,
	}
	return &dohForwarder{
		endpoint: defaultUpstreamDoHEndpoint,
		client: &http.Client{
			Transport: transport,
			Timeout:   upstreamTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		limit: make(chan struct{}, maxUpstreamConcurrency),
	}
}

func newDoHForwarder(endpoint string, client *http.Client) *dohForwarder {
	return &dohForwarder{
		endpoint: endpoint,
		client:   client,
		limit:    make(chan struct{}, maxUpstreamConcurrency),
	}
}

func (forwarder *dohForwarder) Forward(ctx context.Context, queryWire []byte) ([]byte, error) {
	if len(queryWire) == 0 || len(queryWire) > maxDNSMessageSize {
		return nil, fmt.Errorf("invalid DNS message size")
	}
	var query dns.Msg
	if err := query.Unpack(queryWire); err != nil || query.Response {
		return nil, fmt.Errorf("invalid DNS query")
	}

	select {
	case forwarder.limit <- struct{}{}:
		defer func() { <-forwarder.limit }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, forwarder.endpoint, bytes.NewReader(queryWire))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}
	request.Header.Set("Content-Type", dnsMessageMediaType)
	request.Header.Set("Accept", dnsMessageMediaType)

	response, err := forwarder.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send upstream DNS request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned HTTP status %d", response.StatusCode)
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType != dnsMessageMediaType {
		return nil, fmt.Errorf("upstream returned an invalid content type")
	}

	responseWire, err := io.ReadAll(io.LimitReader(response.Body, maxDNSMessageSize+1))
	if err != nil {
		return nil, fmt.Errorf("read upstream DNS response: %w", err)
	}
	if len(responseWire) == 0 || len(responseWire) > maxDNSMessageSize {
		return nil, fmt.Errorf("upstream DNS response has an invalid size")
	}
	var answer dns.Msg
	if err := answer.Unpack(responseWire); err != nil {
		return nil, fmt.Errorf("upstream returned malformed DNS data")
	}
	if err := validateUpstreamAnswer(&query, &answer); err != nil {
		return nil, err
	}
	return responseWire, nil
}

func validateUpstreamAnswer(query, answer *dns.Msg) error {
	if !answer.Response || answer.Id != query.Id || len(answer.Question) != len(query.Question) {
		return fmt.Errorf("upstream response does not match the DNS request")
	}
	for i, question := range query.Question {
		responseQuestion := answer.Question[i]
		if !strings.EqualFold(dns.Fqdn(question.Name), dns.Fqdn(responseQuestion.Name)) ||
			question.Qtype != responseQuestion.Qtype || question.Qclass != responseQuestion.Qclass {
			return fmt.Errorf("upstream response question does not match the DNS request")
		}
	}
	return nil
}
