# Docker Load Balancer

A learning project for building a load balancer in Go that discovers and distributes traffic across Docker containers.

## What This Does

- Discovers backend containers dynamically using the Docker SDK
- Load balances HTTP requests using round-robin algorithm
- Runs entirely in Docker with Docker Compose

## Quick Start

```bash
# Build images
docker compose build

# Start all services
docker compose up

# Test it
curl http://localhost:8000
```

The proxy listens on port 8000 and distributes requests to backend containers.

## Architecture

```
Client → Proxy → Backend Containers (round-robin)
```

## Future Evolution

This project will expand to explore:
- Real-time container discovery using Docker Events API
- Health checks for backend containers
- Alternative load balancing strategies (weighted, least connections, IP hash)
- Graceful handling of backend failures
- Alternative service discovery (Consul, etcd, Kubernetes)