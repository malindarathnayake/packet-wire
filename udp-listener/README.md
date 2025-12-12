# UDP Listener Utility

This utility runs a small UDP listener that writes every received packet into a CSV file. The file name includes the Unix epoch at startup so each run creates a separate capture file.

Each CSV row contains:

- `timestamp` (RFC3339 with nanoseconds)
- `remote_addr` (IP:port of sender)
- `size_bytes` (payload size)
- `message` (raw payload as a string)

The capture file is named like:

```text
<epoch>_udp_capture.csv
```

For example: `1712680123_udp_capture.csv`.

The default capture directory is `/captures` inside the container.

## Configuration

### Option 1: JSON Configuration File (Recommended)

You can configure the listener using a JSON file. See `udp-listener/config.json` or `udp-listener/config.example.json` for examples.

Configuration structure:

```json
{
  "listen_addr": ":9000",
  "capture_dir": "/captures",
  "buffer_size": 2048,
  "reply_mode": false,
  "id_name": "UNKNOWN",
  "ack_message": "ACK-PLTEST_{id_name}"
}
```

Note: The `{id_name}` placeholder will be replaced with the actual value from `id_name` field.

Configuration fields:
- `listen_addr`: UDP listen address (e.g., `:9000`, `:9100`)
- `capture_dir`: Directory where CSV files are written
- `buffer_size`: Size of read buffer in bytes (default: 2048)
- `reply_mode`: Enable/disable ACK replies (`true`/`false`)
- `id_name`: Identifier name (used for default ACK message)
- `ack_message`: Custom ACK message to send back (defaults to `ACK-PLTEST_{id_name}` if not specified)
  - Supports placeholder: `{id_name}` will be replaced with the actual `id_name` value
  - Example: `"ack_message": "ACK-PLTEST_{id_name}"` becomes `"ACK-PLTEST_SERVER01"` if `id_name` is `"SERVER01"`
- `encryption_passphrase`: Optional passphrase for AES-256-GCM decryption (leave empty to disable)
- `file_receive_mode`: Enable/disable file reception mode (`true`/`false`)
- `file_receive_dir`: Directory where received files are stored
- `client_id`: Unique identifier for this listener instance (required for metrics)
- `dashboard_url`: Dashboard API endpoint for metrics (e.g., `http://dashboard:8080/api/metrics`)
- `heartbeat_interval_seconds`: Interval in seconds for sending metrics (default: 5)
- `listen_ip`: Listen IP address for metrics reporting (optional, auto-detected if empty)
- `local_ip`: Local IP address for metrics reporting (optional)
- `external_ip`: External IP address for metrics reporting (optional)

Edit the values as needed, mount the file into the container, and set the `CONFIG_FILE` environment variable to its path.

#### Metrics Configuration

To send runtime metrics to the dashboard, configure:
- `client_id`: Unique identifier for this listener (e.g., `"listener-01"`)
- `dashboard_url`: Full URL to the dashboard API (e.g., `"http://dashboard:8080/api/metrics"`)
- `heartbeat_interval_seconds`: How often to send metrics (default: 5 seconds)

When running with Docker Compose or in a Docker network, use the service name (e.g., `http://dashboard:8080/api/metrics`). For standalone containers, use the host IP or `host.docker.internal` on Windows/Mac.

Metrics sent include:
- Packets received & bytes received
- Average latency (if timestamps are included in packets)
- Encryption decode success/failure counts
- Last receive timestamp
- Detected source IP

### Option 2: Environment Variables

If no config file is provided, the listener falls back to environment variables:

- `CONFIG_FILE` – path to JSON configuration file (optional, takes precedence over env vars).
- `UDP_LISTEN_ADDR` – UDP listen address (default `:9000`)
  - Example: `:9100` to listen on port 9100 on all interfaces.
