# UDP Switchboard - API Reference

Complete reference for all public types, functions, and methods in the UDP Switchboard codebase.

---

## Table of Contents

1. [Package: config](#package-config)
2. [Package: logging](#package-logging)
3. [Package: metrics](#package-metrics)
4. [Package: relay](#package-relay)

---

## Package: config

**Import:** `udp-switchboard/internal/config`

### Types

#### Config

Main application configuration structure.

```go
type Config struct {
    App     AppConfig     `yaml:"app"`
    Logging LoggingConfig `yaml:"logging"`
    Metrics MetricsConfig `yaml:"metrics"`
}
```

#### AppConfig

Core application settings.

```go
type AppConfig struct {
    BindIP                     string `yaml:"bind_ip"`
    ResponseTimeoutMs          int    `yaml:"response_timeout_ms"`
    ShutdownGracePeriodSeconds int    `yaml:"shutdown_grace_period_seconds"`
    RoutesFile                 string `yaml:"routes_file"`
}
```

| Field | Type | YAML Key | Description |
|-------|------|----------|-------------|
| `BindIP` | string | `bind_ip` | IP address to bind listeners (default: "0.0.0.0") |
| `ResponseTimeoutMs` | int | `response_timeout_ms` | Timeout waiting for backend ACK in milliseconds |
| `ShutdownGracePeriodSeconds` | int | `shutdown_grace_period_seconds` | Time to wait for in-flight requests on shutdown |
| `RoutesFile` | string | `routes_file` | Path to routes.json file |

#### LoggingConfig

Logging configuration.

```go
type LoggingConfig struct {
    Level string     `yaml:"level"`
    GELF  GELFConfig `yaml:"gelf"`
}
```

| Field | Type | YAML Key | Description |
|-------|------|----------|-------------|
| `Level` | string | `level` | Log level: debug, info, warn, error |
| `GELF` | GELFConfig | `gelf` | GELF server configuration |

#### GELFConfig

GELF (Graylog Extended Log Format) settings.

```go
type GELFConfig struct {
    Enabled  bool   `yaml:"enabled"`
    Host     string `yaml:"host"`
    Port     string `yaml:"port"`
    Facility string `yaml:"facility"`
}
```

| Field | Type | YAML Key | Env Override | Description |
|-------|------|----------|--------------|-------------|
| `Enabled` | bool | `enabled` | - | Enable/disable GELF logging |
| `Host` | string | `host` | `GELF_HOST` | Graylog server hostname |
| `Port` | string | `port` | `GELF_PORT` | Graylog server port |
| `Facility` | string | `facility` | - | Application identifier in logs |

#### MetricsConfig

Metrics configuration wrapper.

```go
type MetricsConfig struct {
    InfluxDB InfluxDBConfig `yaml:"influxdb"`
}
```

#### InfluxDBConfig

InfluxDB connection settings.

```go
type InfluxDBConfig struct {
    Enabled              bool   `yaml:"enabled"`
    URL                  string `yaml:"url"`
    Token                string `yaml:"token"`
    Org                  string `yaml:"org"`
    Bucket               string `yaml:"bucket"`
    FlushIntervalSeconds int    `yaml:"flush_interval_seconds"`
    InsecureSkipVerify   bool   `yaml:"insecure_skip_verify"`
}
```

| Field | Type | YAML Key | Env Override | Description |
|-------|------|----------|--------------|-------------|
| `Enabled` | bool | `enabled` | - | Enable/disable InfluxDB metrics |
| `URL` | string | `url` | `INFLUX_URL` | InfluxDB server URL |
| `Token` | string | `token` | `INFLUX_TOKEN` | Authentication token |
| `Org` | string | `org` | `INFLUX_ORG` | Organization name |
| `Bucket` | string | `bucket` | `INFLUX_BUCKET` | Bucket name for metrics |
| `FlushIntervalSeconds` | int | `flush_interval_seconds` | - | Interval between metric flushes |
| `InsecureSkipVerify` | bool | `insecure_skip_verify` | - | Skip TLS certificate verification |

#### Route

Single port-to-target mapping.

```go
type Route struct {
    Port     int    `json:"port"`
    Target   string `json:"target"`
    Customer string `json:"customer,omitempty"`
    Device   string `json:"Device,omitempty"`
}
```

| Field | Type | JSON Key | Required | Description |
|-------|------|----------|----------|-------------|
| `Port` | int | `port` | Yes | UDP port to listen on (1-65535) |
| `Target` | string | `target` | Yes | Backend IP to forward packets to |
| `Customer` | string | `customer` | No | Customer identifier for metrics |
| `Device` | string | `Device` | No | Device identifier for metrics |

#### Routes

Collection of routing rules.

```go
type Routes struct {
    Routes []Route `json:"routes"`
}
```

### Functions

#### LoadConfig

Load application configuration from YAML file with environment variable overlays.

```go
func LoadConfig(configPath string) (*Config, error)
```

**Parameters:**
- `configPath`: Path to YAML configuration file

**Returns:**
- `*Config`: Parsed and validated configuration
- `error`: Error if file not found, invalid YAML, or validation fails

**Environment Overrides Applied:**
- `LOG_LEVEL` → `logging.level`
- `GELF_HOST` → `logging.gelf.host`
- `GELF_PORT` → `logging.gelf.port`
- `INFLUX_URL` → `metrics.influxdb.url`
- `INFLUX_TOKEN` → `metrics.influxdb.token`
- `INFLUX_ORG` → `metrics.influxdb.org`
- `INFLUX_BUCKET` → `metrics.influxdb.bucket`

**Example:**
```go
config, err := config.LoadConfig("config/config.yaml")
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}
```

#### LoadRoutes

Load routing table from JSON file.

```go
func LoadRoutes(routesPath string) (*Routes, error)
```

**Parameters:**
- `routesPath`: Path to JSON routes file

**Returns:**
- `*Routes`: Parsed and validated routes
- `error`: Error if file not found, invalid JSON, or validation fails

**Validation Rules:**
- At least one route required
- Port must be 1-65535
- No duplicate ports
- Target must be valid IPv4 address

**Example:**
```go
routes, err := config.LoadRoutes("config/routes.json")
if err != nil {
    return fmt.Errorf("failed to load routes: %w", err)
}
```

### Methods

#### Routes.GetPortList

Get list of all configured ports.

```go
func (r *Routes) GetPortList() []int
```

**Returns:**
- `[]int`: Slice of all port numbers

#### Routes.GetRouteByPort

Look up route configuration by port number.

```go
func (r *Routes) GetRouteByPort(port int) (*Route, bool)
```

**Parameters:**
- `port`: Port number to look up

**Returns:**
- `*Route`: Route configuration if found
- `bool`: True if route exists

#### Routes.GetPortRange

Get minimum and maximum port numbers.

```go
func (r *Routes) GetPortRange() (int, int)
```

**Returns:**
- `int`: Minimum port number
- `int`: Maximum port number

---

## Package: logging

**Import:** `udp-switchboard/internal/logging`

### Types

#### Level

Log level enumeration.

```go
type Level int

const (
    LevelDebug Level = iota
    LevelInfo
    LevelWarn
    LevelError
)
```

| Constant | Value | GELF Code | Use Case |
|----------|-------|-----------|----------|
| `LevelDebug` | 0 | 7 | Per-packet tracing, diagnostics |
| `LevelInfo` | 1 | 6 | Startup, shutdown, configuration |
| `LevelWarn` | 2 | 4 | Degraded state, fallbacks |
| `LevelError` | 3 | 3 | Failures affecting functionality |

#### Logger

Structured logger with GELF support.

```go
type Logger struct {
    level      Level
    gelfWriter *gelf.UDPWriter
    facility   string
}
```

#### LogEntry

Structured log entry representation.

```go
type LogEntry struct {
    Message   string
    Level     Level
    Component string
    Extra     map[string]interface{}
}
```

| Field | Type | Description |
|-------|------|-------------|
| `Message` | string | Log message text |
| `Level` | Level | Log severity level |
| `Component` | string | Source component name |
| `Extra` | map[string]interface{} | Additional structured fields |

### Functions

#### NewLogger

Create a new logger with GELF support or stdout fallback.

```go
func NewLogger(level string, gelfHost, gelfPort, facility string, gelfEnabled bool) (*Logger, error)
```

**Parameters:**
- `level`: Log level (debug, info, warn, error)
- `gelfHost`: Graylog server hostname
- `gelfPort`: Graylog server port
- `facility`: Application identifier
- `gelfEnabled`: Enable GELF output

**Returns:**
- `*Logger`: Configured logger
- `error`: Error if invalid log level

**Behavior:**
- If GELF enabled and connection succeeds → GELF output
- If GELF enabled but connection fails → Log warning, use stdout
- If GELF disabled → stdout output

**Example:**
```go
logger, err := logging.NewLogger("info", "graylog.example.com", "12201", "udp-switchboard", true)
if err != nil {
    return err
}
defer logger.Close()
```

### Methods

#### Logger.Debug

Log a debug message.

```go
func (l *Logger) Debug(message string)
```

#### Logger.DebugWith

Log a debug message with extra fields.

```go
func (l *Logger) DebugWith(message, component string, extra map[string]interface{})
```

**Parameters:**
- `message`: Log message
- `component`: Source component (e.g., "relay", "listener")
- `extra`: Additional structured fields

**Example:**
```go
logger.DebugWith("Packet received", "listener", map[string]interface{}{
    "port":        18172,
    "client_addr": "192.168.1.100:54321",
    "size":        256,
})
```

#### Logger.Info

Log an info message.

```go
func (l *Logger) Info(message string)
```

#### Logger.InfoWith

Log an info message with extra fields.

```go
func (l *Logger) InfoWith(message, component string, extra map[string]interface{})
```

#### Logger.Warn

Log a warning message.

```go
func (l *Logger) Warn(message string)
```

#### Logger.WarnWith

Log a warning message with extra fields.

```go
func (l *Logger) WarnWith(message, component string, extra map[string]interface{})
```

#### Logger.Error

Log an error message.

```go
func (l *Logger) Error(message string)
```

#### Logger.ErrorWith

Log an error message with extra fields.

```go
func (l *Logger) ErrorWith(message, component string, extra map[string]interface{})
```

#### Logger.Close

Close the GELF writer.

```go
func (l *Logger) Close() error
```

### Event-Specific Methods

#### Logger.LogStartupComplete

Log successful application startup.

```go
func (l *Logger) LogStartupComplete(routesCount int, startupDurationMs int64)
```

**Parameters:**
- `routesCount`: Number of active routes
- `startupDurationMs`: Startup time in milliseconds

**Structured Fields:**
- `event_type`: "startup"
- `routes_count`: Number of routes
- `startup_duration_ms`: Duration

#### Logger.LogShutdownInitiated

Log shutdown start.

```go
func (l *Logger) LogShutdownInitiated(inFlightCount int)
```

**Parameters:**
- `inFlightCount`: Number of in-flight requests

**Structured Fields:**
- `event_type`: "shutdown"
- `in_flight_count`: Number of requests

#### Logger.LogShutdownComplete

Log successful shutdown.

```go
func (l *Logger) LogShutdownComplete(drainDurationMs int64)
```

**Parameters:**
- `drainDurationMs`: Time spent draining in milliseconds

**Structured Fields:**
- `event_type`: "shutdown"
- `drain_duration_ms`: Duration

#### Logger.LogRouteError

Log repeated failures to a backend.

```go
func (l *Logger) LogRouteError(port int, target string, errorCount int, windowSeconds int)
```

**Parameters:**
- `port`: Affected port
- `target`: Backend IP
- `errorCount`: Number of errors
- `windowSeconds`: Time window for errors

#### Logger.LogConfigError

Log configuration errors.

```go
func (l *Logger) LogConfigError(file, errorMsg string)
```

#### Logger.LogBindFailed

Log port bind failures.

```go
func (l *Logger) LogBindFailed(port int, errorMsg string)
```

---

## Package: metrics

**Import:** `udp-switchboard/internal/metrics`

### Types

#### Config

InfluxDB connection configuration.

```go
type Config struct {
    URL                  string
    Token                string
    Org                  string
    Bucket               string
    FlushIntervalSeconds int
    Enabled              bool
    InsecureSkipVerify   bool
}
```

#### MetricsWriter

Async buffered writer for InfluxDB.

```go
type MetricsWriter struct {
    client       influxdb2.Client
    writeAPI     api.WriteAPIBlocking
    enabled      bool
    org          string
    bucket       string
    flushTicker  *time.Ticker
    buffer       chan *Measurement
    stopChan     chan struct{}
    wg           sync.WaitGroup
    mu           sync.RWMutex
    connected    bool
    retryCount   int
    maxRetries   int
    baseDelay    time.Duration
}
```

#### Measurement

Single metric measurement.

```go
type Measurement struct {
    Name      string
    Tags      map[string]string
    Fields    map[string]interface{}
    Timestamp time.Time
}
```

| Field | Type | Description |
|-------|------|-------------|
| `Name` | string | Measurement name (e.g., "udp_relay") |
| `Tags` | map[string]string | Indexed tag values |
| `Fields` | map[string]interface{} | Metric values |
| `Timestamp` | time.Time | Measurement timestamp |

### Functions

#### NewMetricsWriter

Create a new metrics writer with buffering and retry logic.

```go
func NewMetricsWriter(config Config) (*MetricsWriter, error)
```

**Parameters:**
- `config`: InfluxDB connection configuration

**Returns:**
- `*MetricsWriter`: Configured metrics writer
- `error`: Error if config incomplete

**Behavior:**
- If `Enabled: false` → Returns disabled writer (no-op)
- Validates required fields: URL, Token, Org, Bucket
- Starts background writer goroutine
- Tests connection asynchronously

**Example:**
```go
writer, err := metrics.NewMetricsWriter(metrics.Config{
    URL:                  os.Getenv("INFLUX_URL"),
    Token:                os.Getenv("INFLUX_TOKEN"),
    Org:                  os.Getenv("INFLUX_ORG"),
    Bucket:               os.Getenv("INFLUX_BUCKET"),
    FlushIntervalSeconds: 10,
    Enabled:              true,
})
if err != nil {
    log.Printf("WARN: Continuing without metrics: %v", err)
}
```

### Methods

#### MetricsWriter.RecordRelayMetric

Record a UDP relay operation metric.

```go
func (w *MetricsWriter) RecordRelayMetric(port int, target, status, customer, device string, latencyMs float64)
```

**Parameters:**
- `port`: UDP port number
- `target`: Backend IP address
- `status`: Operation status (success, timeout, error)
- `customer`: Customer identifier (optional)
- `device`: Device identifier (optional)
- `latencyMs`: Operation latency in milliseconds

**InfluxDB Measurement:**
- Name: `udp_relay`
- Tags: `port`, `target`, `status`, `customer`, `device`
- Fields: `latency_ms`, `count`

**Example:**
```go
writer.RecordRelayMetric(18172, "207.189.178.80", "success", "acme-fleet", "device-001", 15.5)
```

#### MetricsWriter.RecordErrorMetric

Record an error metric.

```go
func (w *MetricsWriter) RecordErrorMetric(port int, target, errorType, customer, device string)
```

**Parameters:**
- `port`: UDP port number
- `target`: Backend IP address
- `errorType`: Error classification (timeout, dial_failed, write_failed, read_failed)
- `customer`: Customer identifier (optional)
- `device`: Device identifier (optional)

**InfluxDB Measurement:**
- Name: `udp_relay_errors`
- Tags: `port`, `target`, `error_type`, `customer`, `device`
- Fields: `count`

#### MetricsWriter.RecordStatusMetric

Record a status/heartbeat metric.

```go
func (w *MetricsWriter) RecordStatusMetric(routesActive int, uptimeSeconds int64, instance string)
```

**Parameters:**
- `routesActive`: Number of active routes
- `uptimeSeconds`: Application uptime in seconds
- `instance`: Instance identifier (hostname)

**InfluxDB Measurement:**
- Name: `udp_switchboard_status`
- Tags: `instance`
- Fields: `routes_active`, `uptime_seconds`

#### MetricsWriter.Flush

Force immediate write of buffered measurements.

```go
func (w *MetricsWriter) Flush()
```

**Behavior:**
- Drains current buffer
- Writes all measurements to InfluxDB
- Called automatically on shutdown

#### MetricsWriter.GetStats

Get metrics writer statistics.

```go
func (w *MetricsWriter) GetStats() map[string]interface{}
```

**Returns:**
- `map[string]interface{}`: Statistics including:
  - `enabled`: Whether writer is enabled
  - `connected`: Current connection status
  - `retry_count`: Consecutive failures
  - `buffer_length`: Current buffer size
  - `buffer_cap`: Buffer capacity

#### MetricsWriter.Close

Shut down the metrics writer.

```go
func (w *MetricsWriter) Close() error
```

**Behavior:**
1. Signal background writer to stop
2. Wait for writer to finish
3. Close InfluxDB client

---

## Package: relay

**Import:** `udp-switchboard/internal/relay`

### Types

#### PacketHandler

Per-packet relay handler.

```go
type PacketHandler struct {
    route           *config.Route
    responseTimeout time.Duration
    logger          *logging.Logger
    metricsWriter   *metrics.MetricsWriter
}
```

#### RelayResult

Result of a packet relay operation.

```go
type RelayResult struct {
    Success   bool
    ACK       []byte
    Error     error
    ErrorType string
    StartTime time.Time
}
```

| Field | Type | Description |
|-------|------|-------------|
| `Success` | bool | True if ACK received |
| `ACK` | []byte | ACK payload (if success) |
| `Error` | error | Error details (if failed) |
| `ErrorType` | string | Error classification |
| `StartTime` | time.Time | For latency calculation |

**ErrorType Values:**
- `timeout`: Backend response timeout
- `dial_failed`: Failed to connect to backend
- `write_failed`: Failed to send packet
- `read_failed`: Failed to receive ACK

#### Listener

UDP socket listener for a single port.

```go
type Listener struct {
    route           *config.Route
    socket          *net.UDPConn
    handler         *PacketHandler
    logger          *logging.Logger
    ctx             context.Context
    cancel          context.CancelFunc
    wg              sync.WaitGroup
    inFlightCounter int64
    bindIP          string
}
```

#### ListenerManager

Manager for all UDP listeners.

```go
type ListenerManager struct {
    listeners     []*Listener
    logger        *logging.Logger
    metricsWriter *metrics.MetricsWriter
    config        *config.Config
    routes        *config.Routes
    ctx           context.Context
    cancel        context.CancelFunc
    wg            sync.WaitGroup
}
```

### Functions

#### NewPacketHandler

Create a packet handler for a route.

```go
func NewPacketHandler(route *config.Route, responseTimeout time.Duration, logger *logging.Logger, metricsWriter *metrics.MetricsWriter) *PacketHandler
```

**Parameters:**
- `route`: Route configuration
- `responseTimeout`: ACK wait timeout
- `logger`: Logger instance
- `metricsWriter`: Metrics writer (can be nil)

**Returns:**
- `*PacketHandler`: Configured handler

#### NewListener

Create a UDP listener for a route.

```go
func NewListener(route *config.Route, bindIP string, responseTimeout time.Duration, logger *logging.Logger, metricsWriter *metrics.MetricsWriter) (*Listener, error)
```

**Parameters:**
- `route`: Route configuration
- `bindIP`: IP address to bind to
- `responseTimeout`: ACK wait timeout
- `logger`: Logger instance
- `metricsWriter`: Metrics writer (can be nil)

**Returns:**
- `*Listener`: Configured listener
- `error`: Error if creation fails

#### NewListenerManager

Create a listener manager.

```go
func NewListenerManager(config *config.Config, routes *config.Routes, logger *logging.Logger, metricsWriter *metrics.MetricsWriter) *ListenerManager
```

**Parameters:**
- `config`: Application configuration
- `routes`: Route configurations
- `logger`: Logger instance
- `metricsWriter`: Metrics writer (can be nil)

**Returns:**
- `*ListenerManager`: Configured manager

### Methods

#### PacketHandler.HandlePacket

Process a packet (fire-and-forget).

```go
func (h *PacketHandler) HandlePacket(packet []byte, clientAddr *net.UDPAddr)
```

**Parameters:**
- `packet`: UDP packet data
- `clientAddr`: Client address for ACK relay

**Note:** This method does not return the ACK. Use `HandlePacketWithResult` when you need the ACK.

#### PacketHandler.HandlePacketWithResult

Process a packet and return result.

```go
func (h *PacketHandler) HandlePacketWithResult(packet []byte, clientAddr *net.UDPAddr) *RelayResult
```

**Parameters:**
- `packet`: UDP packet data
- `clientAddr`: Client address

**Returns:**
- `*RelayResult`: Result containing ACK or error details

**Flow:**
1. Resolve target address
2. Dial backend UDP
3. Set response timeout deadline
4. Write packet to backend
5. Read ACK (blocks until deadline)
6. Record metrics
7. Return result

#### PacketHandler.GetRoute

Get route configuration.

```go
func (h *PacketHandler) GetRoute() *config.Route
```

#### Listener.Start

Bind socket and start listening.

```go
func (l *Listener) Start() error
```

**Returns:**
- `error`: Error if bind fails

**Behavior:**
- Binds UDP socket to `bindIP:route.Port`
- Starts listening goroutine
- Returns immediately after bind

#### Listener.Stop

Gracefully stop the listener.

```go
func (l *Listener) Stop()
```

**Behavior:**
1. Cancel context (signals shutdown)
2. Close socket (interrupts blocking read)
3. Wait for listen loop to exit

#### Listener.GetInFlightCount

Get number of in-flight requests.

```go
func (l *Listener) GetInFlightCount() int64
```

**Returns:**
- `int64`: Current in-flight count (atomic read)

#### Listener.GetPort

Get the listening port.

```go
func (l *Listener) GetPort() int
```

#### ListenerManager.StartAll

Create and start all listeners.

```go
func (lm *ListenerManager) StartAll() error
```

**Returns:**
- `error`: Error if any listener fails to start

**Behavior:**
1. Create listener for each route
2. Bind and start each listener
3. Start status reporter goroutine
4. On failure: stop all started listeners

#### ListenerManager.StopAll

Stop all listeners.

```go
func (lm *ListenerManager) StopAll()
```

**Behavior:**
1. Cancel manager context
2. Wait for status reporter
3. Stop all listeners

#### ListenerManager.GetTotalInFlightCount

Get total in-flight requests across all listeners.

```go
func (lm *ListenerManager) GetTotalInFlightCount() int64
```

**Returns:**
- `int64`: Sum of all listener in-flight counts

#### ListenerManager.GetListenerCount

Get number of active listeners.

```go
func (lm *ListenerManager) GetListenerCount() int
```

#### ListenerManager.GetPortList

Get list of all listening ports.

```go
func (lm *ListenerManager) GetPortList() []int
```

#### ListenerManager.WaitForGracefulShutdown

Wait for in-flight requests to complete.

```go
func (lm *ListenerManager) WaitForGracefulShutdown(maxWait time.Duration) time.Duration
```

**Parameters:**
- `maxWait`: Maximum time to wait

**Returns:**
- `time.Duration`: Actual time waited

**Behavior:**
- Polls in-flight count every 100ms
- Returns immediately when count reaches 0
- Returns after maxWait if timeout

---

## InfluxDB Measurements Reference

### udp_relay

Per-packet relay metrics.

| Tag | Type | Values | Description |
|-----|------|--------|-------------|
| `port` | string | "18172" | UDP port number |
| `target` | string | "207.189.178.80" | Backend IP |
| `status` | string | success, timeout, dial_failed, write_failed, read_failed | Operation result |
| `customer` | string | "acme-fleet" | Customer identifier |
| `device` | string | "device-001" | Device identifier |

| Field | Type | Description |
|-------|------|-------------|
| `latency_ms` | float | Operation latency |
| `count` | int | Always 1 (for aggregation) |

### udp_relay_errors

Error-specific metrics.

| Tag | Type | Values | Description |
|-----|------|--------|-------------|
| `port` | string | "18172" | UDP port number |
| `target` | string | "207.189.178.80" | Backend IP |
| `error_type` | string | timeout, dial_failed, write_failed, read_failed | Error classification |
| `customer` | string | "acme-fleet" | Customer identifier |
| `device` | string | "device-001" | Device identifier |

| Field | Type | Description |
|-------|------|-------------|
| `count` | int | Always 1 (for aggregation) |

### udp_switchboard_status

Application heartbeat metrics.

| Tag | Type | Description |
|-----|------|-------------|
| `instance` | string | Hostname or "udp-switchboard" |

| Field | Type | Description |
|-------|------|-------------|
| `routes_active` | int | Number of active routes |
| `uptime_seconds` | int | Application uptime |

---

## GELF Log Events Reference

### Standard Fields

All log entries include:
- `version`: "1.1"
- `host`: Hostname
- `short_message`: Log message
- `timestamp`: Unix timestamp
- `level`: GELF level (3-7)
- `facility`: Application name

### Custom Fields

| Field | Type | Description |
|-------|------|-------------|
| `_component` | string | Source component (main, relay, listener, config) |
| `_event_type` | string | Event category (startup, runtime, error, shutdown) |

### Event-Specific Fields

**startup_complete:**
- `_routes_count`: Number of routes
- `_startup_duration_ms`: Startup time

**shutdown_initiated:**
- `_in_flight_count`: Pending requests

**shutdown_complete:**
- `_drain_duration_ms`: Drain time

**route_error:**
- `_port`: Affected port
- `_target`: Backend IP
- `_error_count`: Error count
- `_window_seconds`: Time window

**config_error:**
- `_file`: Config file path
- `_error`: Error message

**bind_failed:**
- `_port`: Failed port
- `_error`: Error message
