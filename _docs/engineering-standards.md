# Engineering Standards Reference

Reference document for implementation patterns, concurrency, error handling, and code review standards. Consult when writing specs or reviewing implementations.

---

## Baseline Requirements

These are NOT optional and should NOT appear in "Out of Scope":

- Retry/reconnection for external dependencies (exponential backoff)
- Graceful shutdown on SIGTERM/SIGINT with coordinated thread shutdown
- Structured logging (logging module, not print)
- Environment variable overrides for config
- Health checks for critical dependencies at startup
- Exceptions for errors, exit codes only at main() entry point
- Proper synchronization for shared state
- Bounded queues for producer/consumer patterns

---

## Error Propagation

Validation and startup code throws exceptions, never calls exit directly. Only main() entry point catches and converts to exit codes.

**Why:** Enables testability, proper cleanup (finally/using/try-with-resources), and composed systems.

| Language | Throw | Don't Do |
|----------|-------|----------|
| Python | `raise ConfigError("message")` | `sys.exit(1)` |
| C# | `throw new ConfigurationException("message")` | `Environment.Exit(1)` |
| Java | `throw new ConfigException("message")` | `System.exit(1)` |

---

## Concurrency Patterns

### 1. Identify Shared State First

| State | Writers | Readers | Pattern |
|-------|---------|---------|---------|
| metrics_buffer | worker threads | Prometheus handler | Producer/Consumer |
| config | startup only | all threads | Immutable after init |
| connection_pool | all threads | all threads | Resource Pool |
| last_run_status | scheduler | health endpoint | Reader/Writer |

### 2. Synchronization Primitive Selection

| Scenario | Python | C# | Java |
|----------|--------|-----|------|
| Simple mutual exclusion | `threading.Lock` | `lock (obj)` | `synchronized` |
| Reentrant (same thread) | `threading.RLock` | `lock (obj)` | `ReentrantLock` |
| Read-heavy, rare writes | Use Lock | `ReaderWriterLockSlim` | `ReadWriteLock` |
| Producer/consumer queue | `queue.Queue` | `BlockingCollection` | `BlockingQueue` |
| Thread-safe dict | Lock + dict | `ConcurrentDictionary` | `ConcurrentHashMap` |
| One-time init flag | `threading.Event` | `Lazy<T>` | `CountDownLatch` |
| Limit concurrent access | `threading.Semaphore` | `SemaphoreSlim` | `Semaphore` |
| Coordinated shutdown | `threading.Event` | `CancellationToken` | `volatile boolean` |

### 3. Lock Discipline

- **Lock ordering:** Always acquire multiple locks in consistent, documented order
- **Lock scope:** Minimal - never hold locks during I/O, network calls, or blocking operations
- **Lock granularity:** Prefer fine-grained (per-resource) over coarse (global) locks
- **No lock-then-block:** Never await/sleep/block while holding a lock
- **Timeout on acquisition:** Use tryLock with timeout for deadlock prevention in complex scenarios

### 4. Pattern: Producer/Consumer

**Python:**
```python
from queue import Queue, Empty

metrics_queue = Queue(maxsize=1000)  # Bounded

# Producer
metrics_queue.put(item, timeout=1)

# Consumer
while not shutdown_event.is_set():
    try:
        item = metrics_queue.get(timeout=1)
        process(item)
    except Empty:
        continue
```

**C#:**
```csharp
private readonly BlockingCollection<Metric> _queue = new(boundedCapacity: 1000);

// Producer
_queue.Add(item, cancellationToken);

// Consumer
foreach (var item in _queue.GetConsumingEnumerable(cancellationToken))
    Process(item);
```

### 5. Pattern: Reader/Writer

**Python:**
```python
_status_lock = threading.Lock()
_last_status = {}

def update_status(key, value):
    with _status_lock:
        _last_status[key] = value  # Fast, no I/O inside lock

def get_status():
    with _status_lock:
        return dict(_last_status)  # Return copy, not reference
```

**C#:**
```csharp
private readonly ReaderWriterLockSlim _lock = new();
private readonly Dictionary<string, object> _status = new();

public void UpdateStatus(string key, object value) {
    _lock.EnterWriteLock();
    try { _status[key] = value; }
    finally { _lock.ExitWriteLock(); }
}

public IDictionary<string, object> GetStatus() {
    _lock.EnterReadLock();
    try { return new Dictionary<string, object>(_status); }
    finally { _lock.ExitReadLock(); }
}
```

