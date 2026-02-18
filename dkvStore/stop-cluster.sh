#!/usr/bin/env bash

GRPC_BASE_PORT=9000
HTTP_BASE_PORT=8000
NODES=5

echo "Stopping Raft cluster..."
echo "  gRPC ports: $GRPC_BASE_PORT-$((GRPC_BASE_PORT + NODES - 1))"
echo "  HTTP ports: $HTTP_BASE_PORT-$((HTTP_BASE_PORT + NODES - 1))"
echo

# Kill both gRPC and HTTP ports
for ((i=0; i<NODES; i++)); do
  GRPC_PORT=$((GRPC_BASE_PORT + i))
  HTTP_PORT=$((HTTP_BASE_PORT + i))
  
  # Try gRPC port
  GRPC_PIDS=$(lsof -t -i tcp:$GRPC_PORT 2>/dev/null)
  if [[ -n "$GRPC_PIDS" ]]; then
    echo "→ Killing process(es) on gRPC port $GRPC_PORT: $GRPC_PIDS"
    kill $GRPC_PIDS 2>/dev/null || true
  fi
  
  # Try HTTP port
  HTTP_PIDS=$(lsof -t -i tcp:$HTTP_PORT 2>/dev/null)
  if [[ -n "$HTTP_PIDS" ]]; then
    echo "→ Killing process(es) on HTTP port $HTTP_PORT: $HTTP_PIDS"
    kill $HTTP_PIDS 2>/dev/null || true
  fi
done

# Force kill anything still alive
sleep 1

echo
echo "Checking for remaining processes..."

for ((i=0; i<NODES; i++)); do
  GRPC_PORT=$((GRPC_BASE_PORT + i))
  HTTP_PORT=$((HTTP_BASE_PORT + i))
  
  GRPC_PIDS=$(lsof -t -i tcp:$GRPC_PORT 2>/dev/null)
  if [[ -n "$GRPC_PIDS" ]]; then
    echo "→ Force killing gRPC port $GRPC_PORT: $GRPC_PIDS"
    kill -9 $GRPC_PIDS 2>/dev/null || true
  fi
  
  HTTP_PIDS=$(lsof -t -i tcp:$HTTP_PORT 2>/dev/null)
  if [[ -n "$HTTP_PIDS" ]]; then
    echo "→ Force killing HTTP port $HTTP_PORT: $HTTP_PIDS"
    kill -9 $HTTP_PIDS 2>/dev/null || true
  fi
done

echo
echo "✓ Cluster stopped."