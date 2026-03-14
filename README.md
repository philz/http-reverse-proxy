Warning: this is vibe coded based on an idea @crawshaw told me about.

# http-reverse-proxy

If B can connect to A, but A can't connect to B, you can
use this tunnel to allow A to connect to B. It works
by doing a request from B to A, and then hijacking
the underlying TCP connection.

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

This is analogous to `ssh -R 1234:localhost:1234 remote` — the
firewalled machine initiates the outbound connection, and traffic
flows back through it in reverse. Instead of SSH, this uses HTTP
connection hijacking and HTTP/2 multiplexing as the transport, so
the proxied traffic is plain HTTP and works with standard HTTP
clients and servers with no tunneling overhead.

## Usage

On the public machine (A):

    http-reverse-proxy listen --addr :8000 --secret mytoken

On the firewalled machine (B):

    http-reverse-proxy attach --forward 1234 --secret mytoken A:8000

Requests to `A:8000` are now forwarded to `B:1234`.

### Extra headers

Pass `-H` to send additional headers with the attach request:

    http-reverse-proxy attach --forward 1234 --secret mytoken \
        -H "X-Region:us-east-1" -H "X-Instance:abc123" A:8000

## Protocol

1. B sends `POST /__reverse_proxy` with `Upgrade: reverse-proxy` and
   `X-Reverse-Proxy-Secret` headers (plus any `-H` headers)
2. A validates the secret, hijacks the connection, replies `101 Switching Protocols`
3. Magic byte exchange for synchronization
4. A creates an HTTP/2 client, B starts an HTTP/2 server — both over
   the same TCP connection
5. A reverse-proxies all other requests through the H2 connection to B
