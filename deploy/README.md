# Packet Wire Deployment Guide

Production-ready Docker Compose configurations for deploying Packet Wire stack.

## Quick Start

Each service has its own Docker Compose file for maximum flexibility.

### Start Individual Services

**Dashboard:**
```bash
cd deploy
docker-compose -f docker-compose.dashboard.yml up -d
```
Access at: http://localhost:8080

**UDP Listener:**
```bash
cd deploy
docker-compose -f docker-compose.udp-listener.yml up -d
```
Listening on: UDP port 9000

**UDP Sender:**
```bash
cd deploy
docker-compose -f docker-compose.udp-sender.yml up -d
```
Sends test packets to udp-listener:9000 every 10s

### Start All Services

**Linux/Mac:**
```bash
cd deploy
chmod +x start.sh
./start.sh
```

**Windows (PowerShell):**
```powershell
cd deploy
.\start.ps1
```

**Manual:**
```bash
cd deploy
docker-compose -f docker-compose.dashboard.yml up -d
docker-compose -f docker-compose.udp-listener.yml up -d
docker-compose -f docker-compose.udp-sender.yml up -d
```

## Docker Compose Files

- `docker-compose.dashboard.yml` - Dashboard service only
- `docker-compose.udp-listener.yml` - UDP Listener service only
- `docker-compose.udp-sender.yml` - UDP Sender service only

All services share the same Docker network (`packet-wire-network`) for seamless communication.

## Configuration Files

### `listener-config.json`

UDP Listener configuration. Key settings:

```json
{
  "listen_addr": ":9000",
  "capture_dir": "/captures",
  "reply_mode": true,
  "ack_message": "ACK-PACKET-WIRE",
  "client_id": "listener-01",
  "dashboard_url": "http://dashboard:8080/api/metrics",
  "heartbeat_interval_seconds": 5
}
```

**Important fields:**
- `listen_addr`: Port to listen on (`:9000` = all interfaces, port 9000)
- `reply_mode`: Send ACK replies to senders
- `client_id`: Unique identifier for this listener instance
- `dashboard_url`: Dashboard metrics endpoint
- `encryption_passphrase`: Leave empty to disable encryption

### `dashboard-config.json`

Dashboard configuration for Sankey flow visualization:

```json
{
  "targetGroups": [
    {
      "name": "Relay",
      "ipPatterns": ["209.34.233.*"],
      "color": "#3498db",
      "description": "Legacy backend servers"
    }
  ],
  "defaultTargetColor": "#f39c12",
  "showUnmatchedIPs": true,
  "layerColors": {
    "customer": "#9b59b6",
    "device": "#34495e"
  },
  "logLevel": "info"
}
```

**Key settings:**
- `targetGroups`: Define IP groups with colors for Sankey visualization
  - `ipPatterns`: Array of IP patterns (supports wildcards)
  - `statusMatch`: Match by status (error, timeout, success)
  - `color`: Hex color for this target group
- `defaultTargetColor`: Color for unmatched IPs
- `showUnmatchedIPs`: Show IPs not matching any target group
- `layerColors`: Colors for customer and device nodes in Sankey diagram

### `env.example` (optional)

Copy `env.example` to `.env` for environment-based configuration:

```bash
cp env.example .env
# Edit .env with your values
```

Then use with docker-compose:
```bash
docker-compose -f docker-compose.dashboard.yml --env-file .env up -d
docker-compose -f docker-compose.udp-listener.yml --env-file .env up -d
```

## Port Configuration

Default ports:
- **Dashboard Web UI:** 8080 (TCP)
- **UDP Listener:** 9000 (UDP)

To change ports, edit the respective Docker Compose file:

**Dashboard** (`docker-compose.dashboard.yml`):
```yaml
services:
  dashboard:
    ports:
      - "8081:8080"  # Change host port (8081)
```

**UDP Listener** (`docker-compose.udp-listener.yml`):
```yaml
services:
  udp-listener:
    ports:
      - "9100:9000/udp"  # Change host port (9100)
```

**Note:** If changing dashboard port, also update `listener-config.json` → `dashboard_url`.

## Volume Mounts

### Captures Directory

All UDP packet captures are stored in `./captures`:

```bash
deploy/
  captures/
    1234567890_udp_capture.csv
    1234567891_udp_capture.csv
```

Files are named with Unix epoch timestamp at startup.

### Configuration Files

Config files are mounted read-only:
- `listener-config.json` → `/config/config.json`
- `dashboard-config.json` → `/app/dashboard-config.json`

To apply config changes:
```bash
# Restart specific service
docker-compose -f docker-compose.udp-listener.yml restart

# Or restart all services
docker-compose -f docker-compose.dashboard.yml restart
docker-compose -f docker-compose.udp-listener.yml restart
docker-compose -f docker-compose.udp-sender.yml restart
```

## Network Architecture

All services communicate via Docker bridge network `packet-wire-network`:

```
Dashboard (dashboard:8080)
    ↑
    │ HTTP /api/metrics
    │
UDP Listener (udp-listener:9000)
    ↑
    │ UDP packets
    │
UDP Sender (udp-sender) or External clients
```

**Service DNS names:**
- `dashboard` - Dashboard service
- `udp-listener` - UDP Listener service
- `udp-sender` - UDP Sender service (full stack only)

## Managing Services

### Start services

```bash
# Start individual service
docker-compose -f docker-compose.dashboard.yml up -d
docker-compose -f docker-compose.udp-listener.yml up -d
docker-compose -f docker-compose.udp-sender.yml up -d

# Or use convenience script
./start.sh  # Linux/Mac
.\start.ps1  # Windows
```

### Stop services

