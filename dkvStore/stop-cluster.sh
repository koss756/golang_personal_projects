#!/usr/bin/env bash

BASE_PORT=9000
NODES=5

echo "Stopping Raft cluster (ports $BASE_PORT-$((BASE_PORT + NODES - 1)))..."

for ((i=0; i<NODES; i++)); do
  PORT=$((BASE_PORT + i))
  PIDS=$(lsof -t -i tcp:$PORT)

  if [[ -n "$PIDS" ]]; then
    echo "→ Killing process(es) on port $PORT: $PIDS"
    kill $PIDS 2>/dev/null || true
  else
    echo "→ No process on port $PORT"
  fi
done

# Force kill anything still alive
sleep 1

for ((i=0; i<NODES; i++)); do
  PORT=$((BASE_PORT + i))
  PIDS=$(lsof -t -i tcp:$PORT)

  if [[ -n "$PIDS" ]]; then
    echo "→ Force killing process(es) on port $PORT: $PIDS"
    kill -9 $PIDS 2>/dev/null || true
  fi
done

echo "Done."