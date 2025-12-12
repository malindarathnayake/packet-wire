# Feature Spec: Dual Mirroring

**Status:** Final  
**Author:** Engineering  
**Date:** December 2025

---

## Intent

Send every UDP packet to two backends simultaneously:
1. **Primary target** - existing backend (from `"target"` field)
2. **Mirror target** - secondary backend (from `"mirror_ip"`, defaults to global)

One backend waits for device-specific ACK payload and relays it back. The other is fire-and-forget.

**Use case**: Migration testing - send traffic to both old (ATL) and new (Nginx) infrastructure, choose which one responds to devices.

---

## Architecture

### Current Flow (No Mirroring)

```
Device → Switchboard → Primary Backend
              ↓
         ACK relay ← 
```

### Proposed Flow (With Mirroring)

```
Device → Switchboard ─┬─→ ACK Backend (wait for ACK) → Relay ACK to device
                      │
                      └─→ Fire-and-Forget Backend (send only, no wait)
```

### Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| ACK source | Configurable (primary or mirror) | Supports migration testing |
| Non-ACK backend | Fire-and-forget | Don't block ACK flow |
| Mirror timing | Concurrent with primary | Minimize latency impact |
| Fire-and-forget | Send only, no read | Simplest implementation |
| Global defaults | routes.json top-level | Reduces config duplication |

---

## Configuration Schema

### routes.json - Full Example

```json
{
  "mirror": true,
  "mirror_ip": "192.168.94.241",
  "ack_from_mirror": false,
  "routes": [
    {
      "port": 18172,
      "target": "207.189.178.80",
      "customer": "powertelematics",
      "Device": "calamplmd"
    },
    {
      "port": 8807,
      "target": "209.34.233.116",
      "customer": "legacy-customer",
      "Device": "legacy-device",
      "ack_from_mirror": true
    },
    {
      "port": 10166,
      "target": "209.34.233.121",
      "customer": "acme",
      "Device": "sensor-alpha",
      "mirror": false
    }
  ]
}
```

### Global Fields (routes.json top-level)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `mirror` | bool | false | Enable mirroring for all routes |
| `mirror_ip` | string | "" | Default mirror IP (IPv4) |
| `ack_from_mirror` | bool | false | Wait for ACK from mirror instead of primary |

### Per-Route Fields (override global)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `port` | int | required | UDP port to listen on (1-65535) |
| `target` | string | required | Backend IP to forward to (IPv4) |
| `customer` | string | "" | Label for metrics/logging context |
| `Device` | string | "" | Device type label for metrics |
| `mirror` | bool | (global) | Override global mirror setting |
| `mirror_ip` | string | (global) | Override global mirror IP |
| `ack_from_mirror` | bool | (global) | Override ACK source |

### config.yaml Addition

```yaml
app:
  bind_ip: "0.0.0.0"
  response_timeout_ms: 2000
  routes_file: "config/routes.json"
  mirror_ip: "192.168.94.241"  # Fallback if not in routes.json
```

### Field Resolution Priority

| Field | Resolution Order |
|-------|-----------------|
| `mirror` | Route-level → Global (routes.json) → `false` |
| `mirror_ip` | Route-level → Global (routes.json) → Global (config.yaml) → Error |
| `ack_from_mirror` | Route-level → Global (routes.json) → `false` |

---

## Behavior Specification

### Packet Flow

#### Case 1: Mirroring Disabled

```
Device → Switchboard → Primary (wait ACK) → Relay ACK to device
```

No change from existing behavior.

#### Case 2: Mirroring Enabled, ACK from Primary (Default)

```
Device → Switchboard ─┬→ Primary (wait ACK) → Relay ACK to device
                      │
                      └→ Mirror (fire-and-forget)
```

#### Case 3: Mirroring Enabled, ACK from Mirror

```
Device → Switchboard ─┬→ Primary (fire-and-forget)
                      │
                      └→ Mirror (wait ACK) → Relay ACK to device
```

### Fire-and-Forget Implementation

For the non-ACK backend:

```go
func (h *PacketHandler) sendFireAndForget(packet []byte, targetIP, backendType string) {
    // Dial UDP
    conn, err := net.DialUDP("udp", nil, targetAddr)
    if err != nil {
        recordMetric(port, targetIP, "dial_failed", backendType, 0)
        return
    }
    defer conn.Close()
    
    // Write packet (no read)
    _, err = conn.Write(packet)
    if err != nil {
        recordMetric(port, targetIP, "write_failed", backendType, 0)
        return
    }
    
    // Success - packet sent
    recordMetric(port, targetIP, "sent", backendType, latencyMs)
}
```

**Key points:**
- No `SetReadDeadline` - we don't read
- No `Read()` call - fire-and-forget
- Record `"sent"` status (not `"success"`) to distinguish from ACK-verified

---

## Metrics

### Extended Measurement: `udp_relay`

| Tag | Type | Values | Description |
|-----|------|--------|-------------|
| `port` | string | "18172" | UDP port |
| `target` | string | "207.189.178.80" | Backend IP |
| `backend_type` | string | "primary", "mirror" | **NEW**: Which backend |
| `status` | string | success, sent, timeout, dial_failed, write_failed, read_failed | Result |
| `customer` | string | "acme-fleet" | Customer label |
| `device` | string | "calamplmd" | Device type |