- `CAPTURE_DIR` – directory where CSV files are written (default `/captures` in the container).
- `UDP_BUFFER_SIZE` – size of the read buffer in bytes (optional, default `2048`).
- `REPLY_MODE` – enable reply mode to send ACK back to sender (default `false`, set to `true` to enable).
- `ID_NAME` – identifier name included in default ACK messages (default `UNKNOWN`).
- `ACK_MESSAGE` – custom ACK message to send back (defaults to `ACK-PLTEST_{ID_NAME}` if not specified).
- `ENCRYPTION_PASSPHRASE` – optional passphrase for AES-256-GCM decryption.
- `FILE_RECEIVE_MODE` – enable/disable file reception mode (default `false`).
- `FILE_RECEIVE_DIR` – directory where received files are stored.
- `CLIENT_ID` – unique identifier for this listener instance (required for metrics).
- `DASHBOARD_URL` – dashboard API endpoint for metrics (e.g., `http://dashboard:8080/api/metrics`).
- `HEARTBEAT_INTERVAL` – interval in seconds for sending metrics (default `5`).
- `LISTEN_IP` – listen IP address for metrics reporting (optional).
- `LOCAL_IP` – local IP address for metrics reporting (optional).
- `EXTERNAL_IP` – external IP address for metrics reporting (optional).

## Building the Docker Image

From the repository root, run:

```bash
docker build -f udp-listener/Dockerfile -t udp-listener:latest udp-listener
```

Or from within the `udp-listener` directory:

```bash
cd udp-listener
docker build -t udp-listener:latest .
```

Replace `udp-listener:latest` with a full registry path if pushing to a registry (for example, `myregistry.example.com/udp-listener:latest`).

## Pushing the Image

After logging in to your container registry (for example, Docker Hub, ECR, ACR, etc.), push:

```bash
docker push udp-listener:latest
```

## Running on Linux

### Using Config File (Recommended)

From the repo root:

```bash
mkdir -p captures

# Edit udp-listener/config.json to your preferences
docker run --rm -it \
  -p 9000:9000/udp \
  -v "$(pwd)/captures:/captures" \
  -v "$(pwd)/udp-listener/config.json:/config/config.json:ro" \
  -e CONFIG_FILE="/config/config.json" \
  --name udp-listener \
  udp-listener:latest
```

### Using Environment Variables

```bash
mkdir -p captures

docker run --rm -it \
  -p 9000:9000/udp \
  -v "$(pwd)/captures:/captures" \
  --name udp-listener \
  udp-listener:latest
```

Notes:

- UDP port `9000` on the host is mapped to `9000/udp` in the container.
- All CSV files are written into `./captures` on the host.
- To listen on another port, set `UDP_LISTEN_ADDR`, for example:

```bash
docker run --rm -it \
  -p 9100:9100/udp \
  -e UDP_LISTEN_ADDR=":9100" \
  -v "$(pwd)/captures:/captures" \
  --name udp-listener \
  udp-listener:latest
```

To enable reply mode with a custom ID:

```bash
docker run --rm -it \
  -p 9100:9100/udp \
  -e UDP_LISTEN_ADDR=":9100" \
  -e REPLY_MODE="true" \
  -e ID_NAME="MYAPP" \
  -v "$(pwd)/captures:/captures" \
  --name udp-listener \
  udp-listener:latest
```

This will respond to each received packet with `ACK-PLTEST_MYAPP`.

To use a completely custom ACK message:

```bash
docker run --rm -it \
  -p 9100:9100/udp \
  -e UDP_LISTEN_ADDR=":9100" \
  -e REPLY_MODE="true" \
  -e ACK_MESSAGE="RECEIVED_OK" \
  -v "$(pwd)/captures:/captures" \
  --name udp-listener \
  udp-listener:latest
```

This will respond with `RECEIVED_OK` instead of the default format.

## Running on Windows (PowerShell)

### Using Config File (Recommended)

From the repo root in a PowerShell session:

```powershell
New-Item -ItemType Directory -Force -Path .\captures | Out-Null

# Edit udp-listener\config.json to your preferences
docker run --rm -it `
  -p 9000:9000/udp `
  -v "${PWD}\captures:/captures" `
  -v "${PWD}\udp-listener\config.json:/config/config.json:ro" `
  -e CONFIG_FILE="/config/config.json" `
  --name udp-listener `
  udp-listener:latest
```

### Using Environment Variables

```powershell
New-Item -ItemType Directory -Force -Path .\captures | Out-Null

docker run --rm -it `
  -p 9000:9000/udp `
  -v "${PWD}\captures:/captures" `
  --name udp-listener `
  udp-listener:latest
```

To listen on a different port, for example 9100:

```powershell
docker run --rm -it `
  -p 9100:9100/udp `
  -e UDP_LISTEN_ADDR=":9100" `
  -v "${PWD}\captures:/captures" `
  --name udp-listener `
  udp-listener:latest
