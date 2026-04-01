#!/usr/bin/env python3
"""Interactive CLI for the dkvStore Raft HTTP API."""

from __future__ import annotations

import json
import os
import random
import sys
from typing import Any, Optional

import requests

PORT_RANGE = list(range(8000, 8005))
BASE_HTTP_PORT = 8000
DEFAULT_TIMEOUT = 2.0


# --- terminal styling (no extra deps; respects NO_COLOR) ---

def _use_color() -> bool:
    return sys.stdout.isatty() and not os.environ.get("NO_COLOR")


class _ColorOn:
    ok = "\033[32m"
    err = "\033[31m"
    info = "\033[36m"
    dim = "\033[2m"
    bold = "\033[1m"
    reset = "\033[0m"


class _ColorOff:
    ok = err = info = dim = bold = reset = ""


_C = _ColorOn() if _use_color() else _ColorOff()


def _style(text: str, *parts: str) -> str:
    return "".join(parts) + text + _C.reset


def print_ok(msg: str) -> None:
    print(_style(msg, _C.ok, _C.bold))


def print_err(msg: str) -> None:
    print(_style(msg, _C.err, _C.bold), file=sys.stderr)


def print_info(msg: str) -> None:
    print(_style(msg, _C.info))


def print_dim(msg: str) -> None:
    print(_style(msg, _C.dim))


def print_json(data: Any, *, title: Optional[str] = None) -> None:
    """Pretty-print JSON with an optional heading."""
    body = json.dumps(data, indent=2, sort_keys=True)
    if title:
        print()
        print(_style(title, _C.bold))
        print(_C.dim + "─" * min(len(title) + 4, 72) + _C.reset)
    print(body)


def print_banner() -> None:
    print()
    print(_style("  dkv Raft client", _C.bold))
    print_dim("  SET / DELETE  ·  STORE / STATE  ·  FAULT  ·  Ctrl+C to exit")
    print()


# --- HTTP helpers (single place for URL + errors) ---


def _url(port: int, path: str) -> str:
    return f"http://localhost:{port}{path}"


def http_get_json(port: int, path: str, *, timeout: float = DEFAULT_TIMEOUT) -> Optional[Any]:
    try:
        r = requests.get(_url(port, path), timeout=timeout)
        if r.status_code != 200:
            return None
        return r.json()
    except (requests.RequestException, ValueError):
        return None


def http_post_json(
    port: int, path: str, payload: dict, *, timeout: float = DEFAULT_TIMEOUT
) -> Optional[dict]:
    try:
        r = requests.post(_url(port, path), json=payload, timeout=timeout)
        return r.json()
    except (requests.RequestException, ValueError):
        return None


def http_post_expect_empty(
    port: int,
    path: str,
    payload: dict,
    *,
    expected: int = 204,
    timeout: float = DEFAULT_TIMEOUT,
) -> bool:
    try:
        r = requests.post(_url(port, path), json=payload, timeout=timeout)
        return r.status_code == expected
    except requests.RequestException:
        return False


def http_delete_expect_empty(
    port: int,
    path: str,
    *,
    params: Optional[dict] = None,
    expected: int = 204,
    timeout: float = DEFAULT_TIMEOUT,
) -> bool:
    try:
        r = requests.delete(_url(port, path), params=params, timeout=timeout)
        return r.status_code == expected
    except requests.RequestException:
        return False


# --- Raft command routing ---


def post_command(port: int, payload: dict) -> Optional[dict]:
    return http_post_json(port, "/command", payload)


def find_and_send(payload: dict) -> None:
    ports = PORT_RANGE.copy()
    random.shuffle(ports)

    for port in ports:
        resp = post_command(port, payload)
        if resp is None:
            continue

        if resp.get("success"):
            print_ok(f"Success (via :{port})")
            print_json(resp, title="Response")
            return

        leader = resp.get("leader_id")
        if leader:
            try:
                base_num = int(str(leader).split("_")[-1])
            except ValueError:
                print_err(f"Could not parse leader_id: {leader!r}")
                print_json(resp, title="Raw response")
                return
            http_port = BASE_HTTP_PORT + base_num
            print_info(f"Not leader — retrying leader :{http_port} ({leader})")
            leader_resp = post_command(http_port, payload)
            if leader_resp is not None:
                if leader_resp.get("success"):
                    print_ok(f"Success (leader :{http_port})")
                else:
                    print_err(f"Leader :{http_port} returned failure")
                print_json(leader_resp, title="Response")
            else:
                print_err(f"No response from leader :{http_port}")
            return

        print_err(f"Unexpected response from :{port}")
        print_json(resp, title="Response")
        return

    print_err("No reachable node returned a valid response (tried all ports).")


# --- Optional local reads (STORE / STATE) ---


def get_store(port: int) -> Optional[Any]:
    return http_get_json(port, "/store")


def get_state(port: int) -> Optional[Any]:
    return http_get_json(port, "/state")


