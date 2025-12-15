# Packet-Wire Test Workflow

This document describes the **PW-Test Mode** workflow for detecting packet drops and analyzing UDP transmission quality between sender and listener.

## Overview

The PW-Test Mode (`--pw-test-mode`) adds sequence numbers to packets, enabling:
- **Drop detection**: Identify missing packets by sequence gaps
- **Latency analysis**: Track per-packet timing
- **ACK validation**: Verify listener responses match expected format

## Packet Format

When `--pw-test-mode` is enabled, packets are structured as:

```
TEST|{seq}|TIMESTAMP|{nanoseconds}|{your_message}
```

Example:
```
TEST|42|TIMESTAMP|1702666543123456789|PING
```

With `--raw` mode (no timestamp):
```
TEST|42|PING
```

## Quick Start

### 1. Configure the Listener

Edit `udp-listener/config.json`:

```json
{
  "listen_addr": ":9000",
  "capture_dir": "./captures",
  "reply_mode": true,
  "id_name": "SERVER01",
  "test_mode": true,
  "enhanced_ack": true,
  "test_report_file": "./captures/test_report.json"
}
```

| Option | Description |
|--------|-------------|
| `test_mode` | Enable detailed message tracking |
| `enhanced_ack` | Send structured ACK: `ACK\|{seq}\|{timestamp}\|{id}` |
| `test_report_file` | Output path for JSON report (auto-generated if empty) |

### 2. Start the Listener

```powershell
cd udp-listener
$env:CONFIG_FILE = "config.json"
go run main.go
```

Or with Docker:
```powershell
docker run --rm -it `
  -p 9000:9000/udp `
  -v "${PWD}\captures:/captures" `
  -v "${PWD}\udp-listener\config.json:/config/config.json:ro" `
  -e CONFIG_FILE="/config/config.json" `
  udp-listener:latest
```

### 3. Run the Sender with PW-Test Mode

```powershell
cd udp-sender

# Send 100 packets with sequence tracking
.\udp-sender.exe --target 127.0.0.1:9000 --message "PING" `
  --continuous --interval-ms 100 --count 100 `
  --pw-test-mode --console
```

### 4. Stop Listener & View Report

Press `Ctrl+C` on the listener. It outputs:

```
═══════════════════════════════════════════════════════════════
                    TEST MODE SUMMARY
═══════════════════════════════════════════════════════════════
  Total Received:  97 packets
  Total ACKed:     97 packets
  Sequence Range:  1 - 100
  Expected Count:  100
  Missing Seqs:    3 (DROPS DETECTED)
  First Missing:   [23 45 78]...
  Latency (avg):   2.451 ms
  Latency (min):   0.234 ms
  Latency (max):   15.678 ms
═══════════════════════════════════════════════════════════════
test report written to: ./captures/test_report.json
```

---

## Sender Options

### PW-Test Mode Flag

```
--pw-test-mode    Prepend TEST|{seq}| to each message for drop analysis
```

### Example Commands

```powershell
# Basic test - 100 packets at 100ms intervals
.\udp-sender.exe --target 127.0.0.1:9000 --message "TEST" `
  --continuous --interval-ms 100 --count 100 `
  --pw-test-mode --console

# Stress test with autotest
.\udp-sender.exe --target 127.0.0.1:9000 --message "STRESS" `
  --autotest --max-pps 50 --warmup-mins 1 `
  --pw-test-mode --output sender_results.json

# High-frequency test (10 PPS for 60 seconds)
.\udp-sender.exe --target 127.0.0.1:9000 --message "BURST" `
  --continuous --interval-ms 100 --count 600 `
  --pw-test-mode

# With encryption
.\udp-sender.exe --target 127.0.0.1:9000 --message "SECURE" `
  --passphrase "my-secret-key" `
  --continuous --interval-ms 200 --count 50 `
  --pw-test-mode --console
```

---

## Listener Options

### Test Mode Configuration

| Config Key | Env Variable | Default | Description |
|------------|--------------|---------|-------------|
| `test_mode` | `TEST_MODE` | `false` | Enable test message tracking |
| `test_report_file` | `TEST_REPORT_FILE` | auto | Output path for JSON report |
| `enhanced_ack` | `ENHANCED_ACK` | `false` | Structured ACK format |

### Enhanced ACK Format

When `enhanced_ack: true`, the listener responds with:

```
ACK|{seq_num}|{receive_timestamp_ns}|{id_name}
```

Example:
```
ACK|42|1702666543123456789|SERVER01
```

