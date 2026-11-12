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
`http://127.0.0.1:8054/dns-query`, and `GET /health` returns a minimal health
status. Set `AWSOME_DNS_HTTP_ADDR` to change the HTTP listen address, for
example:

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

## Deploying on Render Free

Create a **Web Service** from this repository and use the following commands:

- Build: `go build -o awsome-dns .`
- Start: `./awsome-dns`
- Health check path: `/health`

Render supplies `PORT` at runtime. When `AWSOME_DNS_HTTP_ADDR` is not set, the
DoH HTTP server binds to `0.0.0.0:$PORT`, as required for Render web services.
An explicitly set `AWSOME_DNS_HTTP_ADDR` takes precedence. Render provides
HTTPS for the web service URL; use `https://<your-service>.onrender.com/dns-query`
for DoH and `https://<your-service>.onrender.com/health` for health checks.
The application itself only serves HTTP, behind Render's HTTPS termination.

On Render Free, public access is through the HTTP(S) web service. Do not rely
on the existing UDP or TCP DNS listener being publicly reachable there; use
DoH as the public DNS transport.

Free web services spin down after 15 minutes without inbound traffic and can
take about a minute to start again when requested. While spun down, requests
may see a startup delay. Render's free web-service filesystem is ephemeral:
changes to `domains.json` are lost when the service spins down, restarts, or
redeploys. The CLI continues to work, but changes made on the running instance
are not durable and must not be treated as persistent records.

There is no safe, permanent free Render filesystem option for this JSON
database. Render persistent disks require a paid service. Render's free
PostgreSQL expires after 30 days, and free Key Value is in-memory, so neither
is a durable, drop-in replacement for this database. For a no-cost development
workflow, keep `domains.json` under version control, make record changes in the
repository, and redeploy; this preserves a reviewed source of truth, but it is
not live persistence for CLI changes made on Render. Durable runtime updates
require a separately selected persistent storage service and a deliberate
database integration.

## Domain management

The server loads domain records from `domains.json` in its working directory.
Manage records with the command-line interface:

```sh
./awsome-dns add example.awsome 8.8.8.8
./awsome-dns add-cname quali-boost.awsome quali-boost-28h4.vercel.app
./awsome-dns list
./awsome-dns update example.awsome 1.1.1.1
./awsome-dns update-cname quali-boost.awsome new-target.example.com
./awsome-dns delete example.awsome
```

The `list` command shows each domain, its record type, and its value. Domain
changes are saved atomically to the JSON database. On persistent local storage,
a running DNS server reloads the database when it changes and applies new
records on subsequent queries. On Render Free, the database file is ephemeral
as described above. Add rejects duplicates; update and delete require an
existing domain.

A CNAME response points to its configured hostname. This DNS server does not
recursively resolve that hostname or return its IP address; a DNS client or
upstream resolver must perform the subsequent lookup.

`update-cname` updates an existing CNAME target and can also convert an
existing A record into a CNAME record, replacing the A value for that domain.

The JSON database keeps existing A entries in the `domain` and `ipv4` format.
CNAME entries use `domain` and `target`; each entry must contain exactly one
of `ipv4` or `target`.

## Development Rules
- No frontend or dashboard.
- Build the DNS engine entirely through code.
- Keep the project lightweight and modular.
- Prepare the project to run inside GitHub Codespaces.
