#!/usr/bin/env bash

set -e

NODES=2
GRPC_BASE_PORT=9000
HTTP_BASE_PORT=8000
CMD="go run cmd/dkvStore/main.go"

mkdir -p logs
pids=()

echo "Starting $NODES-node Raft cluster..."

for ((i=0; i<NODES; i++)); do
  GRPC_PORT=$((GRPC_BASE_PORT + i))
  HTTP_PORT=$((HTTP_BASE_PORT + i))

  ID=":$GRPC_PORT"

  PEERS=()
  for ((j=0; j<NODES; j++)); do
    if [[ $i -ne $j ]]; then
      PEERS+=(":$((GRPC_BASE_PORT + j))")
    fi
  done

  PEER_LIST=$(IFS=,; echo "${PEERS[*]}")

  echo "→ Starting node $ID"
  echo "  HTTP: :$HTTP_PORT"
  echo "  Peers: $PEER_LIST"

  $CMD \
    --id="$ID" \
    --grpc-addr="$ID" \
    --http-addr=":$HTTP_PORT" \
    --peers="$PEER_LIST" \
    > logs/node-$GRPC_PORT.log 2>&1 &

  pids+=($!)
done

echo
echo "✓ Cluster started."
echo "To stop: kill ${pids[*]}"