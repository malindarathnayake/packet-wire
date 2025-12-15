<p align="center">
  <img src="../Logo/svglogo.svg" alt="Packet Wire" width="400">
</p>

# Dashboard

Web UI for visualizing UDP traffic flows using Sankey diagrams.

## Features

- Sankey flow diagrams (Customer → Device → Target)
- Packet counts and success rates
- Latency tracking per flow
- Time-based filtering (5m to 24h)
- Flow type filtering
- Status filtering (Success, Timeouts, Errors)
- Minimum packet threshold filtering
- Auto-refresh every 30 seconds

🎨 **Professional UI**
- Modern gradient design with responsive layout
- Interactive tooltips showing detailed flow information
- Status indicators and real-time update timestamps
- Mobile-friendly responsive design
- Expandable system log panel for debugging

🔧 **Configurable Target Groups**
- Customize target IP ranges and labels via JSON config
- Define color schemes for different backend systems
- Match traffic by IP patterns or status codes
- No code changes needed - just edit `dashboard-config.json`

## Quick Start

### Prerequisites
- Go 1.22+
- Access to your InfluxDB instance with UDP Switchboard metrics
- UDP Switchboard running and collecting metrics

### 1. Environment Setup

Create a `.env` file in the dashboard directory (or copy from `env.example`):

```bash
cp env.example .env
# Edit .env with your settings
```

Example `.env` contents:

```bash
# InfluxDB Configuration
INFLUX_URL=http://your-influxdb:8086
INFLUX_TOKEN=your-influxdb-token
INFLUX_ORG=your-org
INFLUX_BUCKET=your-bucket

# Optional: Skip TLS certificate verification (for internal PKI/self-signed certs)
# WARNING: Only use in trusted environments (dev/internal networks)
INFLUX_SKIP_TLS_VERIFY=false

# Dashboard Configuration  
DASHBOARD_PORT=8080
```

### 2. Run the Dashboard

```bash
# Install dependencies
go mod tidy

# Start the dashboard server
go run server.go
```

The dashboard will perform startup checks and display:
```
=== UDP Switchboard Dashboard Starting ===
Step 1: Validating configuration...
✓ Configuration valid
Step 2: Initializing InfluxDB client...
Step 3: Testing InfluxDB connection to http://localhost:8086...
✓ InfluxDB connection successful
Step 4: Verifying query access to bucket "udp-metrics"...
✓ InfluxDB query access verified
Step 5: Setting up HTTP routes...
✓ HTTP routes configured
=== Startup Complete ===

Dashboard URL: http://localhost:8080
API endpoint: http://localhost:8080/api/traffic-flows
InfluxDB: http://localhost:8086 (org: myorg, bucket: udp-metrics)

Dashboard is ready to serve requests.
```

The dashboard will be available at: **http://localhost:8080**

### 3. Docker Deployment (Recommended)

#### Option A: Using Docker Compose (Easiest)
```bash
# Build and run with docker-compose
docker-compose up -d
```

#### Option B: Manual Docker Commands
```bash
# Build the Docker image
docker build -t udp-switchboard-dashboard .

# Run the container with environment variables
docker run -d \
  --name udp-switchboard-dashboard \
  -p 8080:8080 \
  -e INFLUX_URL=http://your-influxdb:8086 \
  -e INFLUX_TOKEN=your-influxdb-token \
  -e INFLUX_ORG=your-org \
  -e INFLUX_BUCKET=your-bucket \
  -e DASHBOARD_PORT=8080 \
  udp-switchboard-dashboard

# Or run with environment file
docker run -d \
  --name udp-switchboard-dashboard \
  -p 8080:8080 \
  --env-file .env \
  udp-switchboard-dashboard
```

#### Option C: Using an existing InfluxDB container
```bash
# Create a network for containers to communicate
docker network create dashboard-network

# Run InfluxDB (if not already running)
docker run -d \
  --name influxdb \
  --network dashboard-network \
  -p 8086:8086 \
  -v influxdb-data:/var/lib/influxdb2 \
  influxdb:2.7

# Build and run dashboard
docker build -t udp-switchboard-dashboard .

docker run -d \
  --name udp-switchboard-dashboard \
  --network dashboard-network \
  -p 8080:8080 \
  -e INFLUX_URL=http://influxdb:8086 \
  -e INFLUX_TOKEN=your-token \
  -e INFLUX_ORG=your-org \
  -e INFLUX_BUCKET=your-bucket \
  udp-switchboard-dashboard
```

## Flow Visualization Types

### Customer → Device → Target (Default)
Shows the complete traffic flow from customers through their device types to backend targets. Perfect for understanding customer migration patterns.

