import json
import random
import requests

PORT_RANGE = list(range(8000, 8005))


def grpc_to_http_port(grpc_port: int) -> int:
    """Cluster uses HTTP on grpc_port - 1000 (e.g. :9004 gRPC → :8004 HTTP)."""
    return grpc_port - 1000


def send_command(port: int, payload: dict):
    url = f"http://localhost:{port}/command"
    try:
        response = requests.post(url, json=payload, timeout=2)
        print(response.text)
        return response.json()
    except requests.RequestException:
        return None


def find_and_send(payload: dict):
    ports = PORT_RANGE.copy()
    random.shuffle(ports)

    for port in ports:
        resp = send_command(port, payload)

        if not resp:
            continue

        # Success
        if resp.get("success"):
            print(f"✓ Success from port {port}: {resp}")
            return

        # Redirect to leader (leader_id is gRPC address like ":9004"; HTTP is offset by -1000)
        leader = resp.get("leader_id")
        if leader:
            grpc_port = int(leader.lstrip(":"))
            http_port = grpc_to_http_port(grpc_port)

            leader_resp = send_command(http_port, payload)
            print(f"→ Redirected to leader (gRPC :{grpc_port}, HTTP :{http_port})")
            print(f"✓ Leader response: {leader_resp}")
            return

    print("✗ No reachable leader found.")
    
def get_store(port: int):
    url = f"http://localhost:{port}/store"
    try:
        response = requests.get(url, timeout=2)
        return response.json()
    except requests.RequestException:
        return None


def parse_input(user_input: str):
    parts = user_input.strip().split()

    if len(parts) == 0:
        return None

    op = parts[0].upper()

    if op == "SET":
        if len(parts) != 3:
            print("Usage: SET <key> <value>")
            return None

        return {
            "op": "set",
            "key": parts[1],
            "value": parts[2],
        }

    elif op == "DELETE":
        if len(parts) != 2:
            print("Usage: DELETE <key>")
            return None

        return {
            "op": "delete",
            "key": parts[1],
        }
    
    elif op == "STORE":
        port = 8000
        if len(parts) == 2:
            try:
                port = int(parts[1].lstrip(":"))
            except ValueError:
                print("Usage: STORE [port]  e.g. STORE 8002")
                return None
        store = get_store(port)
        if store is None:
            print(f"✗ Could not reach :{port}")
        else:
            print(f"Store @ :{port}:")
            print(json.dumps(store, indent=2))
        return None  # handled inline, no command to send

    else:
        print("Unknown command. Use SET or DELETE.")
        return None


def main():
    print("Cluster CLI Client")
    print("Type key:value (Ctrl+C to exit)\n")

    while True:
        try:
            user_input = input("> ")
            payload = parse_input(user_input)

            if payload:
                find_and_send(payload)

        except KeyboardInterrupt:
            print("\nExiting...")
            break
        except Exception as e:
            print("Error:", e)


if __name__ == "__main__":
    main()
