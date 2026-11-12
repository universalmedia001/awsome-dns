# AWSOME DNS

A private DNS system for the `.awsome` domain namespace.

## Project Goal
Build a custom private DNS engine that allows domains such as `mrawsomeltd.awsome` to resolve to websites through our own DNS resolver.

## Technology
- Go (Golang)
- DNS protocol
- GitHub Codespaces

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
