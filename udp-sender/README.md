<p align="center">
  <img src="../Logo/svglogo.svg" alt="Packet Wire" width="400">
</p>

# UDP Sender

CLI to send UDP messages or chunked file payloads. Useful for testing UDP endpoints and simulating IoT devices.

> Testing for packet drops? See the [PW-Test Workflow Guide](../_docs/test-workflow.md).

## Build (Docker image)

From the repo root:

```bash
docker build -t udp-sender -f /udp-sender/Dockerfi .
```

## Run with Docker

### Send a message

```bash
# Docker Desktop (Windows/Mac)
docker run --rm udp-sender \
  --target host.docker.internal:9000 \
  --message '{"device_id":"test-1","status":"ok"}'

# Linux host networking
docker run --rm --network host udp-sender \
  --target 127.0.0.1:9000 \
  --message '{"device_id":"test-1","status":"ok"}'
```

### Send a file

Mount the file into the container and point `--file` at the in-container path:

```bash
docker run --rm \
  -v /path/on/host/sample.bin:/data/sample.bin:ro \
  udp-sender \
  --target host.docker.internal:9000 \
  --file /data/sample.bin \
  --chunk-size 1024
```



## Build (binary)

From the repo root:

```bash
docker build -t udp-sender -f udp-sender/Dockerfile .
```

Example usage:

```bash
# Send a single JSON message
./udp-sender --target 127.0.0.1:9000 --message '{"device_id":"test-1","status":"ok"}'

# Send a file in 1KB chunks
./udp-sender --target 127.0.0.1:9000 --file ./captures/sample.bin --chunk-size 1024
```

## Continuous/Repeated Sending

Send messages at defined intervals:

```bash
# Send every 2 seconds (proceeds without confirmation)
./udp-sender --target 127.0.0.1:9000 --message '{"heartbeat":true}' \
  --continuous --interval-sec 2

# Send every 500ms (high frequency)
./udp-sender --target 127.0.0.1:9000 --message '{"ping":true}' \
  --continuous --interval-ms 500

# Send 10 packets at 1-second intervals then stop
./udp-sender --target 127.0.0.1:9000 --message '{"test":true}' \
  --continuous --interval-sec 1 --count 10
```

### Interval Validation

| Interval | Behavior |
|----------|----------|
| 1-5 seconds | Proceeds without confirmation |
| >5 seconds | Requires `--confirm` flag (guards against typos) |

```bash
# This will fail (interval too high, might be a mistake)
./udp-sender --target 127.0.0.1:9000 --message '{"slow":true}' \
  --continuous --interval-sec 10

# Add --confirm to proceed with high intervals
./udp-sender --target 127.0.0.1:9000 --message '{"slow":true}' \
  --continuous --interval-sec 10 --confirm
```

### Interval Flags

| Flag | Description |
|------|-------------|
| `--interval-sec` | Seconds between packets (e.g., `2`, `0.5`) |
| `--interval-ms` | Milliseconds between packets (e.g., `500`, `100`) |
| `--count` | Stop after N packets (0 = unlimited) |
| `--confirm` | Required for intervals >5 seconds |

> **Note:** Use either `--interval-sec` OR `--interval-ms`, not both.

## Console Mode

Enable verbose output showing packet details and replies with `--console`:

```bash
# Basic console mode
./udp-sender --target 127.0.0.1:9000 --message '{"ping":true}' --console

# Continuous with console (see each packet and reply)
./udp-sender --target 127.0.0.1:9000 --message '{"test":true}' \
  --continuous --interval-sec 2 --console

# Custom reply timeout (default 1000ms)
./udp-sender --target 127.0.0.1:9000 --message '{"ping":true}' \
  --console --reply-timeout-ms 2000
```

### Console Output

Console mode displays:
- **Packet number** and timestamp
- **Sent bytes** and encryption status
- **Message preview** (truncated if long)
- **Reply content** (if received within timeout)
  - ASCII text shown as readable strings
  - Binary/non-ASCII shown as hex dump
- **RTT** (round-trip time) for replies

Example output:
```
╔══════════════════════════════════════════════════════════════════╗
║                    UDP SENDER - CONSOLE MODE                     ║
╠══════════════════════════════════════════════════════════════════╣
║  Target: 127.0.0.1:9000                                          ║
║  Reply Timeout: 1000ms                                           ║
║  Encryption: None                                                ║
╚══════════════════════════════════════════════════════════════════╝

┌─────────────────────────────────────────────────────────────────┐
│ PACKET #1                                          [14:30:15.123] │
├─────────────────────────────────────────────────────────────────┤
│ → SENT to 127.0.0.1:9000
│   Bytes: 45 (plain)
│   Message:
│   "{"ping":true}"
│
│ ← REPLY from 127.0.0.1:9000
│   Bytes: 32 | RTT: 1.234ms
│   Content:
│   "{"status":"ok","received":true}"
└─────────────────────────────────────────────────────────────────┘
```

