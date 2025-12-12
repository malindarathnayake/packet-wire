package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// TimestampPrefix marks packets with embedded timestamp for latency calculation
	TimestampPrefix    = "TIMESTAMP|"
	TimestampDelimiter = "|"
)

// Config holds sender configuration
type Config struct {
	ClientID              string `json:"client_id"`
	DashboardURL          string `json:"dashboard_url"`
	HeartbeatIntervalSecs int    `json:"heartbeat_interval_seconds"`
	SourceIP              string `json:"source_ip"`
	LocalIP               string `json:"local_ip"`
	ExternalIP            string `json:"external_ip"`
	Target                string `json:"target"`
	Passphrase            string `json:"passphrase"`
}

// Metrics tracks runtime statistics
type Metrics struct {
	PacketsSent  int64
	BytesSent    int64
	LastSendTime time.Time
	mu           sync.RWMutex
}

// MetricsPayload is sent to dashboard
type MetricsPayload struct {
	ClientID          string    `json:"client_id"`
	ClientType        string    `json:"client_type"`
	Timestamp         time.Time `json:"timestamp"`
	SourceIP          string    `json:"source_ip"`
	LocalIP           string    `json:"local_ip,omitempty"`
	ExternalIP        string    `json:"external_ip,omitempty"`
	DestinationIP     string    `json:"destination_ip"`
	DestinationPort   int       `json:"destination_port"`
	PacketsSent       int64     `json:"packets_sent"`
	BytesSent         int64     `json:"bytes_sent"`
	EncryptionEnabled bool      `json:"encryption_enabled"`
	LastSendTime      time.Time `json:"last_send_time,omitempty"`
}

var (
	metrics     = &Metrics{}
	config      Config
	shutdownCtx context.Context
	cancelFunc  context.CancelFunc
)

func main() {
	target := flag.String("target", "", "target UDP address in the form host:port (required)")
	message := flag.String("message", "", "message payload to send (JSON or any text/binary encoded as UTF-8)")
	filePath := flag.String("file", "", "path to a file to send in UDP chunks")
	chunkSize := flag.Int("chunk-size", 1024, "chunk size in bytes when sending a file")
	passphrase := flag.String("passphrase", "", "optional passphrase for AES-GCM encryption (must match listener)")

	// New flags for dashboard metrics
	clientID := flag.String("client-id", "", "unique client identifier for dashboard (e.g., sender01)")
	dashboardURL := flag.String("dashboard-url", "", "dashboard metrics endpoint URL (e.g., http://dashboard:8080/api/metrics)")
	sourceIP := flag.String("source-ip", "", "operator-configured source IP for dashboard correlation")
	localIP := flag.String("local-ip", "", "optional local/container IP")
	externalIP := flag.String("external-ip", "", "optional NAT'd external IP")
	heartbeatInterval := flag.Int("heartbeat-interval", 5, "seconds between dashboard heartbeats")
	configFile := flag.String("config", "", "path to JSON config file (overrides flags)")

	// Continuous mode flags
	continuous := flag.Bool("continuous", false, "send packets continuously at specified interval")
	intervalMs := flag.Int("interval-ms", 1000, "milliseconds between packets in continuous mode")
	count := flag.Int("count", 0, "number of packets to send (0 = unlimited in continuous mode)")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "  %s --target host:port [--message \"...\" | --file /path/to/file] [options]\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Flags:")
		flag.PrintDefaults()
		fmt.Fprintln(flag.CommandLine.Output(), `

Examples:
  # Send a single JSON message
  udp-sender --target 127.0.0.1:9000 --message '{"device_id":"test-1","status":"ok"}'

  # Send continuously with dashboard reporting
  udp-sender --target 192.168.92.22:9000 --message '{"test":true}' \
    --continuous --interval-ms 100 \
    --client-id sender01 --source-ip 192.168.100.10 \
    --dashboard-url http://dashboard:8080/api/metrics

  # Send with encryption and dashboard metrics
  udp-sender --target 127.0.0.1:9000 --message '{"secure":true}' \
    --passphrase "entrust-spearmint-defy" \
    --client-id sender01 --dashboard-url http://localhost:8080/api/metrics

  # Use config file
  udp-sender --config sender-config.json`)
	}

	flag.Parse()

	// Setup shutdown context
	shutdownCtx, cancelFunc = context.WithCancel(context.Background())
	defer cancelFunc()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("received signal %v, initiating shutdown", sig)
		cancelFunc()
	}()

	// Load config from file if specified
	if *configFile != "" {
		if err := loadConfigFromFile(*configFile); err != nil {
			log.Fatalf("failed to load config file: %v", err)
		}
		// Override with command line target if specified
		if *target != "" {
			config.Target = *target
		}
		if *passphrase != "" {
			config.Passphrase = *passphrase
		}
	} else {
		// Build config from flags
		config = Config{
			ClientID:              *clientID,
			DashboardURL:          *dashboardURL,
			HeartbeatIntervalSecs: *heartbeatInterval,
			SourceIP:              *sourceIP,
			LocalIP:               *localIP,
			ExternalIP:            *externalIP,
			Target:                *target,
			Passphrase:            *passphrase,
		}
	}

	if config.Target == "" {
		fmt.Fprintln(os.Stderr, "error: --target is required")
		flag.Usage()
		os.Exit(1)
	}

	hasMessage := *message != ""
	hasFile := *filePath != ""

	if hasMessage == hasFile {
		fmt.Fprintln(os.Stderr, "error: exactly one of --message or --file must be specified")
		flag.Usage()
		os.Exit(1)
	}

	if hasFile && *chunkSize <= 0 {
		fmt.Fprintln(os.Stderr, "error: --chunk-size must be a positive integer when using --file")
		os.Exit(1)
	}

	addr, err := net.ResolveUDPAddr("udp", config.Target)
	if err != nil {
		log.Fatalf("failed to resolve target address %q: %v", config.Target, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Fatalf("failed to dial UDP target %q: %v", config.Target, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("failed to close UDP connection: %v", err)
		}
	}()

	// Start dashboard heartbeat if configured
	var wg sync.WaitGroup
	if config.DashboardURL != "" && config.ClientID != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runHeartbeat(shutdownCtx, addr)
		}()
		log.Printf("dashboard heartbeat enabled: %s (client: %s)", config.DashboardURL, config.ClientID)
	}

	// Execute send operation
	if hasMessage {
		if *continuous {
			runContinuousSend(shutdownCtx, conn, config.Target, *message, config.Passphrase, *intervalMs, *count)
		} else {
			sendMessage(conn, config.Target, *message, config.Passphrase)
		}
	} else if hasFile {
		if err := sendFile(conn, config.Target, *filePath, config.Passphrase, *chunkSize); err != nil {
			log.Fatalf("failed to send file: %v", err)
		}
	}

	// Wait for heartbeat goroutine to finish
	cancelFunc()
	wg.Wait()
}

func loadConfigFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	log.Printf("loaded configuration from %s", path)
	return nil
}

func runContinuousSend(ctx context.Context, conn *net.UDPConn, target, message, passphrase string, intervalMs, count int) {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	sent := 0
	for {
		select {
		case <-ctx.Done():
			log.Printf("continuous send stopped: %d packets sent", sent)
			return
		case <-ticker.C:
			sendMessage(conn, target, message, passphrase)
			sent++
			if count > 0 && sent >= count {
				log.Printf("continuous send complete: %d packets sent", sent)
				return
			}
		}
	}
}

func sendMessage(conn *net.UDPConn, target, message, passphrase string) {
	// Prepend timestamp for latency calculation
	timestampedData := prependTimestamp([]byte(message))

	data := timestampedData
	if passphrase != "" {
		encrypted, err := encryptWithPassphrase(timestampedData, passphrase)
		if err != nil {
			log.Fatalf("failed to encrypt message: %v", err)
		}
		data = encrypted
	}

	n, err := conn.Write(data)
	if err != nil {
		log.Printf("failed to send message to %s: %v", target, err)
		return
	}

	// Update metrics
	atomic.AddInt64(&metrics.PacketsSent, 1)
	atomic.AddInt64(&metrics.BytesSent, int64(n))
	metrics.mu.Lock()
	metrics.LastSendTime = time.Now()
	metrics.mu.Unlock()

	log.Printf("sent %d bytes to %s", n, target)
}

