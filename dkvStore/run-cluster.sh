#!/usr/bin/env bash

set -e

NODES=5
GRPC_BASE_PORT=9000
HTTP_BASE_PORT=8000
# CMD="go run cmd/dkvStore/main.go"
CMD="./bin/dkvStore"

pids=()

echo "Starting $NODES-node Raft cluster..."

for ((i=0; i<NODES; i++)); do
  ID=$((i + 1))
  GRPC_PORT=$((GRPC_BASE_PORT + i))
  HTTP_PORT=$((HTTP_BASE_PORT + i))

  PEERS=()
  for ((j=0; j<NODES; j++)); do
    if [[ $i -ne $j ]]; then
      PEERS+=(":$((GRPC_BASE_PORT + j))")
    fi
  done

  PEER_LIST=$(IFS=,; echo "${PEERS[*]}")

  echo "→ Starting node $ID"
  echo "  gRPC: :$GRPC_PORT | HTTP: :$HTTP_PORT"
  echo "  Peers: $PEER_LIST"

  $CMD \
    --id=$ID \
    --grpc-addr=:$GRPC_PORT \
    --http-addr=:$HTTP_PORT \
    --peers=$PEER_LIST \
    > logs/node-$ID.log 2>&1 &

  pids+=($!)
done

echo
echo "✓ Cluster started."
echo
echo "gRPC ports: $GRPC_BASE_PORT-$((GRPC_BASE_PORT + NODES - 1))"
echo "HTTP ports: $HTTP_BASE_PORT-$((HTTP_BASE_PORT + NODES - 1))"
echo
echo "Example HTTP request:"
echo "  curl -X POST http://localhost:$HTTP_BASE_PORT/command -d '{\"msg\":\"hello\"}'"
echo
echo "Logs: logs/node-<id>.log"
echo "To stop: kill ${pids[*]}"