### Console Flags

| Flag | Description |
|------|-------------|
| `--console` | Enable verbose console output |
| `--reply-timeout-ms` | Milliseconds to wait for reply (default: 1000) |
| `--raw` | Send message without TIMESTAMP prefix |

## PW-Test Mode (Drop Analysis)

Enable sequence number tracking to detect packet drops:

```bash
# Send 100 packets with sequence tracking
./udp-sender --target 127.0.0.1:9000 --message "PING" \
  --continuous --interval-ms 100 --count 100 \
  --pw-test-mode --console

# Combined with autotest for stress testing
./udp-sender --target 127.0.0.1:9000 --message "STRESS" \
  --autotest --max-pps 50 --pw-test-mode --output results.json
```

### Packet Format

When enabled, packets are prefixed with `TEST|{seq}|`:

```
TEST|42|TIMESTAMP|1702666543123456789|PING
```

With `--raw` mode:
```
TEST|42|PING
```

### PW-Test Flags

| Flag | Description |
|------|-------------|
| `--pw-test-mode` | Prepend `TEST\|{seq}\|` to messages for drop analysis |

> **Full documentation:** See [PW-Test Workflow Guide](../_docs/test-workflow.md) for listener configuration and report analysis.

## Autotest Mode (Stress Testing)

Run automated stress tests to find the breaking point of your UDP endpoint:

```bash
# Basic autotest (5 min warm-up, ramp to 100 PPS)
./udp-sender --target 192.168.94.22:20671 --message "TESTMESSAGE" --raw --autotest --console

# Custom warm-up and max PPS
./udp-sender --target 192.168.94.22:20671 --message "TEST" --raw --autotest --warmup-mins 2 --max-pps 50 --console
```

### How It Works

| Phase | Description |
|-------|-------------|
| **Phase 1: Warm-up** | Sends 1 packet every 10 seconds for N minutes (default: 5 mins) |
| **Phase 2: Ramp-up** | Tests increasing PPS: 1 → 2 → 5 → 10 → 20 → 50 → 100... |

Each PPS level runs for 30 seconds. If success rate drops below 80%, that's marked as the **breaking point**.

### Autotest Flags

| Flag | Description |
|------|-------------|
| `--autotest` | Enable autotest mode |
| `--warmup-mins` | Warm-up duration in minutes (default: 5) |
| `--max-pps` | Maximum packets per second to test (default: 100) |
| `--output` | Save results to JSON file |

### Example Output

```
╔══════════════════════════════════════════════════════════════════════╗
║                       AUTOTEST SUMMARY                               ║
╠══════════════════════════════════════════════════════════════════════╣
║  Total Packets Sent:    1250                                         ║
║  Replies Received:      1180                                         ║
║  Timeouts:              70                                           ║
║  Overall Success Rate:  94.4%                                        ║
╠══════════════════════════════════════════════════════════════════════╣
║  Max Successful PPS:    50                                           ║
║  Breaking Point:        100                                        ⚠ ║
╚══════════════════════════════════════════════════════════════════════╝
```

### Save Results to JSON

```bash
./udp-sender --target 192.168.94.22:20671 --message "TEST" --raw --autotest --output results.json
```

Output file example:
```json
{
  "timestamp": "2025-12-15T13:30:00Z",
  "target": "192.168.94.22:20671",
  "test_type": "autotest",
  "duration": "5m30s",
  "packets_sent": 1250,
  "replies_received": 1180,
  "timeouts": 70,
  "success_rate_percent": 94.4,
  "max_success_pps": 50,
  "breaking_point": 100,
  "raw_mode": true,
  "encrypted": false
}
```

## Multi-Target Mode

Test multiple endpoints from a single JSON config file (max 10 targets):

```bash
./udp-sender --targets-file targets.json --raw --autotest --output results.json
```

### Targets File Format

Create a `targets.json`:
```json
{
  "targets": [
    {
      "name": "Server-A",
      "address": "192.168.94.22:20671",
      "message": "PING-A"
    },
    {
      "name": "Server-B",
      "address": "192.168.94.23:20671",
      "message": "PING-B"
    },
    {
      "name": "Server-C",
      "address": "10.0.0.50:9000"
    }
  ]
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | No | Display name (defaults to address) |
| `address` | **Yes** | Target IP:port |
| `message` | No | Target-specific message (uses `--message` if omitted) |
| `passphrase` | No | Target-specific encryption key |

### Multi-Target Flags

| Flag | Description |
|------|-------------|
| `--targets-file` | Path to JSON file with targets (max 10) |
| `--output` | Save all results to JSON file |

