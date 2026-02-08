#!/usr/bin/env bash

set -e

NODES=5
BASE_PORT=9000
CMD="go run cmd/dkvStore/main.go"

pids=()

echo "Starting $NODES-node Raft cluster..."

for ((i=0; i<NODES; i++)); do
  ID=$((i + 1))
  PORT=$((BASE_PORT + i))

  PEERS=()
  for ((j=0; j<NODES; j++)); do
    if [[ $i -ne $j ]]; then
      PEERS+=(":$((BASE_PORT + j))")
    fi
  done

  PEER_LIST=$(IFS=,; echo "${PEERS[*]}")

  echo "→ Starting node $ID on :$PORT (peers: $PEER_LIST)"

  $CMD \
    --id=$ID \
    --addr=:$PORT \
    --peers=$PEER_LIST \
    > logs/node-$ID.log 2>&1 &

  pids+=($!)
done

echo
echo "Cluster started."
echo "Logs: node-<id>.log"
echo "To stop all nodes: ./stop-cluster.sh"