### 6. Pattern: Coordinated Shutdown

**Python:**
```python
shutdown_event = threading.Event()

# Worker thread
while not shutdown_event.is_set():
    try:
        item = queue.get(timeout=1)
        process(item)
    except Empty:
        continue

# Main thread shutdown
shutdown_event.set()
worker_thread.join(timeout=30)
if worker_thread.is_alive():
    logger.warning("Worker did not complete gracefully")
```

**C#:**
```csharp
private readonly CancellationTokenSource _cts = new();

// Worker
while (!_cts.Token.IsCancellationRequested) {
    if (_queue.TryTake(out var item, TimeSpan.FromSeconds(1), _cts.Token))
        Process(item);
}

// Shutdown
_cts.Cancel();
if (!workerTask.Wait(TimeSpan.FromSeconds(30)))
    _logger.Warn("Worker did not complete gracefully");
```

---

## Anti-Patterns to Flag in Review

| Anti-Pattern | Why It's Bad |
|--------------|--------------|
| Lock held during network/disk I/O | Blocks other threads, destroys throughput |
| Nested locks without documented ordering | Deadlock risk |
| Unbounded queue | Memory exhaustion under load |
| Bare shared variable without synchronization | Race conditions, torn reads |
| `Thread.Abort()` / `thread.interrupt()` for shutdown | Corrupts state, skips cleanup |
| Double-checked locking without volatile | Broken on most platforms |
| Returning internal collection reference | Caller can mutate your state |
| `sys.exit()` / `Environment.Exit()` in library code | Untestable, skips cleanup |
| `print()` instead of logging | No levels, no structure, no routing |
| Fixed retry delay | Thundering herd, no backoff |

---

## Retry/Backoff Standards

```
Attempts: 3 (configurable)
Backoff: Exponential with jitter
  - Attempt 1: immediate
  - Attempt 2: 5s + random(0-1s)
  - Attempt 3: 10s + random(0-2s)
  
After exhausted retries:
  - Startup: raise exception (let main convert to exit code)
  - Runtime: log error, increment failure counter, continue to next cycle
  
Connection recreation:
  - After N consecutive failures, destroy and recreate connection/pool
```

---

## Logging Standards

```
Library: logging module (Python), ILogger (C#), SLF4J (Java)
Format: %(asctime)s - %(name)s - %(levelname)s - %(message)s
Level control: LOG_LEVEL env var, default INFO

Structured fields for GELF:
  - _event_type: startup, runtime, error, shutdown
  - _component: validator, scheduler, consumer, etc.
  - _duration_ms: for timed operations

Never log:
  - Credentials or tokens
  - Full stack traces at INFO (use DEBUG)
  - Per-message logs in high-throughput loops (aggregate)
```

---

## HTTP Server Standards

For metrics/health endpoints alongside main processing:

| Language | Use | Don't Use |
|----------|-----|-----------|
| Python | `ThreadingHTTPServer` | `HTTPServer` (single-threaded) |
| C# | Kestrel / `WebApplication` | - |
| Java | Jetty / Undertow | Single-threaded `HttpServer` |

- Bind to `0.0.0.0` for container accessibility
- Suppress default access logging in handler (noisy)
- Health endpoint must not make expensive calls per request

---

## Code Review Checklist

**Core:**
- [ ] Library choices match spec's Implementation Directives
- [ ] Error handling matches the matrix (detection, action, recovery)
- [ ] Exceptions raised for errors, exit codes only in main()
- [ ] Logging uses proper module with configurable level
- [ ] No credentials in code or config files

**Lifecycle:**
- [ ] SIGTERM triggers graceful shutdown
- [ ] Shutdown event/token coordinates threads
- [ ] Join/wait with timeout on worker threads

**Concurrency:**
- [ ] Shared state identified and protected
- [ ] No bare shared variables
- [ ] No I/O while holding locks
- [ ] Bounded queues with explicit max size
- [ ] Returns copies of collections, not references

**Resilience:**
- [ ] Retry with exponential backoff for external deps
- [ ] Connection recreation after persistent failures