| Field | Type | Description |
|-------|------|-------------|
| `latency_ms` | float | Operation latency |
| `count` | int | Always 1 |

### Status Semantics

| Status | Meaning |
|--------|---------|
| `success` | ACK received and verified (ACK backend only) |
| `sent` | Packet sent successfully, no ACK waited (fire-and-forget) |
| `timeout` | ACK backend didn't respond in time |
| `dial_failed` | Couldn't connect to backend |
| `write_failed` | Write to backend socket failed |
| `read_failed` | Read from ACK backend failed |

### Query Examples

```flux
// Primary backend health
from(bucket: "switchboard")
  |> filter(fn: (r) => r.backend_type == "primary")
  |> filter(fn: (r) => r.status == "success")

// Mirror send failures
from(bucket: "switchboard")
  |> filter(fn: (r) => r.backend_type == "mirror")
  |> filter(fn: (r) => r.status == "dial_failed" or r.status == "write_failed")

// Which backend is ACKing
from(bucket: "switchboard")
  |> filter(fn: (r) => r.status == "success")
  |> group(by: ["backend_type"])
```

---

## Logging

| Event | Level | When | Extra Fields |
|-------|-------|------|--------------|
| `mirror_enabled` | INFO | Startup, for each mirrored route | `port`, `primary_target`, `mirror_ip`, `ack_source` |
| `mirror_send_failed` | DEBUG | Fire-and-forget send fails | `port`, `target`, `error_type` |
| `mirror_config_invalid` | ERROR | Validation fails | `port`, `error` |

**No per-packet logging** - too noisy. Use metrics for observability.

---

## Error Handling

| Scenario | Action |
|----------|--------|
| Global mirror enabled, no mirror_ip anywhere | Exit(1), validation error |
| Route mirror_ip invalid IPv4 | Exit(1), validation error |
| Fire-and-forget dial fails | Record metric, continue (ACK flow unaffected) |
| Fire-and-forget write fails | Record metric, continue (ACK flow unaffected) |
| ACK backend fails (mirroring enabled) | Normal timeout/error handling, device retries |

**Critical:** Fire-and-forget failures MUST NOT block or delay the ACK flow.

---

## Testing Strategy

### Unit Tests

| Test | Verifies |
|------|----------|
| Config parsing with mirror fields | JSON parsing |
| Global defaults applied to routes | Resolution logic |
| Route overrides global | Priority order |
| Validation: mirror=true, no mirror_ip | Error |
| Validation: invalid mirror_ip | Error |

### Integration Tests

| Test | Verifies |
|------|----------|
| Mirror disabled | Existing behavior unchanged |
| Mirror enabled, ack from primary | Both backends get packet, ACK from primary |
| Mirror enabled, ack from mirror | Both backends get packet, ACK from mirror |
| Fire-and-forget failure | ACK flow continues, error metric recorded |

### Manual Testing

1. Send test payload to switchboard port
2. Both backends (primary + mirror) receive packet
3. Designated ACK backend sends response → device receives it
4. Check Kafka: both backends should have processed message
5. Flip `ack_from_mirror`, test again

---

## Performance Impact

### Resource Usage (per packet)

| Scenario | Goroutines | Network Calls | Metrics |
|----------|------------|---------------|---------|
| No mirror | 1 | 1 | 1 |
| With mirror | 2 | 2 | 2 |

**Latency impact:** 
- Goroutine spawn: ~1-5μs
- Fire-and-forget is async, doesn't block ACK flow
- **Net impact on device response time: <5μs**

---

## Implementation Files

| File | Changes |
|------|---------|
| `internal/config/routes.go` | Add Routes global fields, Route mirror fields, validation |
| `internal/config/config.go` | Add `MirrorIP` to AppConfig |
| `internal/metrics/influx.go` | Add `backend_type` parameter to RecordRelayMetric |
| `internal/relay/handler.go` | Add sendFireAndForget, update HandlePacketWithResult |
| `internal/relay/listener.go` | Pass globalMirrorIP to handler |

---

## Rollout Plan

1. **Code + test** - Implement and verify locally
2. **Deploy to dev** - Enable mirroring for 1-2 test routes
3. **Validate** - Check both backends receive, correct ACK source
4. **Production rollout:**
   - Enable mirror globally, `ack_from_mirror=false` (shadow mode)
   - Monitor for 48h
   - Flip selected routes to `ack_from_mirror=true`
   - After validation, change `target` to new backend, remove mirror

---

## Out of Scope

- Mirror target port != route port (always same port)
- Multiple mirror targets per route
- Mirror health checks / automatic failover
- Rate limiting / throttling per backend
- Packet transformation or inspection
- Waiting for ACKs from both backends

---

## References

- [Implementation Guide](implementation-guide.md) - Extension points
- [API Reference](api-reference.md) - Current handler interface
- [Engineering Standards](engineering-standards.md) - Concurrency patterns
