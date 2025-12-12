# Dashboard Metrics System Specification

Real-time network monitoring dashboard for packet-wire sender/receiver pairs with transparent mirroring support.

## Architecture

```
┌──────────────┐                    ┌──────────────┐
│  sender01    │────UDP packets────▶│ receiver01   │
│              │   (with timestamp)  │              │
└──────┬───────┘                    └──────┬───────┘
       │                                   │
       │                             [Mirror Device]
       │                                   │
       │                            ┌──────┴───────┐
       │                            │ receiver02   │
       │                            │  (mirror)    │
       │                            └──────┬───────┘
       │                                   │
       │ HTTP POST /api/metrics            │ HTTP POST /api/metrics
       │ (5s interval)                     │ (5s interval)
       │                                   │
       ▼                                   ▼
┌─────────────────────────────────────────────────┐
│            Dashboard (Go + WebSocket)           │
│  - Receives metrics from clients                │
│  - Stores in Redis (6h retention)               │
│  - Broadcasts updates via WebSocket             │
│  - Correlates sender→receiver via source IP     │
└────────────┬────────────────────────────────────┘
             │
             ▼
      ┌─────────────┐         ┌──────────────────┐
      │   Redis     │         │  Browser         │
      │  (state)    │◀────────│  (D3.js network) │
      └─────────────┘         └──────────────────┘
```

## Packet Protocol Enhancement

### Timestamp Format

All UDP packets include a timestamp prefix BEFORE encryption:

```
TIMESTAMP|<unix_nanos>|<original_payload>
```

**Example:**
```
TIMESTAMP|1702345678123456789|{"device":"test","status":"ok"}
```

**Implementation:**
1. Sender prepends timestamp before encryption
2. Receiver decrypts, then extracts timestamp
3. Receiver calculates latency: `time.Now().UnixNano() - extracted_timestamp`

### Protocol Constants

```go
const (
    TimestampPrefix    = "TIMESTAMP|"
    TimestampDelimiter = "|"
)
```

## Metrics Schema

### Sender Heartbeat (POST /api/metrics)

```json
{
  "client_id": "sender01",
  "client_type": "sender",
  "timestamp": "2025-12-12T01:00:00Z",
  
  "source_ip": "192.168.100.10",
  "local_ip": "172.17.0.3",
  "external_ip": "88.66.45.5",
  "destination_ip": "192.168.92.22",
  "destination_port": 9000,
  
  "packets_sent": 1240,
  "bytes_sent": 568000,
  "encryption_enabled": true,
  "last_send_time": "2025-12-12T00:59:58Z"
}
```

### Receiver Heartbeat (POST /api/metrics)

```json
{
  "client_id": "receiver01",
  "client_type": "receiver",
  "timestamp": "2025-12-12T01:00:00Z",
  
  "listen_ip": "192.168.92.22",
  "local_ip": "172.17.0.5",
  "external_ip": "88.66.45.10",
  "listen_port": 9000,
  
  "detected_source_ip": "192.168.100.10",
  
  "packets_received": 1234,
  "bytes_received": 567890,
  "encryption_enabled": true,
  "decode_success_count": 1230,
  "decode_failure_count": 4,
  "avg_latency_ms": 12.5,
  "last_receive_time": "2025-12-12T00:59:59Z"
}
```

## Dashboard API Endpoints

### POST /api/metrics

Accept metrics from sender/receiver clients.

**Request:** JSON body as described above

**Response:**
```json
{
  "status": "ok",
  "client_id": "sender01",
  "received_at": "2025-12-12T01:00:00.123Z"
}
```

**Error Response:**
```json
{
  "status": "error",
  "error": "invalid client_type: must be 'sender' or 'receiver'"
}
```

### GET /api/network

Get current network topology for visualization.