This allows the sender to:
- Validate ACK came from the correct listener
- Know which packet was acknowledged
- Calculate one-way latency

---

## Report Format

The test report (`test_report.json`) contains:

```json
{
  "listener_id": "SERVER01",
  "listen_addr": ":9000",
  "start_time": "2024-12-15T10:00:00Z",
  "end_time": "2024-12-15T10:05:00Z",
  "total_received": 97,
  "total_acked": 97,
  "first_seq": 1,
  "last_seq": 100,
  "expected_count": 100,
  "missing_seqs": [23, 45, 78],
  "avg_latency_ms": 2.451,
  "min_latency_ms": 0.234,
  "max_latency_ms": 15.678,
  "records": [
    {
      "seq_num": 1,
      "receive_time": "2024-12-15T10:00:00.123Z",
      "ack_time": "2024-12-15T10:00:00.124Z",
      "source_addr": "192.168.1.100:54321",
      "sender_timestamp_ns": 1702666543123456789,
      "latency_ms": 1.234,
      "payload_size": 45,
      "ack_sent": true,
      "message": "TEST|1|TIMESTAMP|1702666543123456789|PING"
    }
  ]
}
```

### Report Fields

| Field | Description |
|-------|-------------|
| `total_received` | Number of packets received |
| `total_acked` | Number of ACKs sent |
| `first_seq` / `last_seq` | Sequence number range |
| `expected_count` | Expected packets based on sequence range |
| `missing_seqs` | Array of missing sequence numbers (drops) |
| `avg_latency_ms` | Average sender→listener latency |
| `records` | Per-packet details |

---

## Sequence Number Detection

The listener extracts sequence numbers from multiple formats:

### 1. PW-Test Format (Recommended)
```
TEST|{seq}|{payload}
```

### 2. JSON Payloads
The listener auto-detects these fields:
- `seq`
- `sequence`
- `seq_num`
- `seqnum`
- `packet_num`
- `id`

Example:
```json
{"seq": 42, "data": "test"}
```

---

## Full Test Workflow Example

### Step 1: Prepare Environment

```powershell
# Terminal 1 - Start listener
cd udp-listener
$env:CONFIG_FILE = "config.json"
go run main.go
```

### Step 2: Run Test

```powershell
# Terminal 2 - Run sender
cd udp-sender
.\udp-sender.exe --target 127.0.0.1:9000 --message "PINGTEST" `
  --continuous --interval-ms 50 --count 500 `
  --pw-test-mode --console
```

### Step 3: Analyze Results

```powershell
# Terminal 1 - Stop listener with Ctrl+C
# Review console summary

# Parse JSON report
Get-Content .\captures\test_report.json | ConvertFrom-Json | Select-Object total_received, missing_seqs
```

### Step 4: Interpret Results

| Metric | Good | Warning | Bad |
|--------|------|---------|-----|
| Missing Seqs | 0 | 1-5 | >5 |
| Success Rate | >99% | 95-99% | <95% |
| Avg Latency | <5ms | 5-20ms | >20ms |

---

## Troubleshooting

### No Sequence Numbers Detected

- Ensure `--pw-test-mode` is enabled on sender
- Check message format starts with `TEST|`
- Verify listener has `test_mode: true`

### Missing ACKs

- Enable `reply_mode: true` on listener
- Check firewall allows UDP responses
- Increase `--reply-timeout-ms` on sender

### High Latency Variance

- Network congestion or routing issues
- Try different intervals
- Check for packet buffering

### Report File Not Created

- Verify `test_report_file` path is writable
- Ensure listener shuts down gracefully (Ctrl+C, not kill)
- Check `capture_dir` directory exists

---

## Docker Compose Test Setup

```yaml
version: '3.8'
services:
  listener:
    build: ./udp-listener
    ports:
      - "9000:9000/udp"
    volumes:
      - ./captures:/captures
      - ./udp-listener/config.json:/config/config.json:ro
    environment:
      - CONFIG_FILE=/config/config.json

  # Run sender manually or as one-shot
  sender:
    build: ./udp-sender
    depends_on:
      - listener
    command: >
      --target listener:9000
      --message "DOCKER-TEST"
      --continuous --interval-ms 100 --count 100
      --pw-test-mode
```

---

## See Also

- [UDP Listener README](../udp-listener/README.md)
- [UDP Sender README](../udp-sender/README.md)
- [Dashboard Metrics Spec](./dashboard-metrics-spec.md)