func sendFile(conn *net.UDPConn, target, filePath, passphrase string, chunkSize int) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file %q: %w", filePath, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("failed to close file %q: %v", filePath, err)
		}
	}()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat file %q: %w", filePath, err)
	}

	log.Printf("sending file %q (%d bytes) to %s in chunks of %d bytes", filePath, info.Size(), target, chunkSize)

	// Send a small header first so the listener knows to create a file.
	headerPlain := fmt.Sprintf("FILE_START %s %d\n", filepath.Base(filePath), info.Size())
	headerData := prependTimestamp([]byte(headerPlain))
	if passphrase != "" {
		encryptedHeader, encErr := encryptWithPassphrase(headerData, passphrase)
		if encErr != nil {
			return fmt.Errorf("encrypt file header: %w", encErr)
		}
		headerData = encryptedHeader
	}

	if n, err := conn.Write(headerData); err != nil {
		return fmt.Errorf("send file header: %w", err)
	} else {
		atomic.AddInt64(&metrics.PacketsSent, 1)
		atomic.AddInt64(&metrics.BytesSent, int64(n))
	}
	log.Printf("sent file header for %q", filePath)

	buf := make([]byte, chunkSize)
	var totalSent int64
	var chunks int64

	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			chunk := prependTimestamp(buf[:n])
			if passphrase != "" {
				encryptedChunk, encErr := encryptWithPassphrase(chunk, passphrase)
				if encErr != nil {
					return fmt.Errorf("encrypt file chunk: %w", encErr)
				}
				chunk = encryptedChunk
			}

			written, writeErr := conn.Write(chunk)
			if writeErr != nil {
				return fmt.Errorf("write UDP packet: %w", writeErr)
			}

			atomic.AddInt64(&metrics.PacketsSent, 1)
			atomic.AddInt64(&metrics.BytesSent, int64(written))
			metrics.mu.Lock()
			metrics.LastSendTime = time.Now()
			metrics.mu.Unlock()

			totalSent += int64(n)
			chunks++

			log.Printf("sent chunk %d (%d bytes plain, total %d/%d)", chunks, n, totalSent, info.Size())
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read file: %w", readErr)
		}
	}

	log.Printf("completed sending file %q: %d bytes in %d chunks", filePath, totalSent, chunks)
	return nil
}

// prependTimestamp adds a timestamp prefix to the payload for latency measurement
func prependTimestamp(payload []byte) []byte {
	timestamp := time.Now().UnixNano()
	prefix := fmt.Sprintf("%s%d%s", TimestampPrefix, timestamp, TimestampDelimiter)
	return append([]byte(prefix), payload...)
}

func encryptWithPassphrase(plaintext []byte, passphrase string) ([]byte, error) {
	key := sha256.Sum256([]byte(passphrase))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	return append(nonce, ciphertext...), nil
}

// runHeartbeat sends periodic metrics to the dashboard
func runHeartbeat(ctx context.Context, addr *net.UDPAddr) {
	interval := time.Duration(config.HeartbeatIntervalSecs) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send initial heartbeat
	sendHeartbeat(addr)

	for {
		select {
		case <-ctx.Done():
			log.Println("heartbeat stopped")
			return
		case <-ticker.C:
			sendHeartbeat(addr)
		}
	}
}

func sendHeartbeat(addr *net.UDPAddr) {
	metrics.mu.RLock()
	lastSend := metrics.LastSendTime
	metrics.mu.RUnlock()

	payload := MetricsPayload{
		ClientID:          config.ClientID,
		ClientType:        "sender",
		Timestamp:         time.Now(),
		SourceIP:          config.SourceIP,
		LocalIP:           config.LocalIP,
		ExternalIP:        config.ExternalIP,
		DestinationIP:     addr.IP.String(),
		DestinationPort:   addr.Port,
		PacketsSent:       atomic.LoadInt64(&metrics.PacketsSent),
		BytesSent:         atomic.LoadInt64(&metrics.BytesSent),
		EncryptionEnabled: config.Passphrase != "",
		LastSendTime:      lastSend,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("failed to marshal metrics: %v", err)
		return
	}

	// Send with retry and exponential backoff
	if err := sendWithRetry(config.DashboardURL, data); err != nil {
		log.Printf("failed to send heartbeat: %v", err)
	}
}

// sendWithRetry sends HTTP POST with exponential backoff (per engineering standards)
func sendWithRetry(url string, data []byte) error {
	maxRetries := 3
	baseDelay := 5 * time.Second

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter
			delay := baseDelay * time.Duration(attempt)
			jitter := time.Duration(float64(delay) * 0.2 * (0.5 - float64(time.Now().UnixNano()%1000)/1000))
			time.Sleep(delay + jitter)
		}

		ctx, cancel := context.WithTimeout(shutdownCtx, 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("create request: %w", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		cancel()

		if err != nil {
			lastErr = fmt.Errorf("send request: %w", err)
			log.Printf("heartbeat attempt %d failed: %v", attempt+1, err)
			continue
		}

		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		log.Printf("heartbeat attempt %d returned %d", attempt+1, resp.StatusCode)
	}

	return fmt.Errorf("all %d retries failed: %w", maxRetries, lastErr)
}

// parseDestinationPort extracts port from target address
func parseDestinationPort(target string) int {
	_, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(portStr)
	return port
}