**Response:**
```json
{
  "nodes": [
    {
      "id": "sender01",
      "type": "sender",
      "status": "online",
      "source_ip": "192.168.100.10",
      "destination_ip": "192.168.92.22",
      "packets_sent": 1240,
      "encryption_enabled": true,
      "last_seen": "2025-12-12T01:00:00Z"
    },
    {
      "id": "receiver01",
      "type": "receiver",
      "status": "online",
      "listen_ip": "192.168.92.22",
      "detected_source_ip": "192.168.100.10",
      "packets_received": 1234,
      "avg_latency_ms": 12.5,
      "decode_success_rate": 0.997,
      "encryption_enabled": true,
      "last_seen": "2025-12-12T01:00:00Z"
    }
  ],
  "edges": [
    {
      "from": "sender01",
      "to": "receiver01",
      "packets_sent": 1240,
      "packets_received": 1234,
      "packet_loss_rate": 0.005,
      "avg_latency_ms": 12.5,
      "is_mirror": false
    }
  ],
  "timestamp": "2025-12-12T01:00:05Z"
}
```

### GET /api/metrics/{client_id}

Get historical metrics for a specific client.

**Query Parameters:**
- `from`: Start timestamp (ISO8601)
- `to`: End timestamp (ISO8601)
- `limit`: Max records (default 100)

**Response:**
```json
{
  "client_id": "sender01",
  "metrics": [
    {"timestamp": "...", "packets_sent": 1200, ...},
    {"timestamp": "...", "packets_sent": 1220, ...}
  ]
}
```

### WebSocket /ws

Real-time updates for dashboard frontend.

**Message Types:**

Node update (sent when metrics received):
```json
{
  "type": "node_update",
  "data": {
    "id": "sender01",
    "type": "sender",
    "status": "online",
    "packets_sent": 1240,
    ...
  }
}
```

Node offline:
```json
{
  "type": "node_offline",
  "data": {
    "id": "sender01",
    "last_seen": "2025-12-12T00:59:55Z"
  }
}
```

Edge update:
```json
{
  "type": "edge_update",
  "data": {
    "from": "sender01",
    "to": "receiver01",
    "packets_sent": 1240,
    "packets_received": 1234,
    "avg_latency_ms": 12.5
  }
}
```

## Redis Data Model

### Client Registry

```
Key: client:{client_id}
Type: Hash
TTL: None (managed by cleanup job)

Fields:
  - type: "sender" | "receiver"
  - status: "online" | "offline"
  - last_seen: timestamp
  - config: JSON of IP config
  - latest_metrics: JSON of latest metrics
```

### Metrics History

```
Key: metrics:{client_id}
Type: Sorted Set
Score: Unix timestamp (milliseconds)
Value: JSON metrics payload
TTL: 6 hours (configurable)
```

### Connection Edges

```
Key: edges
Type: Hash
Field: "{sender_id}:{receiver_id}"
Value: JSON edge data
```

## Connection Correlation Logic

```go
func correlateConnections(senders, receivers []Client) []Edge {
    var edges []Edge
    
    for _, sender := range senders {
        for _, receiver := range receivers {
            // Match if receiver detected packets from this sender
            if receiver.DetectedSourceIP == sender.SourceIP {
                edges = append(edges, Edge{
                    From:            sender.ClientID,
                    To:              receiver.ClientID,
                    PacketsSent:     sender.PacketsSent,
                    PacketsReceived: receiver.PacketsReceived,
                    AvgLatencyMs:    receiver.AvgLatencyMs,
                    IsMirror:        false, // Set in post-processing
                })
            }
        }
    }
    
    // Detect mirrors: multiple receivers from same sender
    detectMirrors(edges)
    
    return edges
}

func detectMirrors(edges []Edge) {
    // Group by sender
    bySender := make(map[string][]Edge)
    for _, e := range edges {
        bySender[e.From] = append(bySender[e.From], e)
    }
    
    // If sender has multiple receivers, they might be mirrors
    for _, receiverEdges := range bySender {
        if len(receiverEdges) > 1 {
            // Check if packet counts are similar (within 5%)
            for i := 1; i < len(receiverEdges); i++ {
                ratio := float64(receiverEdges[i].PacketsReceived) / 
                         float64(receiverEdges[0].PacketsReceived)
                if ratio > 0.95 && ratio < 1.05 {
                    receiverEdges[i].IsMirror = true
                }
            }
        }
    }
}
```

## Configuration

### Dashboard Environment Variables

```bash
# Redis connection
REDIS_URL=redis:6379
REDIS_PASSWORD=                    # Optional

# Server
DASHBOARD_PORT=8080
DASHBOARD_HOST=0.0.0.0

# Metrics retention
METRICS_RETENTION_HOURS=6

# Client timeout (seconds without heartbeat = offline)
CLIENT_TIMEOUT_SECONDS=15
```

