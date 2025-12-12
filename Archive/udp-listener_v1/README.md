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

You can configure the listener using a JSON file. See `cmd/udp-listener/config.json` or `cmd/udp-listener/config.example.json` for examples.

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

Edit the values as needed, mount the file into the container, and set the `CONFIG_FILE` environment variable to its path.

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

## Building the Docker Image

From the repository root (where `go.mod` lives), run:

```bash
docker build -f cmd/udp-listener/Dockerfile -t udp-listener:latest .
```

Replace `YOUR_IMAGE_NAME` with something like `udp-listener:latest` or a full registry path (for example, `myregistry.example.com/udp-listener:latest`).

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

# Edit cmd/udp-listener/config.json to your preferences
docker run --rm -it \
  -p 9000:9000/udp \
  -v "$(pwd)/captures:/captures" \
  -v "$(pwd)/cmd/udp-listener/config.json:/config/config.json:ro" \
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

# Edit cmd\udp-listener\config.json to your preferences
docker run --rm -it `
  -p 9000:9000/udp `
  -v "${PWD}\captures:/captures" `
  -v "${PWD}\cmd\udp-listener\config.json:/config/config.json:ro" `
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

## Running on Windows (cmd.exe)

From the repo root in `cmd.exe`:

```cmd
mkdir captures

docker run --rm -it ^
  -p 9000:9000/udp ^
  -v %cd%\captures:/captures ^
  --name udp-listener ^
  YOUR_IMAGE_NAME
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