```bash
# Stop individual service
docker-compose -f docker-compose.dashboard.yml down
docker-compose -f docker-compose.udp-listener.yml down
docker-compose -f docker-compose.udp-sender.yml down

# Stop all (preserves network)
docker stop packet-wire-dashboard packet-wire-listener packet-wire-sender
docker rm packet-wire-dashboard packet-wire-listener packet-wire-sender
```

### View logs

```bash
# Individual service
docker-compose -f docker-compose.udp-listener.yml logs -f
docker-compose -f docker-compose.dashboard.yml logs -f
docker-compose -f docker-compose.udp-sender.yml logs -f

# Using container names
docker logs -f packet-wire-listener
docker logs -f packet-wire-dashboard
docker logs -f packet-wire-sender
```

### Restart a service

```bash
docker-compose -f docker-compose.udp-listener.yml restart
docker-compose -f docker-compose.dashboard.yml restart
```

### Check service status

```bash
# All containers
docker ps | grep packet-wire

# Specific service
docker-compose -f docker-compose.udp-listener.yml ps
```

### Update images

```bash
# Pull latest images
docker pull ghcr.io/malindarathnayake/packet-wire-dashboard:latest
docker pull ghcr.io/malindarathnayake/packet-wire-udp-listener:latest
docker pull ghcr.io/malindarathnayake/packet-wire-udp-sender:latest

# Restart with new images
docker-compose -f docker-compose.dashboard.yml up -d --force-recreate
docker-compose -f docker-compose.udp-listener.yml up -d --force-recreate
docker-compose -f docker-compose.udp-sender.yml up -d --force-recreate
```

## Testing the Setup

### 1. Verify Dashboard

```bash
curl http://localhost:8080/health
```

Expected: `200 OK`

### 2. Send Test UDP Packet

**Using netcat (Linux/Mac):**
```bash
echo -n "TESTMESSAGE" | nc -u localhost 9000
```

**Using PowerShell (Windows):**
```powershell
$client = New-Object System.Net.Sockets.UdpClient
$bytes = [System.Text.Encoding]::ASCII.GetBytes("TESTMESSAGE")
$client.Send($bytes, $bytes.Length, "localhost", 9000)
$client.Close()
```

**Using Docker UDP Sender:**
```bash
docker run --rm \
  --network packet-wire-network \
  ghcr.io/malindarathnayake/packet-wire-udp-sender:latest \
  --target udp-listener:9000 \
  --message "TESTPACKET"
```

### 3. Check Captures

```bash
ls -lh captures/
cat captures/*.csv
```

### 4. View Dashboard

Open browser: http://localhost:8080

Check that metrics are being received from the listener.

## Production Considerations

### Security

1. **Firewall:** Only expose necessary ports
2. **Encryption:** Set `encryption_passphrase` in `listener-config.json`
3. **Secrets:** Use Docker secrets instead of config files for sensitive data

### Resource Limits

Add resource limits to the respective Docker Compose file (e.g., `docker-compose.udp-listener.yml`):

```yaml
services:
  udp-listener:
    deploy:
      resources:
        limits:
          cpus: '0.5'
          memory: 256M
        reservations:
          cpus: '0.25'
          memory: 128M
```

### Persistence

For production, use named volumes instead of bind mounts:

```yaml
volumes:
  packet-wire-captures:

services:
  udp-listener:
    volumes:
      - packet-wire-captures:/captures
```

### Monitoring

Check health status:

```bash
docker inspect packet-wire-dashboard | grep -A 10 "Health"
```

### Backups

Backup captures directory:

```bash
tar -czf captures-backup-$(date +%Y%m%d).tar.gz captures/
```

## Troubleshooting

### Dashboard not receiving metrics

1. Check listener logs:
   ```bash
   docker logs packet-wire-listener
   ```

2. Verify network connectivity:
   ```bash
   docker exec packet-wire-listener ping dashboard
   ```

3. Check `dashboard_url` in `listener-config.json`

### UDP packets not being received

1. Check listener is running:
   ```bash
   docker ps | grep packet-wire-listener
   ```

2. Verify port is open:
   ```bash
   netstat -an | grep 9000
   ```

3. Check firewall rules (Windows):
   ```powershell
   netsh advfirewall firewall show rule name=all | Select-String 9000
   ```

### Permission errors on captures directory

```bash
# Fix permissions (Linux/Mac)
chmod 777 captures/

# Or run as specific user
docker-compose -f docker-compose.udp-listener.yml down
chown -R 1000:1000 captures/
docker-compose -f docker-compose.udp-listener.yml up -d
```

## Advanced Configuration

### Multiple UDP Listeners

Run multiple listeners on different ports:

```yaml
services:
  udp-listener-9000:
    image: ghcr.io/malindarathnayake/packet-wire-udp-listener:latest
    ports:
      - "9000:9000/udp"
    environment:
      - CLIENT_ID=listener-01
      - UDP_LISTEN_ADDR=:9000
    volumes:
      - ./captures-9000:/captures

  udp-listener-9100:
    image: ghcr.io/malindarathnayake/packet-wire-udp-listener:latest
    ports:
      - "9100:9100/udp"
    environment:
      - CLIENT_ID=listener-02
      - UDP_LISTEN_ADDR=:9100
    volumes:
      - ./captures-9100:/captures
```

### External Access

To access from outside Docker network, use host network mode:

```yaml
services:
  udp-listener:
    network_mode: host
    # Remove 'ports' section when using host mode
```

**Note:** Host networking doesn't work on Docker Desktop (Windows/Mac).

## Support

For issues and questions:
- Check logs: `docker-compose logs`
- Review component READMEs: [UDP Listener](../udp-listener/README.md), [Dashboard](../dashboard/README.md)
- Check project documentation: [Engineering Standards](../_docs/engineering-standards.md)

