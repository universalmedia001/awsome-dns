package main

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	dnsMessageMediaType  = "application/dns-message"
	maxDNSMessageSize    = 65535
	maxEncodedDNSMessage = (maxDNSMessageSize + 2) / 3 * 4
	dohRequestRate       = 1
	dohRequestBurst      = 60
	maxRateLimitClients  = 4096
)

type dohHandler struct {
	resolver dns.Handler
	limiter  *requestRateLimiter
}

func newDoHHandler(resolver dns.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(dohPath, &dohHandler{resolver: resolver, limiter: newRequestRateLimiter()})
	mux.HandleFunc("/health", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "ok\n")
	})
	return mux
}

func (handler *dohHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != dohPath {
		http.NotFound(writer, request)
		return
	}

	var message []byte
	switch request.Method {
	case http.MethodGet:
		encoded := request.URL.Query().Get("dns")
		if encoded == "" || len(encoded) > maxEncodedDNSMessage {
			http.Error(writer, "invalid DNS query parameter", http.StatusBadRequest)
			return
		}
		decoded, err := decodeDNSQuery(encoded)
		if err != nil {
			http.Error(writer, "invalid DNS query parameter", http.StatusBadRequest)
			return
		}
		message = decoded
	case http.MethodPost:
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != dnsMessageMediaType {
			http.Error(writer, "Content-Type must be application/dns-message", http.StatusUnsupportedMediaType)
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, maxDNSMessageSize)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(writer, "DNS message is too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(writer, "invalid DNS message body", http.StatusBadRequest)
			return
		}
		message = body
	default:
		writer.Header().Set("Allow", "GET, POST")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var query dns.Msg
	if err := query.Unpack(message); err != nil || query.Response {
		http.Error(writer, "invalid DNS wire-format request", http.StatusBadRequest)
		return
	}

	if !handler.limiter.Allow(request.RemoteAddr) {
		writer.Header().Set("Retry-After", "1")
		http.Error(writer, "request rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	if wireHandler, ok := handler.resolver.(interface {
		ExchangeWire(context.Context, []byte) ([]byte, error)
	}); ok {
		response, err := wireHandler.ExchangeWire(request.Context(), message)
		if err != nil {
			http.Error(writer, "DNS upstream unavailable", http.StatusBadGateway)
			return
		}
		writer.Header().Set("Content-Type", dnsMessageMediaType)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(response)
		return
	}

	responseWriter := &dohResponseWriter{}
	handler.resolver.ServeDNS(responseWriter, &query)
	if responseWriter.err != nil || len(responseWriter.message) == 0 {
		http.Error(writer, "DNS query could not be processed", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", dnsMessageMediaType)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(responseWriter.message)
}

type rateLimitEntry struct {
	tokens float64
	last   time.Time
}

type requestRateLimiter struct {
	mu      sync.Mutex
	clients map[string]rateLimitEntry
}

func newRequestRateLimiter() *requestRateLimiter {
	return &requestRateLimiter{clients: make(map[string]rateLimitEntry)}
}

func (limiter *requestRateLimiter) Allow(remoteAddress string) bool {
	client, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		client = remoteAddress
	}
	now := time.Now()

	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	entry, exists := limiter.clients[client]
	if !exists {
		if len(limiter.clients) >= maxRateLimitClients {
			for key, existing := range limiter.clients {
				if now.Sub(existing.last) > 10*time.Minute {
					delete(limiter.clients, key)
				}
			}
		}
		if len(limiter.clients) >= maxRateLimitClients {
			return false
		}
		entry = rateLimitEntry{tokens: dohRequestBurst, last: now}
	} else {
		entry.tokens += now.Sub(entry.last).Seconds() * dohRequestRate
		if entry.tokens > dohRequestBurst {
			entry.tokens = dohRequestBurst
		}
		entry.last = now
	}
	if entry.tokens < 1 {
		limiter.clients[client] = entry
		return false
	}
	entry.tokens--
	limiter.clients[client] = entry
	return true
}

func decodeDNSQuery(encoded string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(encoded)
}

type dohResponseWriter struct {
	message []byte
	err     error
}

func (writer *dohResponseWriter) LocalAddr() net.Addr  { return &net.TCPAddr{} }
func (writer *dohResponseWriter) RemoteAddr() net.Addr { return &net.TCPAddr{} }
func (writer *dohResponseWriter) Write(message []byte) (int, error) {
	writer.message = append(writer.message[:0], message...)
	return len(message), nil
}
func (writer *dohResponseWriter) Close() error        { return nil }
func (writer *dohResponseWriter) TsigStatus() error   { return nil }
func (writer *dohResponseWriter) TsigTimersOnly(bool) {}
func (writer *dohResponseWriter) Hijack()             {}
func (writer *dohResponseWriter) WriteMsg(message *dns.Msg) error {
	packed, err := message.Pack()
	if err != nil {
		writer.err = err
		return err
	}
	writer.message = packed
	return nil
}
