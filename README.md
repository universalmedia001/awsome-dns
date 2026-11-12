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

## DNS resolution and upstream forwarding

The resolver handles `.awsome` names authoritatively from `domains.json`.
Known names use the local A or CNAME record; unknown names in that namespace
return `NXDOMAIN` and are never forwarded.

Queries for names outside `.awsome` are forwarded as DNS wire messages by
HTTP POST to Cloudflare's DoH endpoint
(`https://cloudflare-dns.com/dns-query`). This includes standard query types
such as A, AAAA, CNAME, MX, and TXT. Successful upstream DNS responses are
returned without rewriting their wire-format response. If the upstream times
out or returns an invalid response, DNS clients receive `SERVFAIL` and DoH
clients receive HTTP `502`.

The public DoH endpoint can therefore resolve ordinary internet names as well
as private `.awsome` records. DoH requests are limited to a burst of 60
requests per client address, replenishing at one request per second; a global
limit of 64 concurrent upstream requests and the existing 65,535-byte DNS
message limit also apply. The address is taken from the TCP peer, not an
untrusted forwarded header. Behind a reverse proxy, peer addresses may be
shared, so configure an additional per-client rate limit at the proxy if
needed. Upstream availability depends on Cloudflare DoH and outbound HTTPS
connectivity.

## Client configuration

**Provider:** AWSOME DNS

**DNS-over-HTTPS endpoint:** [https://awsome-dns.onrender.com/dns-query](https://awsome-dns.onrender.com/dns-query)

Names in the private `.awsome` namespace are not globally delegated. A device
or application must send its DNS queries to AWSOME DNS to resolve them.
Configuring DoH in a browser affects that browser's DNS lookups; it does not
necessarily change the resolver used by other applications on the device.

### Google Chrome

1. Open **Settings → Privacy and security → Security**.
2. Enable **Use secure DNS**.
3. If your Chrome version offers a custom provider option (often **With
   custom**), choose it and enter:
   `https://awsome-dns.onrender.com/dns-query`

Chrome's available choices vary by version and platform. If no custom provider
field is offered, Chrome cannot be configured with this endpoint through that
version's settings.

### Mozilla Firefox

1. Open **Settings → Privacy & Security**.
2. Find **DNS over HTTPS**.
3. Select the option to use a custom provider, if available, and enter:
   `https://awsome-dns.onrender.com/dns-query`

Firefox labels and availability vary by version and managed-device policy.

### Windows 11

On Windows 11 builds that support DNS-over-HTTPS, open **Settings → Network &
internet**, select the active Wi-Fi or Ethernet connection, and edit **DNS
server assignment**. Windows' built-in settings may offer DoH only for
recognized providers. Configure AWSOME DNS only if your Windows version or
management tooling permits a custom DoH server/template. The endpoint must be
entered in the template syntax Windows expects; an RFC 8484 URI-template form
is typically:
`https://awsome-dns.onrender.com/dns-query{?dns}`.

Do not assume every Windows version or settings UI supports arbitrary custom
DoH providers. If the endpoint cannot be added as a compatible template, use a
DoH-capable client that supports custom providers.

### Android

Android's built-in **Private DNS** setting normally uses DNS-over-TLS (DoT),
not arbitrary DNS-over-HTTPS endpoints. AWSOME DNS currently provides DoH; it
does not provide DoT. To use AWSOME DNS on Android, use a compatible
third-party DNS client that supports a custom DoH provider and enter the
endpoint above.

### iPhone and iPad

iOS and iPadOS may require a compatible DNS-over-HTTPS client or a
configuration profile to use a custom DoH endpoint. No AWSOME DNS profile is
provided by this project. Use only a profile or client that explicitly supports
custom DoH and configure the endpoint above.

### Testing the public endpoint

Check that the Render service is awake and healthy:

```sh
curl -i https://awsome-dns.onrender.com/health
```

Expect HTTP `200` and the body `ok`. A sleeping Render Free service can take
time to wake before responding.

The following standard-library Python script sends real RFC 8484 GET queries
for `google.com` and `quali-boost.awsome` and prints their DNS answer records:

```sh
python3 - <<'PY'
import base64
import ipaddress
import struct
import urllib.parse
import urllib.request

endpoint = "https://awsome-dns.onrender.com/dns-query"

def decode_name(packet, offset):
    labels = []
    while True:
        length = packet[offset]
        offset += 1
        if length == 0:
            return ".".join(labels) + ".", offset
        if length & 0xC0 == 0xC0:
            pointer = ((length & 0x3F) << 8) | packet[offset]
            suffix, _ = decode_name(packet, pointer)
            return (".".join(labels) + "." if labels else "") + suffix, offset + 1
        labels.append(packet[offset:offset + length].decode("ascii"))
        offset += length

def lookup(domain):
    query = struct.pack("!HHHHHH", 0xA710, 0x0100, 1, 0, 0, 0)
    query += b"".join(bytes([len(label)]) + label.encode("ascii")
                      for label in domain.split("."))
    query += b"\0" + struct.pack("!HH", 1, 1)
    encoded = base64.urlsafe_b64encode(query).decode("ascii").rstrip("=")
    url = endpoint + "?dns=" + urllib.parse.quote(encoded)
    request = urllib.request.Request(url, headers={"Accept": "application/dns-message"})
    with urllib.request.urlopen(request, timeout=20) as response:
        packet = response.read()
        if response.status != 200 or response.headers.get_content_type() != "application/dns-message":
            raise RuntimeError(f"unexpected HTTP response for {domain}")
    _, flags, questions, answers, _, _ = struct.unpack("!HHHHHH", packet[:12])
    offset = 12
    for _ in range(questions):
        _, offset = decode_name(packet, offset)
        offset += 4
    records = []
    for _ in range(answers):
        owner, offset = decode_name(packet, offset)
        rtype, _, _, size = struct.unpack("!HHIH", packet[offset:offset + 10])
        offset += 10
        value = packet[offset:offset + size]
        if rtype == 1 and size == 4:
            records.append(f"{owner} A {ipaddress.ip_address(value)}")
        elif rtype == 5:
            target, _ = decode_name(packet, offset)
            records.append(f"{owner} CNAME {target}")
        offset += size
    rcode = flags & 0x0F
    print(f"{domain}: rcode={rcode}; " + (", ".join(records) if records else "no answers"))

lookup("google.com")
lookup("quali-boost.awsome")
PY
```

`google.com` should return a successful DNS response with one or more current
A or CNAME answers (addresses may change). `quali-boost.awsome` should return
its configured CNAME target, `quali-boost-28h4.vercel.app`. The resolver
returns the alias target; it does not recursively resolve that target to an IP.

### Client and hosting limitations

- Render Free services can sleep when idle, so the first request may be delayed
  while the service wakes.
- `.awsome` is private to this resolver and is not globally delegated.
- Adding a DNS record does not configure Vercel to serve that hostname. The
  hosting provider must separately recognize and configure the custom domain.
- Public HTTPS certificates for private `.awsome` names are not automatically
  available. A DNS answer alone does not provision a certificate.

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
