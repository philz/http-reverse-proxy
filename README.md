# http-reverse-proxy

Tunnels HTTP requests to a server behind a firewall by reversing the
connection direction. The firewalled machine connects *out* to the
public server; the public server then sends requests *back* through
that connection.

```
         internet            firewall
 client ──────→ server A ╌╌╌╌╌╌╌╌╌╌╌→ server B ──→ localhost:PORT
                :8000      (H2 over     :8000        :1234
                          hijacked TCP)
```

B initiates the TCP connection to A. A hijacks it, upgrades to HTTP/2,
and uses it as a transport to reverse-proxy all incoming requests to B.
B forwards them to a local port.

## Usage

On the public machine (A):

    http-reverse-proxy --listen :8000 --secret mytoken

On the firewalled machine (B):

    http-reverse-proxy --forward 1234 A:8000 --secret mytoken

Requests to `A:8000` are now forwarded to `B:1234`.

## Protocol

1. B sends `POST /__reverse_proxy` with `Upgrade: reverse-proxy` and
   `X-Reverse-Proxy-Secret` headers
2. A validates the secret (constant-time), hijacks the connection,
   replies `101 Switching Protocols`
3. Magic byte exchange for synchronization
4. A creates an HTTP/2 client, B starts an HTTP/2 server — both over
   the same TCP connection
5. A reverse-proxies all other requests through the H2 connection to B