```

To enable reply mode with a custom ID:

```powershell
docker run --rm -it `
  -p 9100:9100/udp `
  -e UDP_LISTEN_ADDR=":9100" `
  -e REPLY_MODE="true" `
  -e ID_NAME="MYAPP" `
  -v "${PWD}\captures:/captures" `
  --name udp-listener `
  udp-listener:latest
```

This will respond to each received packet with `ACK-PLTEST_MYAPP`.

To use a completely custom ACK message:

```powershell
docker run --rm -it `
  -p 9100:9100/udp `
  -e UDP_LISTEN_ADDR=":9100" `
  -e REPLY_MODE="true" `
  -e ACK_MESSAGE="RECEIVED_OK" `
  -v "${PWD}\captures:/captures" `
  --name udp-listener `
  udp-listener:latest
```

This will respond with `RECEIVED_OK` instead of the default format.

### Enabling Metrics (Environment Variables)

To send metrics to the dashboard (requires dashboard to be running):

```powershell
docker run --rm -it `
  -p 9000:9000/udp `
  -e CLIENT_ID="listener-01" `
  -e DASHBOARD_URL="http://host.docker.internal:8080/api/metrics" `
  -e HEARTBEAT_INTERVAL="5" `
  -v "${PWD}\captures:/captures" `
  --name udp-listener `
  udp-listener:latest
```

**Note:** Use `host.docker.internal` on Windows/Mac to connect from container to host. If running in a Docker network with the dashboard, use the service name (e.g., `http://dashboard:8080/api/metrics`).

### Running with Docker Compose (Metrics Enabled)

When running with Docker Compose, the listener and dashboard can communicate via Docker network:

```yaml
services:
  dashboard:
    image: dashboard:latest
    ports:
      - "8080:8080"
    networks:
      - packet-wire-net
  
  udp-listener:
    image: udp-listener:latest
    ports:
      - "9000:9000/udp"
    environment:
      - CLIENT_ID=listener-01
      - DASHBOARD_URL=http://dashboard:8080/api/metrics
      - HEARTBEAT_INTERVAL=5
    volumes:
      - ./captures:/captures
    networks:
      - packet-wire-net

networks:
  packet-wire-net:
    driver: bridge
```

## Running on Windows (cmd.exe)

From the repo root in `cmd.exe`:

```cmd
mkdir captures

docker run --rm -it ^
  -p 9000:9000/udp ^
  -v %cd%\captures:/captures ^
  --name udp-listener ^
  udp-listener:latest
```

## Testing with a Sample Packet

Once the container is running and listening on `9000/udp`, you can send a test message like `PLTESTMESSAGE` from Linux:

```bash
echo -n "PLTESTMESSAGE" | nc -u -w1 127.0.0.1 9000
```

On Windows (PowerShell), if you have `nc` or `ncat` installed:

```powershell
echo -n "PLTESTMESSAGE" | ncat -u 127.0.0.1 9000
```

After sending packets, inspect the latest `*_udp_capture.csv` file in your `captures` directory on the host to verify that the messages are intact end-to-end.

## Quick Reference: Common Configurations

### Basic Listener on Port 9000 (PowerShell)

```powershell
docker run --rm -it `
  -p 9000:9000/udp `
  -v "${PWD}\captures:/captures" `
  --name udp-listener `
  udp-listener:latest
```

### With Metrics to Dashboard (PowerShell)

```powershell
docker run --rm -it `
  -p 9000:9000/udp `
  -e CLIENT_ID="listener-01" `
  -e DASHBOARD_URL="http://host.docker.internal:8080/api/metrics" `
  -v "${PWD}\captures:/captures" `
  --name udp-listener `
  udp-listener:latest
```

### With Config File (PowerShell)

```powershell
docker run --rm -it `
  -p 9000:9000/udp `
  -v "${PWD}\captures:/captures" `
  -v "${PWD}\udp-listener\config.json:/config/config.json:ro" `
  -e CONFIG_FILE="/config/config.json" `
  --name udp-listener `
  udp-listener:latest
```

### Complete Example: All Features Enabled (PowerShell)

```powershell
docker run --rm -it `
  -p 9000:9000/udp `
  -e REPLY_MODE="true" `
  -e CLIENT_ID="listener-01" `
  -e DASHBOARD_URL="http://host.docker.internal:8080/api/metrics" `
  -e HEARTBEAT_INTERVAL="5" `
  -v "${PWD}\captures:/captures" `
  --name udp-listener `
  udp-listener:latest
```

