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
    if ":" not in user_input:
        print("Invalid format. Use key:value")
        return None

    key, value = user_input.split(":", 1)
    return {key.strip(): value.strip()}


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