**Example Flow:**
```
soltrack → calamplmd → 207.189.178.80 (Nginx Migrated)
newgate → calampnsewcan → 209.34.233.121 (ATL Legacy)
```

### Customer → Target  
Direct view of which customers are hitting which backend systems. Ideal for migration planning.

### Device → Target
Shows device type distribution across backend systems. Helpful for understanding device compatibility.

## Customizing Target Groups

Edit `dashboard-config.json` to customize target labels, colors, and IP matching patterns:

```json
{
  "targetGroups": [
    {
      "name": "Relay",
      "ipPatterns": ["209.34.233.*"],
      "color": "#3498db",
      "description": "Legacy backend servers"
    },
    {
      "name": "Mirrored",
      "ipPatterns": ["192.168.94.241"],
      "color": "#2ecc71",
      "description": "New backend servers"
    },
    {
      "name": "Errors/Timeouts",
      "statusMatch": ["error", "timeout"],
      "color": "#e74c3c",
      "description": "Failed or timed out connections"
    },
  ],
  "defaultTargetColor": "#f39c12",
  "showUnmatchedIPs": true,
  "layerColors": {
    "customer": "#9b59b6",
    "device": "#34495e"
  }
}
```

**Configuration Options:**
- `name`: Display name in legend
- `ipPatterns`: Array of IP patterns with wildcards (e.g., `"10.0.*"`, `"192.168.1.100"`)
- `statusMatch`: Match by status codes (`"error"`, `"timeout"`, `"success"`)
- `color`: Hex color code for this target group
- `defaultTargetColor`: Color for IPs that don't match any pattern (default: `"#f39c12"`)
- `showUnmatchedIPs`: If `true`, displays actual IP addresses for unmatched targets (default: `true`)
- `layerColors`: Colors for customer and device nodes

**Behavior for Unmatched IPs:**
When `showUnmatchedIPs` is `true` (default), any IP addresses that don't match the defined patterns will be displayed as their actual IP address in the diagram, colored with `defaultTargetColor`. This makes it easy to identify new or unexpected targets.

Changes take effect on next dashboard refresh.

## Color Coding (Default)

- 🔵 **Blue (Relay)**: `209.34.233.*` - Legacy backend servers
- 🟢 **Green (Mirrored)**: `192.168.94.241` - New backend servers  
- 🔴 **Red (Errors/Timeouts)**: Failed/timeout connections
- 🟠 **Orange (Other IPs)**: Unmatched target IPs (shown as actual address)
- 🟣 **Purple (Customers)**: Customer nodes
- ⚫ **Dark Gray (Devices)**: Device type nodes

*Note: These can be customized in `dashboard-config.json`*

## System Logs

Click the **📋 System Logs** panel at the bottom of the dashboard to view:
- Configuration loading status
- Data fetch operations
- Node/link processing information
- Errors and warnings

Logs are kept in memory (last 100 entries) and include timestamps and severity levels.

## Metrics Explained

| Metric | Description |
|--------|-------------|
| **Total Packets** | Sum of all UDP packets processed in time range |
| **Active Routes** | Number of unique port:target combinations with traffic |
| **Avg Latency** | Weighted average response time across all flows |
| **Success Rate** | Percentage of packets that received successful ACKs |
| **Active Customers** | Number of distinct customers with traffic |

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `INFLUX_URL` | Yes | `http://localhost:8086` | InfluxDB server URL |
| `INFLUX_TOKEN` | Yes | - | InfluxDB authentication token |
| `INFLUX_ORG` | Yes | - | InfluxDB organization name |
| `INFLUX_BUCKET` | Yes | - | InfluxDB bucket containing metrics |
| `INFLUX_SKIP_TLS_VERIFY` | No | `false` | Skip TLS certificate verification (internal PKI/self-signed certs) |
| `DASHBOARD_PORT` | No | `8080` | HTTP port for dashboard server |

**Security Note**: Only set `INFLUX_SKIP_TLS_VERIFY=true` in trusted environments (internal networks, development). For production with public InfluxDB instances, always verify certificates.

## API Endpoints

### GET /api/traffic-flows

Query traffic flow data with filtering options.

**Parameters:**
- `timeRange`: `5m`, `15m`, `1h`, `3h`, `6h`, `24h`
- `flowType`: `customer-device-target`, `customer-target`, `device-target`
- `statusFilter`: `all`, `success`, `timeout`, `error`  
- `minPackets`: Minimum packet count threshold (default: 10)

**Example:**
```bash
curl "http://localhost:8080/api/traffic-flows?timeRange=1h&flowType=customer-device-target&statusFilter=all&minPackets=50"
```

