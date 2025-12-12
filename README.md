# Packet Wire

A comprehensive UDP testing and monitoring toolkit for network traffic analysis, visualization, and simulation.

## Overview

Packet Wire consists of three containerized applications that work together to capture, send, and visualize UDP traffic flows in real-time. Designed for testing UDP-based systems, IoT device simulation, and network traffic analysis.

## Components

### 🎯 [UDP Listener](./udp-listener/README.md)

High-performance UDP packet capture service that receives and logs all incoming packets to CSV files with real-time metrics reporting.

**Key Features:**
- CSV logging with timestamps, source IP, and payload
- Optional reply/ACK mode
- AES-256-GCM encryption support
- File reception mode
- Metrics reporting to dashboard API
- Docker-ready with configurable ports

**Quick Start:**
```bash
docker build -f udp-listener/Dockerfile -t udp-listener:latest udp-listener
docker run -p 9000:9000/udp -v ./captures:/captures udp-listener:latest
```

[Full Documentation →](./udp-listener/README.md)

---

### 📊 [Dashboard](./dashboard/README.md)

Real-time traffic flow visualization dashboard with interactive Sankey diagrams for analyzing UDP packet routing and latency.

**Key Features:**
- Interactive Sankey flow diagrams
- Real-time metrics (packets, latency, success rates)
- Multi-source aggregation
- Time-based filtering (5m to 24h)
- WebSocket updates
- Custom flow visualization

**Quick Start:**
```bash
docker build -f dashboard/Dockerfile -t dashboard:latest dashboard
docker run -p 8080:8080 dashboard:latest
```

[Full Documentation →](./dashboard/README.md)

---

### 📤 [UDP Sender](./udp-sender/README.md)

CLI tool for sending UDP messages and chunked file payloads to simulate IoT devices and test UDP endpoints.

**Key Features:**
- Send text messages or binary files
- Chunked file transmission
- AES-256-GCM encryption
- Configurable chunk size and delays
- Timestamp injection for latency testing
- Metrics reporting to dashboard API

**Quick Start:**
```bash
docker build -f udp-sender/Dockerfile -t udp-sender:latest udp-sender
docker run --rm udp-sender --target 192.168.1.100:9000 --message "TEST"
```

[Full Documentation →](./udp-sender/README.md)

---

## Architecture

```
┌─────────────┐                  ┌──────────────┐                 ┌────────────┐
│ UDP Sender  │  ────UDP────>    │ UDP Listener │  ───metrics───> │ Dashboard  │
│  (Client)   │                  │   (Server)   │                 │   (Web)    │
└─────────────┘                  └──────────────┘                 └────────────┘
                                         │
                                         ▼
                                   CSV Captures
                                   /captures/*.csv
```

## Quick Start (All Services)

### Using Docker Compose

```yaml
services:
  dashboard:
    image: ghcr.io/<your-org>/packet-wire-dashboard:latest
    ports:
      - "8080:8080"
    networks:
      - packet-wire

  udp-listener:
    image: ghcr.io/<your-org>/packet-wire-udp-listener:latest
    ports:
      - "9000:9000/udp"
    environment:
      - CLIENT_ID=listener-01
      - DASHBOARD_URL=http://dashboard:8080/api/metrics
    volumes:
      - ./captures:/captures
    networks:
      - packet-wire

  udp-sender:
    image: ghcr.io/<your-org>/packet-wire-udp-sender:latest
    command: >
      --target udp-listener:9000
      --message "Hello from sender"
      --interval 5s
    networks:
      - packet-wire

networks:
  packet-wire:
    driver: bridge
```

### Manual Build 

```bash
# Build all images
docker build -f udp-listener/Dockerfile -t udp-listener:latest udp-listener
docker build -f udp-sender/Dockerfile -t udp-sender:latest udp-sender
docker build -f dashboard/Dockerfile -t dashboard:latest dashboard

# Run dashboard
docker run -d -p 8080:8080 --name dashboard dashboard:latest

# Run listener with metrics
docker run -d -p 9000:9000/udp \
  -e CLIENT_ID=listener-01 \
  -e DASHBOARD_URL=http://host.docker.internal:8080/api/metrics \
  -v ./captures:/captures \
  --name udp-listener \
  udp-listener:latest

# Send test packets
docker run --rm udp-sender \
  --target host.docker.internal:9000 \
  --message "PLTESTMESSAGE"
```

## Container Images

Images are automatically built and published to GitHub Container Registry on commits to main:

- `ghcr.io/<your-org>/packet-wire-udp-listener:latest`
- `ghcr.io/<your-org>/packet-wire-udp-sender:latest`
- `ghcr.io/<your-org>/packet-wire-dashboard:latest`

## Development

### Prerequisites
- Go 1.22+
- Docker & Docker Compose
- Git

### Build from Source

Each component can be built independently:

```bash
# UDP Listener
cd udp-listener && go build -o udp-listener .

# UDP Sender
cd udp-sender && go build -o udp-sender .

# Dashboard
cd dashboard && go build -o dashboard-server server.go
```

## Documentation

- [Engineering Standards](./_docs/engineering-standards.md)
- [API Reference](./_docs/api-reference.md)
- [Dashboard Metrics Spec](./_docs/dashboard-metrics-spec.md)
- [Implementation Guide](./_docs/implementation-guide.md)

## License

[Add your license here]

