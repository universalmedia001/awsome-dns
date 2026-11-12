# AWSOME DNS

A private DNS system for the `.awsome` domain namespace.

## Project Goal
Build a custom private DNS engine that allows domains such as `mrawsomeltd.awsome` to resolve to websites through our own DNS resolver.

## Technology
- Go (Golang)
- DNS protocol
- GitHub Codespaces

## Starting DNS and DNS-over-HTTPS

Build and start the service from the directory containing `domains.json`:

```sh
go build -o awsome-dns .
./awsome-dns
```

By default, the existing DNS service listens on `127.0.0.1:8053` for UDP and
TCP. The DoH endpoint listens on `127.0.0.1:8054` over plain HTTP at
`http://127.0.0.1:8054/dns-query`. Set `AWSOME_DNS_HTTP_ADDR` to change the
HTTP listen address, for example:

```sh
AWSOME_DNS_HTTP_ADDR=127.0.0.1:8080 ./awsome-dns
```

DoH accepts GET requests with a base64url-encoded DNS wire message in the
`dns` query parameter and POST requests with the raw DNS wire message and
`Content-Type: application/dns-message`. Successful responses contain a DNS
wire-format message with the same content type. For example, create a sample
wire-format request and send it with either method:

```sh
python3 - <<'PY'
import struct

labels = b"".join(bytes([len(label)]) + label for label in (b"mrawsomeltd", b"awsome"))
query = struct.pack("!HHHHHH", 0x1234, 0x0100, 1, 0, 0, 0)
query += labels + b"\0" + struct.pack("!HH", 1, 1)
open("query.bin", "wb").write(query)
PY

dns_query=$(python3 -c 'import base64; print(base64.urlsafe_b64encode(open("query.bin", "rb").read()).decode().rstrip("="))')
curl --get --data-urlencode "dns=$dns_query" \
  http://127.0.0.1:8054/dns-query

curl -X POST -H 'Content-Type: application/dns-message' \
  --data-binary @query.bin http://127.0.0.1:8054/dns-query
```

The HTTP listener does not provide TLS. For deployment, keep it bound to a
trusted interface and put an HTTPS reverse proxy in front of it. Configure the
proxy with a certificate for your DNS hostname and forward `/dns-query` to
`http://127.0.0.1:8054/dns-query`; the proxy terminates TLS while this service
continues to handle DNS-over-HTTPS. Do not expose the plain HTTP listener as if
it were HTTPS. For example, an Nginx HTTPS virtual host can proxy this path:

```nginx
server {
    listen 443 ssl;
    server_name dns.example.com;
    ssl_certificate /path/to/fullchain.pem;
    ssl_certificate_key /path/to/private-key.pem;

    location = /dns-query {
        proxy_pass http://127.0.0.1:8054/dns-query;
        proxy_set_header Host $host;
    }
}
```

Provide certificate files through your deployment's certificate-management
process; they are not configured in this application.

GitHub Codespaces is suitable for development and testing only. Its port
forwarding and lifecycle are not a stable, always-on public DNS/DoH hosting
service; use dedicated infrastructure or a DNS hosting provider for permanent
public service.

## Domain management

The server loads domain records from `domains.json` in its working directory.
Manage records with the command-line interface:

```sh
./awsome-dns add example.awsome 8.8.8.8
./awsome-dns list
./awsome-dns update example.awsome 1.1.1.1
./awsome-dns delete example.awsome
```

Domain changes are saved atomically to the JSON database. A running DNS server
reloads the database when it changes, and applies the new records on subsequent
queries. Add rejects duplicates; update and delete require an existing domain.

## Development Rules
- No frontend or dashboard.
- Build the DNS engine entirely through code.
- Keep the project lightweight and modular.
- Prepare the project to run inside GitHub Codespaces.
