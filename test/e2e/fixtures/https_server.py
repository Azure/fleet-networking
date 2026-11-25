import http.server
import json
import os
import ssl
import time
import urllib.parse


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        nonce = urllib.parse.parse_qs(parsed.query).get("proof", [""])[0]
        result = {
            "proof_id": os.environ["FLEET_PROOF_ID"],
            "cluster_id": os.environ["MEMBER_CLUSTER_ID"],
            "request_id": nonce,
            "host": self.headers.get("Host", ""),
            "sni": getattr(self.connection, "fleet_sni", ""),
            "protocol": self.connection.version(),
        }
        print(json.dumps({"event": "tls_request", "path": parsed.path,
                          "probe": "Health Probe" in self.headers.get("User-Agent", ""),
                          **result}), flush=True)
        data = json.dumps(result).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(data)


def observe_sni(connection, name, _context):
    connection.fleet_sni = name or ""


def validated_context(bundle, hostname, client_context=None):
    if os.stat(bundle).st_size > 1024 * 1024:
        raise ValueError("CSI certificate bundle exceeds 1 MiB")
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(bundle, password=lambda: "")
    client_context = client_context or ssl.create_default_context()
    client_context.hostname_checks_common_name = False
    server_in, server_out, client_in, client_out = (ssl.MemoryBIO() for _ in range(4))
    server = context.wrap_bio(server_in, server_out, server_side=True)
    client = client_context.wrap_bio(client_in, client_out, server_hostname=hostname)
    peers = [(client, client_out, server_in), (server, server_out, client_in)]
    done = set()
    # Validate exactly the loaded chain/key with an in-memory, verified handshake.
    for _ in range(16):
        for peer, outgoing, incoming in peers:
            if peer not in done:
                try:
                    peer.do_handshake()
                    done.add(peer)
                except ssl.SSLWantReadError:
                    pass
            incoming.write(outgoing.read())
        if len(done) == 2:
            break
    else:
        raise ValueError("certificate validation handshake did not finish")
    expires = ssl.cert_time_to_seconds(client.getpeercert()["notAfter"])
    if expires - time.time() < 7 * 24 * 60 * 60:
        raise ValueError("serving certificate requires at least seven days remaining")
    return context


def main():
    context = validated_context("/tls/serving.pem", os.environ["ORIGIN_TLS_HOSTNAME"])
    context.set_servername_callback(observe_sni)
    server = http.server.ThreadingHTTPServer(("0.0.0.0", 8443), Handler)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