# --- Fault API ---


def set_fault(
    port: int,
    *,
    latency_ms: int = 0,
    drop_rate: float = 0.0,
    partitioned: bool = False,
    target: Optional[str] = None,
) -> bool:
    payload: dict[str, Any] = {
        "latency_ms": latency_ms,
        "drop_rate": drop_rate,
        "partitioned": partitioned,
    }
    if target:
        payload["target"] = target
    return http_post_expect_empty(port, "/fault", payload)


def clear_fault(port: int, target: Optional[str] = None) -> bool:
    params = {"target": target} if target else None
    return http_delete_expect_empty(port, "/fault", params=params)


def get_fault(port: int) -> Optional[dict]:
    data = http_get_json(port, "/fault")
    return data if isinstance(data, dict) else None


# --- Parsing ---


def parse_port_arg(parts: list[str], index: int, default: int) -> Optional[int]:
    if index >= len(parts):
        return default
    try:
        return int(parts[index].lstrip(":"))
    except ValueError:
        return None


def parse_input(user_input: str) -> Optional[dict]:
    parts = user_input.strip().split()
    if not parts:
        return None

    op = parts[0].upper()

    if op == "SET":
        if len(parts) != 3:
            print_err("Usage: SET <key> <value>")
            return None
        return {"op": "set", "key": parts[1], "value": parts[2]}

    if op == "DELETE":
        if len(parts) != 2:
            print_err("Usage: DELETE <key>")
            return None
        return {"op": "delete", "key": parts[1]}

    if op == "STATE":
        port = parse_port_arg(parts, 1, BASE_HTTP_PORT)
        if port is None:
            print_err("Usage: STATE [port]  (e.g. STATE 8002)")
            return None
        state = get_state(port)
        if state is None:
            print_err(f"Could not GET /state @ :{port}")
        else:
            print_json(state, title=f"Raft state @ :{port}")
        return None

    if op == "STORE":
        port = parse_port_arg(parts, 1, BASE_HTTP_PORT)
        if port is None:
            print_err("Usage: STORE [port]  (e.g. STORE 8002)")
            return None
        store = get_store(port)
        if store is None:
            print_err(f"Could not GET /store @ :{port}")
        else:
            print_json(store, title=f"Store snapshot @ :{port}")
        return None

    if op == "FAULT":
        return _parse_fault(parts)

    print_err("Unknown command. Use SET, DELETE, STORE, STATE, or FAULT.")
    return None


def _fault_show(port: int) -> None:
    snap = get_fault(port)
    if snap is None:
        print_err(f"Could not GET /fault @ :{port}")
    else:
        print_json(snap, title=f"Fault config @ :{port}")


def _parse_fault(parts: list[str]) -> Optional[dict]:
    if len(parts) == 1:
        _fault_show(BASE_HTTP_PORT)
        return None

    sub = parts[1].upper()

    if sub == "GET":
        port = parse_port_arg(parts, 2, BASE_HTTP_PORT)
        if port is None:
            print_err("Usage: FAULT GET [port]")
            return None
        _fault_show(port)
        return None

    if sub == "CLEAR":
        port = parse_port_arg(parts, 2, BASE_HTTP_PORT)
        if port is None:
            print_err("Usage: FAULT CLEAR [port] [target]")
            return None
        target = parts[3] if len(parts) >= 4 else None
        ok = clear_fault(port, target)
        if ok:
            print_ok("Cleared.")
        else:
            print_err(f"CLEAR failed @ :{port}")
        return None

    if sub == "SET":
        if len(parts) < 6:
            print_err(
                "Usage: FAULT SET <port> <latency_ms> <drop_rate> <0|1> [target]\n"
                "  (partitioned: 1 = true; target = optional gRPC dial target)"
            )
            return None
        try:
            port = int(parts[2].lstrip(":"))
            latency_ms = int(parts[3])
            drop_rate = float(parts[4])
        except ValueError:
            print_err("Usage: FAULT SET <port> <latency_ms> <drop_rate> <0|1> [target]")
            return None
        partitioned = parts[5] in ("1", "true", "True")
        target = parts[6] if len(parts) > 6 else None
        ok = set_fault(
            port,
            latency_ms=latency_ms,
            drop_rate=drop_rate,
            partitioned=partitioned,
            target=target,
        )
        if ok:
            print_ok("Fault config applied.")
        else:
            print_err(f"FAULT SET failed @ :{port}")
        return None

    print_err("Unknown FAULT subcommand. Try FAULT, FAULT GET, FAULT CLEAR, FAULT SET.")
    return None


def main() -> None:
    print_banner()
    while True:
        try:
            user_input = input(_style("dkv> ", _C.bold))
            payload = parse_input(user_input)
            if payload:
                find_and_send(payload)
        except KeyboardInterrupt:
            print()
            print_dim("Exiting.")
            break
        except Exception as e:
            print_err(f"Error: {e}")


if __name__ == "__main__":
    main()
