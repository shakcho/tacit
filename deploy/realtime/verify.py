"""Verify bounded incremental SSE through the two-replica ingress fixture."""

import json
import os
from contextlib import closing
from urllib.request import Request, urlopen


def request(origin, bearer, path, headers):
    return urlopen(
        Request(origin + path, headers={"Authorization": "Bearer " + bearer, **headers}),
        timeout=15,
    )


def attach(origin, bearer, cursor):
    return request(origin, bearer, "/v1/events", {
        "Accept": "text/event-stream",
        "Last-Event-ID": cursor,
    })


def verify_handshake(response):
    if (
        response.headers.get_content_type() != "text/event-stream"
        or response.headers.get("Cache-Control") != "no-store, no-transform"
        or response.headers.get("X-Accel-Buffering") != "no"
        or response.readline(2048) != b": connected\n"
        or response.readline(2048) != b"\n"
    ):
        raise RuntimeError("The ingress did not forward the SSE handshake incrementally")
    return response.headers.get("X-Tacit-Fixture-Replica")


def main():
    origin = os.environ.get("TACIT_FIXTURE_ORIGIN", "http://127.0.0.1:18080")
    bearer = os.environ["TACIT_FIXTURE_BEARER"]
    with closing(request(origin, bearer, "/v1/auth/session", {})) as snapshot:
        cursor = json.load(snapshot)["cursor"]
    with closing(attach(origin, bearer, cursor)) as first:
        first_replica = verify_handshake(first)
    with closing(attach(origin, bearer, cursor)) as second:
        second_replica = verify_handshake(second)
        if not first_replica or not second_replica or first_replica == second_replica:
            raise RuntimeError("The second attachment did not reach another API replica")
        heartbeats = 0
        while heartbeats < 4:
            line = second.readline(2048)
            if not line or line.startswith((b"event: resync", b"event: access_revoked")):
                raise RuntimeError("The stream closed before four incremental heartbeats")
            if line == b": heartbeat\n":
                heartbeats += 1
    print("Authenticated reconnect reached a different replica and streamed four heartbeats.")


if __name__ == "__main__":
    main()
