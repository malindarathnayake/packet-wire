# UDP Sender

Simple CLI to send UDP messages or chunked file payloads to a target IP/port, useful for simulating IoT devices that talk to your UDP listener or switchboard.

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