**Response:**
```json
{
  "metrics": {
    "totalPackets": 145623,
    "activeRoutes": 247,
    "avgLatency": 23.4,
    "successRate": 0.987,
    "activeCustomers": 45
  },
  "flows": {
    "nodes": [
      {
        "id": "customer_soltrack",
        "name": "soltrack", 
        "layer": "customer",
        "value": 45230,
        "successRate": 0.99
      }
    ],
    "links": [
      {
        "source": "customer_soltrack",
        "target": "device_calamplmd", 
        "value": 32450,
        "avgLatency": 18.3
      }
    ]
  }
}
```

## Data Requirements

The dashboard reads from your InfluxDB `udp_relay` measurement with these fields:

**Tags:**
- `customer`: Customer name from routes.json
- `device`: Device type from routes.json  
- `target`: Target IP address
- `port`: UDP port number
- `status`: `success`, `timeout`, or `error`

**Fields:**
- `count`: Number of packets (integer)
- `latency_ms`: Response latency in milliseconds (float)

## Troubleshooting

### Startup Failures

#### "Configuration validation failed"
- Check that all required environment variables are set: `INFLUX_TOKEN`, `INFLUX_ORG`, `INFLUX_BUCKET`
- Verify `DASHBOARD_PORT` is a valid number
- Example: `export INFLUX_TOKEN=your-token-here`

#### "InfluxDB connection failed: health check failed"
1. ✅ Verify InfluxDB is running: `curl http://your-influxdb:8086/ping`
2. ✅ Check `INFLUX_URL` points to the correct host and port
3. ✅ Verify network connectivity (firewall rules, Docker network)
4. ✅ Check InfluxDB logs for errors

#### "InfluxDB connection failed: health check returned status: fail"
- InfluxDB is reachable but not healthy
- Check InfluxDB logs: `docker logs influxdb` (if using Docker)
- Verify InfluxDB has sufficient resources (disk space, memory)

#### "Warning: InfluxDB query test failed"
- This is a warning, dashboard will still start
- Usually means the bucket is empty or has no recent data
- Verify bucket name is correct: `INFLUX_BUCKET=your-bucket`
- Check token has read permissions on the bucket

#### "tls: failed to verify certificate: x509: certificate signed by unknown authority"
- This occurs when using internal PKI or self-signed certificates
- **Solution 1 (Quick)**: Skip TLS verification (dev/internal networks only)
  ```bash
  INFLUX_SKIP_TLS_VERIFY=true
  ```
- **Solution 2 (Production)**: Add your CA certificate to the container
- **Warning**: Only skip TLS verification in trusted environments

Example with TLS verification disabled:
```bash
docker run -d \
  --name udp-switchboard-dashboard \
  -p 8080:8080 \
  -e INFLUX_URL=https://your-influxdb:8086 \
  -e INFLUX_TOKEN=your-token \
  -e INFLUX_ORG=your-org \
  -e INFLUX_BUCKET=your-bucket \
  -e INFLUX_SKIP_TLS_VERIFY=true \
  udp-switchboard-dashboard
```

### Dashboard shows "No data available"
1. ✅ Verify InfluxDB connection settings in `.env`
2. ✅ Ensure UDP Switchboard is running and generating metrics
3. ✅ Check that metrics exist for your selected time range
4. ✅ Try increasing the time range (start with 24h)
5. ✅ Lower the minimum packet threshold

### Connection errors
1. ✅ Test InfluxDB connectivity: `curl http://your-influxdb:8086/ping`
2. ✅ Verify token has read permissions on the bucket
3. ✅ Check firewall/network access to InfluxDB

### Performance Issues
1. ✅ Use shorter time ranges for faster queries
2. ✅ Increase minimum packet threshold to reduce node count
3. ✅ Consider creating InfluxDB retention policies for old data

## Development Mode

The dashboard includes mock data when running locally. Simply open `index.html` in a browser to see the interface with sample data.

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Browser       │───▶│  Dashboard       │───▶│   InfluxDB      │
│   (D3.js +      │    │  Server          │    │   (Metrics)     │
│   Sankey)       │◀───│  (Go + HTTP)     │◀───│                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

The Go server queries InfluxDB, aggregates traffic flows, and serves both the static files and REST API. The frontend uses D3.js with the Sankey plugin for interactive visualizations.

---

## Integration with UDP Switchboard

This dashboard is designed to work seamlessly with your existing UDP Switchboard deployment. Simply ensure:

1. **Metrics are flowing**: Your switchboard is writing to InfluxDB
2. **Customer/Device tags**: Routes include `customer` and `Device` fields
3. **Network access**: Dashboard server can reach InfluxDB

The visualization will automatically update to show your real traffic patterns, helping you monitor customer migration from ATL Legacy to Nginx backends in real-time.
