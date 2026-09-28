---
title: Serve over HTTPS
description: Let Chowki serve HTTPS with your certificate, or put it behind a reverse proxy that does.
type: how-to
since: v0.1
edition: community
last_reviewed: 2026-09-28
---

Clients send virtual keys and prompts to the gateway, so encrypt that traffic whenever it leaves
the machine. Chowki can serve HTTPS itself with your certificate, or run behind a reverse proxy,
such as Caddy or nginx, that serves HTTPS for it.

## Before you begin

- A certificate and its private key, in PEM files, for the name that clients use, such as
  `gateway.example.com`. A certificate authority such as Let's Encrypt issues them for free.

## Serve HTTPS with Chowki

1. Put the files where the user that runs `chowki serve` can read them, and make the key readable
   only by that user:

   ```sh
   chmod 600 tls/key.pem
   ```

2. Name them in the `server` section of `chowki.yaml`, and choose the port, such as 8443:

   ```yaml
   server:
     listen: ":8443"
     tls_cert_file: tls/cert.pem
     tls_key_file: tls/key.pem
   ```

   The certificate file holds your certificate, followed by any intermediate certificates.

3. Restart `chowki serve`. Its log says that it serves HTTPS:

   ```text
   {"time":"2026-09-28T06:50:42.704866665+05:00","level":"INFO","msg":"listening","addr":"[::]:8443","scheme":"https"}
   ```

Chowki serves TLS 1.2 and 1.3, and HTTP/2 to clients that support it. Point clients at the new
address, such as `https://gateway.example.com:8443/v1`; `chowki setup --url` prints their settings
with it.

When the certificate is renewed, replace the two files: Chowki loads them again within 30 seconds,
without a restart. If the new files don't load, it keeps serving the old certificate and logs a
warning.

## Verify

Ask for the gateway's readiness over HTTPS. With a self-signed certificate, as here, tell curl to
trust it; leave out `--cacert` for a certificate from a public authority:

```sh
curl -sS --cacert tls/cert.pem https://localhost:8443/readyz
```

Output:

```text
{"status":"ready"}
```

`chowki doctor` checks the certificate, and warns 14 days before it expires:

```text
ok    tls                 certificate for localhost, 127.0.0.1, valid until 2026-12-27
ok    listen              chowki serve is running at https://127.0.0.1:8443
```

## Use a reverse proxy instead

Leave the TLS settings out, let the gateway listen on the machine only, such as with
`listen: "127.0.0.1:8080"`, and let the proxy serve HTTPS and forward to it. Configure the proxy
not to buffer responses, so that streams reach clients as they're written, and to allow long
responses: a stream can last as long as `server.upstream_timeout`. The proxy should send
`X-Forwarded-Proto: https`, so that the dashboard marks its cookies as secure.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `chowki serve` fails with `load the TLS certificate` | A file is missing, isn't PEM, or the key doesn't match the certificate. | Check the paths, and that the key belongs to the certificate. |
| `server.tls_cert_file: set it and server.tls_key_file together, or neither` | Only one of the two settings is set. | Set both. |
| Clients report an unknown certificate authority | The certificate file lacks the intermediate certificates. | Append them to the certificate file, after the certificate. |

## Related

- [Configuration reference](../reference/configuration.md) ·
  [Check your setup](check-your-setup.md)