### Sender Configuration

```json
{
  "client_id": "sender01",
  "dashboard_url": "http://dashboard:8080/api/metrics",
  "heartbeat_interval_seconds": 5,
  
  "source_ip": "192.168.100.10",
  "local_ip": "172.17.0.3",
  "external_ip": "",
  
  "target": "192.168.92.22:9000",
  "passphrase": "your-encryption-key"
}
```

### Receiver Configuration

```json
{
  "client_id": "receiver01",
  "dashboard_url": "http://dashboard:8080/api/metrics",
  "heartbeat_interval_seconds": 5,
  
  "listen_ip": "192.168.92.22",
  "local_ip": "172.17.0.5",
  "external_ip": "",
  
  "listen_addr": ":9000",
  "encryption_passphrase": "your-encryption-key"
}
```

## Docker Compose

```yaml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    command: redis-server --appendonly yes
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 3
    networks:
      - packet-wire

  dashboard:
    build: ./dashboard
    ports:
      - "8080:8080"
    environment:
      - REDIS_URL=redis:6379
      - DASHBOARD_PORT=8080
      - DASHBOARD_HOST=0.0.0.0
      - METRICS_RETENTION_HOURS=6
      - CLIENT_TIMEOUT_SECONDS=15
    depends_on:
      redis:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-q", "-O-", "http://localhost:8080/health"]
      interval: 10s
      timeout: 3s
      retries: 3
    networks:
      - packet-wire

volumes:
  redis-data:

networks:
  packet-wire:
    driver: bridge
```

## Frontend Visualization

### D3.js Force-Directed Graph

```javascript
// Node representation
const nodes = [
  { id: "sender01", type: "sender", status: "online", x: 0, y: 0 },
  { id: "receiver01", type: "receiver", status: "online", x: 0, y: 0 },
];

// Edge representation
const links = [
  { source: "sender01", target: "receiver01", value: 1234, latency: 12.5 }
];

// Force simulation
const simulation = d3.forceSimulation(nodes)
  .force("link", d3.forceLink(links).id(d => d.id).distance(200))
  .force("charge", d3.forceManyBody().strength(-300))
  .force("center", d3.forceCenter(width / 2, height / 2));
```

### Node States & Colors

| Status | Color | Description |
|--------|-------|-------------|
| online | #2ecc71 (green) | Active, receiving heartbeats |
| offline | #e74c3c (red) | No heartbeat for >15s |
| ready | #f1c40f (yellow) | Online but no traffic |
| degraded | #e67e22 (orange) | High latency or packet loss |

### Edge Visual Properties

- **Thickness:** Proportional to packets per second
- **Color:** Based on latency (green < 50ms, yellow < 200ms, red > 200ms)
- **Animation:** Dashed line moving in flow direction
- **Label:** "1.2K pps • 12ms • 🔒"

### Dark Theme

```css
:root {
  --bg-primary: #1a1a2e;
  --bg-secondary: #16213e;
  --bg-tertiary: #0f3460;
  --text-primary: #e6e6e6;
  --text-secondary: #a0a0a0;
  --accent: #e94560;
  --success: #2ecc71;
  --warning: #f1c40f;
  --error: #e74c3c;
}
```

## Error Handling

### Per Engineering Standards

1. **Retry with exponential backoff** for dashboard HTTP POSTs:
   - Attempt 1: immediate
   - Attempt 2: 5s + jitter
   - Attempt 3: 10s + jitter

2. **Graceful shutdown**: Coordinated via context.Context

3. **No blocking while holding locks**: Redis operations are non-blocking

4. **Structured logging**: Use Go log package with levels

5. **Exceptions at boundaries only**: Errors propagate up, main() handles exit codes

## Implementation Order

1. ✅ Write specification (this document)
2. Update docker-compose.yml with Redis
3. Implement packet timestamp protocol in sender
4. Implement packet timestamp + source detection in receiver
5. Implement dashboard backend (Redis + API)
6. Implement dashboard frontend (D3 network graph)
7. Test end-to-end

---

*Specification Version: 1.0*
*Last Updated: 2025-12-12*
