package main

import (
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"

	"github.com/miekg/dns"
)

const (
	dnsMessageMediaType  = "application/dns-message"
	maxDNSMessageSize    = 65535
	maxEncodedDNSMessage = (maxDNSMessageSize + 2) / 3 * 4
)

type dohHandler struct {
	resolver dns.Handler
}

func newDoHHandler(resolver dns.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(dohPath, &dohHandler{resolver: resolver})
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
