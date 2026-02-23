import json
import random
import requests

PORT_RANGE = list(range(8000, 8005))


def leader_id_to_port(leader_id: int) -> int:
    return 7999 + leader_id  # 1->8000, 5->8004


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

        # Redirect to leader
        leader_id = resp.get("leader_id")
        if leader_id:
            leader_id = int(leader_id)
            leader_port = leader_id_to_port(leader_id)
            leader_resp = send_command(leader_port, payload)
            print(f"→ Redirected to leader {leader_id} (port {leader_port})")
            print(f"✓ Leader response: {leader_resp}")
            return

    print("✗ No reachable leader found.")


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
