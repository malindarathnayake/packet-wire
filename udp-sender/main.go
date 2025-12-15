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

	// PWTestPrefix marks packets with sequence numbers for packet-wire test mode
	// Format: TEST|{seq_num}|{payload}
	PWTestPrefix = "TEST|"
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

// ConsoleSettings holds console mode configuration
type ConsoleSettings struct {
	Enabled        bool
	ReplyTimeoutMs int
	RawMode        bool
	AsyncMode      bool
	PWTestMode     bool // Packet-wire test mode: prepends TEST|{seq}| to messages
}

// PWTestState tracks sequence numbers for pw-test-mode
type PWTestState struct {
	SeqNum int64
}

// AutotestConfig holds autotest mode configuration
type AutotestConfig struct {
	Enabled        bool
	WarmupMins     int
	MaxPPS         int
	OutputFile     string
}

// AutotestStats tracks test results
type AutotestStats struct {
	PacketsSent    int64
	RepliesRecv    int64
	Timeouts       int64
	CurrentPPS     int
	MaxSuccessPPS  int
	BreakingPoint  int
	mu             sync.RWMutex
}

// TargetConfig represents a single target from targets file
type TargetConfig struct {
	Name       string `json:"name"`
	Address    string `json:"address"`
	Message    string `json:"message,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

// TargetsFile represents the JSON structure for multi-target input
type TargetsFile struct {
	Targets []TargetConfig `json:"targets"`
}

// TestResult represents results for JSON output
type TestResult struct {
	Timestamp       time.Time `json:"timestamp"`
	Target          string    `json:"target"`
	TargetName      string    `json:"target_name,omitempty"`
	TestType        string    `json:"test_type"`
	Duration        string    `json:"duration"`
	PacketsSent     int64     `json:"packets_sent"`
	RepliesReceived int64     `json:"replies_received"`
	Timeouts        int64     `json:"timeouts"`
	SuccessRate     float64   `json:"success_rate_percent"`
	MaxSuccessPPS   int       `json:"max_success_pps,omitempty"`
	BreakingPoint   int       `json:"breaking_point,omitempty"`
	RawMode         bool      `json:"raw_mode"`
	PWTestMode      bool      `json:"pw_test_mode"`
	Encrypted       bool      `json:"encrypted"`
}

// MultiTargetResults holds results for all targets
type MultiTargetResults struct {
	RunTimestamp time.Time    `json:"run_timestamp"`
	TotalTargets int          `json:"total_targets"`
	Results      []TestResult `json:"results"`
}

const MaxTargets = 10

var (
	metrics        = &Metrics{}
	config         Config
	consoleConfig  ConsoleSettings
	autotestCfg    AutotestConfig
	autotestStats  = &AutotestStats{}
	pwTestState    = &PWTestState{}
	shutdownCtx    context.Context
	cancelFunc     context.CancelFunc
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
	intervalMs := flag.Int("interval-ms", 0, "milliseconds between packets in continuous mode (use this OR --interval-sec)")
	intervalSec := flag.Float64("interval-sec", 0, "seconds between packets in continuous mode (use this OR --interval-ms)")
	count := flag.Int("count", 0, "number of packets to send (0 = unlimited in continuous mode)")
	confirm := flag.Bool("confirm", false, "confirm high interval values (>5s) to avoid accidental slow sends")

	// Console mode flags
	consoleMode := flag.Bool("console", false, "enable verbose console output showing packet details and replies")
	replyTimeout := flag.Int("reply-timeout-ms", 1000, "milliseconds to wait for reply after sending (console mode)")

	// Raw mode flag
	rawMode := flag.Bool("raw", false, "send message as-is without TIMESTAMP prefix (for compatibility with other systems)")

	// Packet-wire test mode flag
	pwTestMode := flag.Bool("pw-test-mode", false, "packet-wire test mode: prepends TEST|{seq}| to each message for drop analysis")

	// Autotest mode flags
	autotest := flag.Bool("autotest", false, "run automated stress test: warm-up then ramp-up to find breaking point")
	warmupDuration := flag.Int("warmup-mins", 5, "autotest warm-up duration in minutes (1 packet/10s)")
	maxPPS := flag.Int("max-pps", 100, "autotest maximum packets per second to test")
	outputFile := flag.String("output", "", "write test results to JSON file")

	// Multi-target mode
	targetsFile := flag.String("targets-file", "", "JSON file with multiple targets (max 10)")
	parallel := flag.Bool("parallel", false, "send to all targets simultaneously (stress test mode)")
	asyncMode := flag.Bool("async", false, "fire-and-forget mode: send without waiting for replies (true PPS stress test)")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "  %s --target host:port [--message \"...\" | --file /path/to/file] [options]\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Flags:")
		flag.PrintDefaults()
		fmt.Fprintln(flag.CommandLine.Output(), `

Examples:
  # Send a single JSON message
  udp-sender --target 127.0.0.1:9000 --message '{"device_id":"test-1","status":"ok"}'

  # Send continuously with dashboard reporting (100ms interval)
  udp-sender --target 192.168.92.22:9000 --message '{"test":true}' \
    --continuous --interval-ms 100 \
    --client-id sender01 --source-ip 192.168.100.10 \
    --dashboard-url http://dashboard:8080/api/metrics

  # Send every 2 seconds (auto-approved)
  udp-sender --target 192.168.92.22:9000 --message '{"heartbeat":true}' \
    --continuous --interval-sec 2

  # Send every 10 seconds (requires --confirm for slow intervals)
  udp-sender --target 192.168.92.22:9000 --message '{"slow":true}' \
    --continuous --interval-sec 10 --confirm

  # Console mode - show packet details and wait for replies
  udp-sender --target 127.0.0.1:9000 --message '{"ping":true}' \
    --continuous --interval-sec 2 --console

  # Send with encryption and dashboard metrics
  udp-sender --target 127.0.0.1:9000 --message '{"secure":true}' \
    --passphrase "entrust-spearmint-defy" \
    --client-id sender01 --dashboard-url http://localhost:8080/api/metrics

  # PW-Test mode - for drop analysis with sequence numbers
  udp-sender --target 127.0.0.1:9000 --message 'PING' \
    --continuous --interval-ms 100 --count 1000 --pw-test-mode --console

  # Full test with autotest and pw-test-mode
  udp-sender --target 127.0.0.1:9000 --message 'TEST' \
    --autotest --max-pps 50 --pw-test-mode --output results.json

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

	// Calculate effective interval in milliseconds
	effectiveIntervalMs := *intervalMs
	if *intervalSec > 0 {
		if *intervalMs > 0 {
			fmt.Fprintln(os.Stderr, "error: specify only one of --interval-ms or --interval-sec, not both")
			os.Exit(1)
		}
		effectiveIntervalMs = int(*intervalSec * 1000)
	}
	// Default to 1000ms if neither specified
	if effectiveIntervalMs <= 0 {
		effectiveIntervalMs = 1000
	}

	// Validate interval for continuous mode
	if *continuous && effectiveIntervalMs > 5000 {
		intervalDisplay := float64(effectiveIntervalMs) / 1000.0
		if !*confirm {
			fmt.Fprintf(os.Stderr, "warning: interval of %.1f seconds is unusually high\n", intervalDisplay)
			fmt.Fprintln(os.Stderr, "  - Intervals 1-2s: normal heartbeat/polling")
			fmt.Fprintln(os.Stderr, "  - Intervals >5s: may indicate a typo or misconfiguration")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Add --confirm flag to proceed with this interval, or adjust:")
			fmt.Fprintf(os.Stderr, "  --interval-sec %.1f --confirm\n", intervalDisplay)
			os.Exit(1)
		}
		log.Printf("confirmed: using %.1f second interval between packets", intervalDisplay)
	}

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

	// Skip target validation if using targets-file
	if config.Target == "" && *targetsFile == "" {
		fmt.Fprintln(os.Stderr, "error: --target or --targets-file is required")
		flag.Usage()
		os.Exit(1)
	}

	hasMessage := *message != ""
	hasFile := *filePath != ""

	// Skip message/file validation for targets-file mode (targets have their own messages)
	if *targetsFile == "" && hasMessage == hasFile {
		fmt.Fprintln(os.Stderr, "error: exactly one of --message or --file must be specified")
		flag.Usage()
		os.Exit(1)
	}

	if hasFile && *chunkSize <= 0 {
		fmt.Fprintln(os.Stderr, "error: --chunk-size must be a positive integer when using --file")
		os.Exit(1)
	}

	// Setup console mode
	consoleConfig = ConsoleSettings{
		Enabled:        *consoleMode,
		ReplyTimeoutMs: *replyTimeout,
		RawMode:        *rawMode,
		AsyncMode:      *asyncMode,
		PWTestMode:     *pwTestMode,
	}

	// Setup autotest mode
	autotestCfg = AutotestConfig{
		Enabled:    *autotest,
		WarmupMins: *warmupDuration,
		MaxPPS:     *maxPPS,
		OutputFile: *outputFile,
	}

	// Handle multi-target mode
	if *targetsFile != "" {
		runMultiTarget(shutdownCtx, *targetsFile, *message, *continuous, effectiveIntervalMs, *count, *parallel)
		return
	}

	if consoleConfig.Enabled && !autotestCfg.Enabled {
		printConsoleHeader()
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
	if autotestCfg.Enabled {
		if !hasMessage {
			fmt.Fprintln(os.Stderr, "error: --autotest requires --message to be specified")
			os.Exit(1)
		}
		runAutotest(shutdownCtx, conn, config.Target, *message, config.Passphrase)
	} else if hasMessage {
		if *continuous {
			runContinuousSend(shutdownCtx, conn, config.Target, *message, config.Passphrase, effectiveIntervalMs, *count)
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
	sendTime := time.Now()
	packetNum := atomic.LoadInt64(&metrics.PacketsSent) + 1

	// Prepare message data based on mode
	var messageData []byte
	if consoleConfig.RawMode {
		messageData = []byte(message)
	} else {
		messageData = prependTimestamp([]byte(message))
	}

	// Prepend TEST|{seq}| if pw-test-mode is enabled
	if consoleConfig.PWTestMode {
		seqNum := atomic.AddInt64(&pwTestState.SeqNum, 1)
		messageData = prependPWTestSeq(messageData, seqNum)
	}

	data := messageData
	encrypted := false
	if passphrase != "" {
		encryptedData, err := encryptWithPassphrase(messageData, passphrase)
		if err != nil {
			log.Fatalf("failed to encrypt message: %v", err)
		}
		data = encryptedData
		encrypted = true
	}

	n, err := conn.Write(data)
	if err != nil {
		if consoleConfig.Enabled {
			printConsoleSendError(packetNum, target, err)
		} else {
			log.Printf("failed to send message to %s: %v", target, err)
		}
		return
	}

	// Update metrics
	atomic.AddInt64(&metrics.PacketsSent, 1)
	atomic.AddInt64(&metrics.BytesSent, int64(n))
	metrics.mu.Lock()
	metrics.LastSendTime = time.Now()
	metrics.mu.Unlock()

	if consoleConfig.Enabled {
		printConsoleSent(packetNum, target, message, n, encrypted, sendTime)
		// Wait for reply
		waitForReply(conn, packetNum, sendTime, passphrase)
	} else {
		log.Printf("sent %d bytes to %s", n, target)
	}
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

// prependPWTestSeq adds a TEST|{seq}| prefix for packet-wire test mode
func prependPWTestSeq(payload []byte, seqNum int64) []byte {
	prefix := fmt.Sprintf("%s%d|", PWTestPrefix, seqNum)
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

// ============================================================================
// Console Mode Functions
// ============================================================================

func printConsoleHeader() {
	w := 66 // inner width
	fmt.Println()
	fmt.Println("╔" + repeatChar("═", w) + "╗")
	fmt.Println("║" + centerText("UDP SENDER - CONSOLE MODE", w) + "║")
	fmt.Println("╠" + repeatChar("═", w) + "╣")
	fmt.Println("║" + padRight(fmt.Sprintf("  Target: %s", config.Target), w) + "║")
	fmt.Println("║" + padRight(fmt.Sprintf("  Reply Timeout: %dms", consoleConfig.ReplyTimeoutMs), w) + "║")
	if config.Passphrase != "" {
		fmt.Println("║" + padRight("  Encryption: AES-GCM (enabled)", w) + "║")
	} else {
		fmt.Println("║" + padRight("  Encryption: None", w) + "║")
	}
	if consoleConfig.RawMode {
		fmt.Println("║" + padRight("  Raw Mode: ON (no timestamp prefix)", w) + "║")
	} else {
		fmt.Println("║" + padRight("  Raw Mode: OFF (timestamp prefix added)", w) + "║")
	}
	if consoleConfig.PWTestMode {
		fmt.Println("║" + padRight("  PW-Test Mode: ON (TEST|seq| prefix for drop analysis)", w) + "║")
	}
	fmt.Println("╚" + repeatChar("═", w) + "╝")
	fmt.Println()
}

func printConsoleSent(packetNum int64, target, message string, bytesSent int, encrypted bool, sendTime time.Time) {
	encStatus := "raw"
	if !consoleConfig.RawMode {
		encStatus = "timestamped"
	}
	if encrypted {
		encStatus += "+encrypted"
	}

	fmt.Println("┌─────────────────────────────────────────────────────────────────┐")
	fmt.Printf("│ PACKET #%-5d                                      [%s] │\n", packetNum, sendTime.Format("15:04:05.000"))
	fmt.Println("├─────────────────────────────────────────────────────────────────┤")
	fmt.Printf("│ → SENT to %s\n", target)
	fmt.Printf("│   Bytes: %d (%s)\n", bytesSent, encStatus)
	fmt.Println("│   Message:")

	// Show message preview (truncate if too long)
	msgPreview := message
	if len(msgPreview) > 60 {
		msgPreview = msgPreview[:57] + "..."
	}
	fmt.Printf("│   \"%s\"\n", msgPreview)
}

func printConsoleSendError(packetNum int64, target string, err error) {
	fmt.Println("┌─────────────────────────────────────────────────────────────────┐")
	fmt.Printf("│ PACKET #%-5d                                           [ERROR] │\n", packetNum)
	fmt.Println("├─────────────────────────────────────────────────────────────────┤")
	fmt.Printf("│ ✗ FAILED to send to %s\n", target)
	fmt.Printf("│   Error: %v\n", err)
	fmt.Println("└─────────────────────────────────────────────────────────────────┘")
	fmt.Println()
}

func waitForReply(conn *net.UDPConn, packetNum int64, sendTime time.Time, passphrase string) {
	// Set read deadline
	deadline := time.Now().Add(time.Duration(consoleConfig.ReplyTimeoutMs) * time.Millisecond)
	conn.SetReadDeadline(deadline)

	buf := make([]byte, 65535)
	n, remoteAddr, err := conn.ReadFromUDP(buf)

	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			fmt.Println("│")
			fmt.Printf("│ ← NO REPLY (timeout after %dms)\n", consoleConfig.ReplyTimeoutMs)
		} else {
			fmt.Println("│")
			fmt.Printf("│ ← READ ERROR: %v\n", err)
		}
		fmt.Println("└─────────────────────────────────────────────────────────────────┘")
		fmt.Println()
		return
	}

	// Calculate round-trip time
	rtt := time.Since(sendTime)
	replyData := buf[:n]

	// Try to decrypt if passphrase is set
	if passphrase != "" {
		decrypted, err := decryptWithPassphrase(replyData, passphrase)
		if err == nil {
			replyData = decrypted
		}
	}

	// Strip timestamp prefix if present
	replyData = stripTimestamp(replyData)

	fmt.Println("│")
	fmt.Printf("│ ← REPLY from %s\n", remoteAddr)
	fmt.Printf("│   Bytes: %d | RTT: %v\n", n, rtt.Round(time.Microsecond))
	fmt.Println("│   Content:")

	// Display reply based on whether it's printable ASCII
	if isPrintableASCII(replyData) {
		// Show as text
		replyStr := string(replyData)
		lines := splitLines(replyStr, 58)
		for _, line := range lines {
			fmt.Printf("│   \"%s\"\n", line)
		}
	} else {
		// Show as hex dump
		fmt.Println("│   [Binary/non-ASCII data]")
		hexDump := formatHexPreview(replyData, 32)
		fmt.Printf("│   %s\n", hexDump)
	}

	fmt.Println("└─────────────────────────────────────────────────────────────────┘")
	fmt.Println()
}

// isPrintableASCII checks if bytes are printable ASCII (including common whitespace)
func isPrintableASCII(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	for _, b := range data {
		// Allow printable ASCII (32-126), tab, newline, carriage return
		if b < 32 && b != '\t' && b != '\n' && b != '\r' {
			return false
		}
		if b > 126 {
			return false
		}
	}
	return true
}

// stripTimestamp removes the TIMESTAMP|...|  prefix if present
func stripTimestamp(data []byte) []byte {
	prefix := []byte(TimestampPrefix)
	if !bytes.HasPrefix(data, prefix) {
		return data
	}
	// Find the delimiter after timestamp
	rest := data[len(prefix):]
	delimIdx := bytes.Index(rest, []byte(TimestampDelimiter))
	if delimIdx == -1 {
		return data
	}
	return rest[delimIdx+1:]
}

// splitLines splits a string into lines of max length
func splitLines(s string, maxLen int) []string {
	var lines []string
	for len(s) > 0 {
		if len(s) <= maxLen {
			lines = append(lines, s)
			break
		}
		lines = append(lines, s[:maxLen])
		s = s[maxLen:]
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}
	return lines
}

// formatHexPreview returns a hex preview of data
func formatHexPreview(data []byte, maxBytes int) string {
	if len(data) > maxBytes {
		data = data[:maxBytes]
	}
	var hex string
	for i, b := range data {
		if i > 0 && i%8 == 0 {
			hex += " "
		}
		hex += fmt.Sprintf("%02x ", b)
	}
	if len(data) == maxBytes {
		hex += "..."
	}
	return hex
}

// decryptWithPassphrase decrypts AES-GCM encrypted data
func decryptWithPassphrase(ciphertext []byte, passphrase string) ([]byte, error) {
	key := sha256.Sum256([]byte(passphrase))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// ============================================================================
// Autotest Mode Functions
// ============================================================================

func runAutotest(ctx context.Context, conn *net.UDPConn, target, message, passphrase string) {
	startTime := time.Now()
	printAutotestHeader()

	// Phase 1: Warm-up (1 packet every 10 seconds)
	warmupPackets := autotestCfg.WarmupMins * 6 // 6 packets per minute at 10s intervals
	fmt.Printf("\n[PHASE 1] WARM-UP: %d minutes (%d packets at 1 per 10 seconds)\n", autotestCfg.WarmupMins, warmupPackets)
	fmt.Println("─────────────────────────────────────────────────────────────────────")

	if !runAutotestPhase(ctx, conn, target, message, passphrase, 10000, warmupPackets, "WARM-UP") {
		return // Cancelled
	}

	// Phase 2: Ramp-up testing
	fmt.Printf("\n[PHASE 2] RAMP-UP: Testing from 1 PPS to %d PPS\n", autotestCfg.MaxPPS)
	fmt.Println("─────────────────────────────────────────────────────────────────────")

	// PPS steps: 1, 2, 5, 10, 20, 50, 100, etc.
	ppsSteps := generatePPSSteps(autotestCfg.MaxPPS)

	for _, pps := range ppsSteps {
		select {
		case <-ctx.Done():
			printAutotestSummary()
			saveSingleTestResult(target, passphrase, startTime)
			return
		default:
		}

		intervalMs := 1000 / pps
		packetsToSend := pps * 30 // Test each level for 30 seconds

		fmt.Printf("\n► Testing %d PPS (interval: %dms, duration: 30s)\n", pps, intervalMs)

		autotestStats.mu.Lock()
		autotestStats.CurrentPPS = pps
		beforeSent := autotestStats.PacketsSent
		beforeTimeouts := autotestStats.Timeouts
		autotestStats.mu.Unlock()

		if !runAutotestPhase(ctx, conn, target, message, passphrase, intervalMs, packetsToSend, fmt.Sprintf("%d PPS", pps)) {
			break // Cancelled
		}

		// Calculate success rate for this phase
		autotestStats.mu.Lock()
		sent := autotestStats.PacketsSent - beforeSent
		timeouts := autotestStats.Timeouts - beforeTimeouts
		successRate := float64(sent-timeouts) / float64(sent) * 100
		autotestStats.mu.Unlock()

		fmt.Printf("  └─ Results: %d sent, %d timeouts, %.1f%% success\n", sent, timeouts, successRate)

		// Check if we've hit the breaking point (>20% failures)
		if successRate < 80 {
			autotestStats.mu.Lock()
			autotestStats.BreakingPoint = pps
			autotestStats.mu.Unlock()
			fmt.Printf("\n⚠ BREAKING POINT DETECTED at %d PPS (success rate dropped below 80%%)\n", pps)
			break
		} else {
			autotestStats.mu.Lock()
			autotestStats.MaxSuccessPPS = pps
			autotestStats.mu.Unlock()
		}
	}

	printAutotestSummary()
	saveSingleTestResult(target, passphrase, startTime)
}

func runAutotestPhase(ctx context.Context, conn *net.UDPConn, target, message, passphrase string, intervalMs, totalPackets int, phaseName string) bool {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	startTime := time.Now()
	sent := 0

	for sent < totalPackets {
		select {
		case <-ctx.Done():
			fmt.Printf("\n⚠ Autotest cancelled during %s phase\n", phaseName)
			return false
		case <-ticker.C:
			sendAutotestPacket(conn, target, message, passphrase)
			sent++

			// Progress indicator every 10 packets or every 30 seconds worth
			if sent%10 == 0 || sent == totalPackets {
				autotestStats.mu.RLock()
				replies := autotestStats.RepliesRecv
				timeouts := autotestStats.Timeouts
				autotestStats.mu.RUnlock()

				elapsed := time.Since(startTime).Round(time.Second)
				fmt.Printf("\r  Progress: %d/%d packets | Replies: %d | Timeouts: %d | Elapsed: %v    ",
					sent, totalPackets, replies, timeouts, elapsed)
			}
		}
	}
	fmt.Println() // New line after progress
	return true
}

func sendAutotestPacket(conn *net.UDPConn, target, message, passphrase string) bool {
	sendTime := time.Now()

	// Prepare message data
	var messageData []byte
	if consoleConfig.RawMode {
		messageData = []byte(message)
	} else {
		messageData = prependTimestamp([]byte(message))
	}

	// Prepend TEST|{seq}| if pw-test-mode is enabled
	if consoleConfig.PWTestMode {
		seqNum := atomic.AddInt64(&pwTestState.SeqNum, 1)
		messageData = prependPWTestSeq(messageData, seqNum)
	}

	data := messageData
	if passphrase != "" {
		encryptedData, err := encryptWithPassphrase(messageData, passphrase)
		if err != nil {
			log.Printf("encryption error: %v", err)
			return false
		}
		data = encryptedData
	}

	_, err := conn.Write(data)
	if err != nil {
		return false
	}

	autotestStats.mu.Lock()
	autotestStats.PacketsSent++
	autotestStats.mu.Unlock()

	// Wait for reply with short timeout (500ms for autotest)
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 65535)
	_, _, err = conn.ReadFromUDP(buf)

	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			autotestStats.mu.Lock()
			autotestStats.Timeouts++
			autotestStats.mu.Unlock()
			return false
		}
		return false
	}

	autotestStats.mu.Lock()
	autotestStats.RepliesRecv++
	autotestStats.mu.Unlock()

	_ = sendTime // Could use for RTT tracking
	return true
}

func generatePPSSteps(maxPPS int) []int {
	steps := []int{}
	sequence := []int{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000}

	for _, s := range sequence {
		if s <= maxPPS {
			steps = append(steps, s)
		}
	}

	// If maxPPS isn't in the sequence, add it
	if len(steps) > 0 && steps[len(steps)-1] != maxPPS {
		steps = append(steps, maxPPS)
	}

	return steps
}

func printAutotestHeader() {
	w := 70 // inner width
	fmt.Println()
	fmt.Println("╔" + repeatChar("═", w) + "╗")
	fmt.Println("║" + centerText("UDP SENDER - AUTOTEST MODE", w) + "║")
	fmt.Println("╠" + repeatChar("═", w) + "╣")
	fmt.Println("║" + padRight(fmt.Sprintf("  Target: %s", config.Target), w) + "║")
	fmt.Println("║" + padRight(fmt.Sprintf("  Warm-up: %d minutes (1 packet per 10 seconds)", autotestCfg.WarmupMins), w) + "║")
	fmt.Println("║" + padRight(fmt.Sprintf("  Max PPS: %d", autotestCfg.MaxPPS), w) + "║")
	if consoleConfig.RawMode {
		fmt.Println("║" + padRight("  Raw Mode: ON", w) + "║")
	}
	if consoleConfig.PWTestMode {
		fmt.Println("║" + padRight("  PW-Test Mode: ON (TEST|seq| prefix)", w) + "║")
	}
	fmt.Println("╠" + repeatChar("═", w) + "╣")
	fmt.Println("║" + padRight("  Press Ctrl+C to stop and see summary", w) + "║")
	fmt.Println("╚" + repeatChar("═", w) + "╝")
}

func printAutotestSummary() {
	autotestStats.mu.RLock()
	defer autotestStats.mu.RUnlock()

	successRate := float64(0)
	if autotestStats.PacketsSent > 0 {
		successRate = float64(autotestStats.PacketsSent-autotestStats.Timeouts) / float64(autotestStats.PacketsSent) * 100
	}

	w := 70 // inner width
	fmt.Println()
	fmt.Println("╔" + repeatChar("═", w) + "╗")
	fmt.Println("║" + centerText("AUTOTEST SUMMARY", w) + "║")
	fmt.Println("╠" + repeatChar("═", w) + "╣")
	fmt.Println("║" + padRight(fmt.Sprintf("  Total Packets Sent:    %d", autotestStats.PacketsSent), w) + "║")
	fmt.Println("║" + padRight(fmt.Sprintf("  Replies Received:      %d", autotestStats.RepliesRecv), w) + "║")
	fmt.Println("║" + padRight(fmt.Sprintf("  Timeouts:              %d", autotestStats.Timeouts), w) + "║")
	fmt.Println("║" + padRight(fmt.Sprintf("  Overall Success Rate:  %.1f%%", successRate), w) + "║")
	fmt.Println("╠" + repeatChar("═", w) + "╣")
	if autotestStats.MaxSuccessPPS > 0 {
		fmt.Println("║" + padRight(fmt.Sprintf("  Max Successful PPS:    %d", autotestStats.MaxSuccessPPS), w) + "║")
	}
	if autotestStats.BreakingPoint > 0 {
		fmt.Println("║" + padRight(fmt.Sprintf("  Breaking Point:        %d ⚠", autotestStats.BreakingPoint), w) + "║")
	} else {
		fmt.Println("║" + padRight("  Breaking Point:        Not reached", w) + "║")
	}
	fmt.Println("╚" + repeatChar("═", w) + "╝")
	fmt.Println()
}

// Helper functions for dynamic box formatting
func repeatChar(char string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += char
	}
	return result
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + repeatChar(" ", width-len(s))
}

func centerText(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	padding := (width - len(s)) / 2
	return repeatChar(" ", padding) + s + repeatChar(" ", width-len(s)-padding)
}

// ============================================================================
// Multi-Target Mode Functions
// ============================================================================

func runMultiTarget(ctx context.Context, targetsFilePath, defaultMessage string, continuous bool, intervalMs, count int, parallel bool) {
	// Load targets file
	targets, err := loadTargetsFile(targetsFilePath)
	if err != nil {
		log.Fatalf("failed to load targets file: %v", err)
	}

	if len(targets) == 0 {
		log.Fatal("no targets found in targets file")
	}

	if len(targets) > MaxTargets {
		log.Fatalf("too many targets: %d (max %d)", len(targets), MaxTargets)
	}

	mode := "SEQUENTIAL"
	if parallel {
		mode = "PARALLEL"
		if consoleConfig.AsyncMode {
			mode = "PARALLEL + ASYNC (TRUE STRESS TEST)"
		}
	}

	fmt.Println()
	fmt.Printf("╔════════════════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║%s║\n", centerText("UDP SENDER - MULTI-TARGET MODE", 72))
	fmt.Printf("╠════════════════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║%s║\n", padRight(fmt.Sprintf("  Targets: %d", len(targets)), 72))
	fmt.Printf("║%s║\n", padRight(fmt.Sprintf("  Mode: %s", mode), 72))
	fmt.Printf("║%s║\n", padRight(fmt.Sprintf("  Autotest: %v", autotestCfg.Enabled), 72))
	fmt.Printf("╚════════════════════════════════════════════════════════════════════════╝\n")

	startTime := time.Now()

	if parallel {
		runParallelTargets(ctx, targets, defaultMessage, continuous, intervalMs, count, startTime)
	} else {
		runSequentialTargets(ctx, targets, defaultMessage, continuous, intervalMs, count, startTime)
	}
}

// runParallelTargets sends to all targets simultaneously
func runParallelTargets(ctx context.Context, targets []TargetConfig, defaultMessage string, continuous bool, intervalMs, count int, startTime time.Time) {
	var wg sync.WaitGroup
	resultsChan := make(chan TestResult, len(targets))
	
	
	fmt.Printf("\n🚀 Starting %d parallel senders...\n", len(targets))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	for i, target := range targets {
		wg.Add(1)
		go func(idx int, t TargetConfig) {
			defer wg.Done()

			// Use target-specific message or default
			message := t.Message
			if message == "" {
				message = defaultMessage
			}
			if message == "" {
				message = "TEST"
			}

			passphrase := t.Passphrase
			if passphrase == "" {
				passphrase = config.Passphrase
			}

			addr, err := net.ResolveUDPAddr("udp", t.Address)
			if err != nil {
				fmt.Printf("  [%d] ✗ %s: resolve failed: %v\n", idx+1, t.Name, err)
				return
			}

			conn, err := net.DialUDP("udp", nil, addr)
			if err != nil {
				fmt.Printf("  [%d] ✗ %s: connect failed: %v\n", idx+1, t.Name, err)
				return
			}
			defer conn.Close()

			fmt.Printf("  [%d] ▶ %s (%s) started\n", idx+1, t.Name, t.Address)

			stats := &parallelStats{}
			targetStartTime := time.Now()

			if autotestCfg.Enabled {
				runParallelAutotest(ctx, conn, t.Address, message, passphrase, stats)
			} else if continuous {
				runParallelContinuous(ctx, conn, t.Address, message, passphrase, intervalMs, count, stats)
			} else {
				sendParallelMessage(conn, t.Address, message, passphrase, stats)
			}

			successRate := float64(0)
			if stats.PacketsSent > 0 {
				successRate = float64(stats.PacketsSent-stats.Timeouts) / float64(stats.PacketsSent) * 100
			}

			result := TestResult{
				Timestamp:       targetStartTime,
				Target:          t.Address,
				TargetName:      t.Name,
				TestType:        "parallel",
				Duration:        time.Since(targetStartTime).Round(time.Second).String(),
				PacketsSent:     stats.PacketsSent,
				RepliesReceived: stats.RepliesRecv,
				Timeouts:        stats.Timeouts,
				SuccessRate:     successRate,
				MaxSuccessPPS:   stats.MaxSuccessPPS,
				BreakingPoint:   stats.BreakingPoint,
				RawMode:         consoleConfig.RawMode,
				PWTestMode:      consoleConfig.PWTestMode,
				Encrypted:       passphrase != "",
			}

			fmt.Printf("  [%d] ✓ %s: %d sent, %.1f%% success\n", idx+1, t.Name, stats.PacketsSent, successRate)
			resultsChan <- result
		}(i, target)
	}

	// Wait for all to complete
	wg.Wait()
	close(resultsChan)

	// Collect results
	var allResults []TestResult
	for result := range resultsChan {
		allResults = append(allResults, result)
	}

	printMultiTargetSummary(allResults)
	saveMultiTargetResults(startTime, allResults)
}

// runSequentialTargets sends to targets one at a time
func runSequentialTargets(ctx context.Context, targets []TargetConfig, defaultMessage string, continuous bool, intervalMs, count int, startTime time.Time) {
	var allResults []TestResult

	for i, target := range targets {
		select {
		case <-ctx.Done():
			fmt.Println("\n⚠ Multi-target test cancelled")
			break
		default:
		}

		fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("  TARGET %d/%d: %s (%s)\n", i+1, len(targets), target.Name, target.Address)
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

		message := target.Message
		if message == "" {
			message = defaultMessage
		}
		if message == "" {
			message = "TEST"
		}

		passphrase := target.Passphrase
		if passphrase == "" {
			passphrase = config.Passphrase
		}

		addr, err := net.ResolveUDPAddr("udp", target.Address)
		if err != nil {
			log.Printf("  ✗ Failed to resolve %s: %v", target.Address, err)
			continue
		}

		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			log.Printf("  ✗ Failed to connect to %s: %v", target.Address, err)
			continue
		}

		autotestStats = &AutotestStats{}
		targetStartTime := time.Now()

		if autotestCfg.Enabled {
			runAutotest(ctx, conn, target.Address, message, passphrase)
		} else if continuous {
			runContinuousSend(ctx, conn, target.Address, message, passphrase, intervalMs, count)
		} else {
			sendMessage(conn, target.Address, message, passphrase)
		}

		conn.Close()
		result := buildTestResult(target, targetStartTime, passphrase != "")
		allResults = append(allResults, result)
	}

	printMultiTargetSummary(allResults)
	saveMultiTargetResults(startTime, allResults)
}

func saveMultiTargetResults(startTime time.Time, allResults []TestResult) {
	if autotestCfg.OutputFile != "" {
		multiResults := MultiTargetResults{
			RunTimestamp: startTime,
			TotalTargets: len(allResults),
			Results:      allResults,
		}
		if err := saveResultsToJSON(autotestCfg.OutputFile, multiResults); err != nil {
			log.Printf("failed to save results: %v", err)
		} else {
			fmt.Printf("\n✓ Results saved to: %s\n", autotestCfg.OutputFile)
		}
	}
}

// Parallel-safe versions of send functions
type parallelStats struct {
	PacketsSent   int64
	RepliesRecv   int64
	Timeouts      int64
	MaxSuccessPPS int
	BreakingPoint int
	stopListener  chan struct{}
}

func sendParallelMessage(conn *net.UDPConn, target, message, passphrase string, stats *parallelStats) {
	var messageData []byte
	if consoleConfig.RawMode {
		messageData = []byte(message)
	} else {
		messageData = prependTimestamp([]byte(message))
	}

	// Prepend TEST|{seq}| if pw-test-mode is enabled
	if consoleConfig.PWTestMode {
		seqNum := atomic.AddInt64(&pwTestState.SeqNum, 1)
		messageData = prependPWTestSeq(messageData, seqNum)
	}

	data := messageData
	if passphrase != "" {
		encryptedData, err := encryptWithPassphrase(messageData, passphrase)
		if err != nil {
			return
		}
		data = encryptedData
	}

	_, err := conn.Write(data)
	if err != nil {
		return
	}
	atomic.AddInt64(&stats.PacketsSent, 1)

	// In async mode, don't wait for reply (listener goroutine handles it)
	if consoleConfig.AsyncMode {
		return
	}

	// Sync mode: wait for reply
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 65535)
	_, _, err = conn.ReadFromUDP(buf)
	if err == nil {
		atomic.AddInt64(&stats.RepliesRecv, 1)
	} else {
		atomic.AddInt64(&stats.Timeouts, 1)
	}
}

// startReplyListener runs in background and counts incoming replies (async mode)
func startReplyListener(conn *net.UDPConn, stats *parallelStats) {
	buf := make([]byte, 65535)
	for {
		select {
		case <-stats.stopListener:
			return
		default:
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, _, err := conn.ReadFromUDP(buf)
			if err == nil {
				atomic.AddInt64(&stats.RepliesRecv, 1)
			}
			// Don't count timeouts in async mode - we just count what we receive
		}
	}
}

func runParallelContinuous(ctx context.Context, conn *net.UDPConn, target, message, passphrase string, intervalMs, count int, stats *parallelStats) {
	// Start reply listener in async mode
	if consoleConfig.AsyncMode {
		stats.stopListener = make(chan struct{})
		go startReplyListener(conn, stats)
		defer close(stats.stopListener)
	}

	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	sent := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendParallelMessage(conn, target, message, passphrase, stats)
			sent++
			if count > 0 && sent >= count {
				// In async mode, wait for trailing replies
				if consoleConfig.AsyncMode {
					time.Sleep(500 * time.Millisecond)
				}
				return
			}
		}
	}
}

func runParallelAutotest(ctx context.Context, conn *net.UDPConn, target, message, passphrase string, stats *parallelStats) {
	// Start reply listener in async mode
	if consoleConfig.AsyncMode {
		stats.stopListener = make(chan struct{})
		go startReplyListener(conn, stats)
		defer close(stats.stopListener)
	}

	// Simplified autotest for parallel mode - just ramp up
	ppsSteps := []int{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000}
	
	for _, pps := range ppsSteps {
		if pps > autotestCfg.MaxPPS {
			break
		}
		
		select {
		case <-ctx.Done():
			return
		default:
		}

		intervalMs := 1000 / pps
		if intervalMs < 1 {
			intervalMs = 1 // Minimum 1ms interval
		}
		packetsToSend := pps * 10 // 10 seconds per level

		beforeReplies := atomic.LoadInt64(&stats.RepliesRecv)
		beforeSent := atomic.LoadInt64(&stats.PacketsSent)

		ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
		sent := 0
		for sent < packetsToSend {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				sendParallelMessage(conn, target, message, passphrase, stats)
				sent++
			}
		}
		ticker.Stop()

		// In async mode, wait a bit for trailing replies
		if consoleConfig.AsyncMode {
			time.Sleep(500 * time.Millisecond)
		}

		// Check success rate
		sentThisPhase := atomic.LoadInt64(&stats.PacketsSent) - beforeSent
		repliesThisPhase := atomic.LoadInt64(&stats.RepliesRecv) - beforeReplies
		if sentThisPhase > 0 {
			successRate := float64(repliesThisPhase) / float64(sentThisPhase) * 100
			if successRate >= 80 {
				stats.MaxSuccessPPS = pps
			} else {
				stats.BreakingPoint = pps
				break
			}
		}
	}
}

func loadTargetsFile(path string) ([]TargetConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var tf TargetsFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	// Validate targets
	for i, t := range tf.Targets {
		if t.Address == "" {
			return nil, fmt.Errorf("target %d missing address", i+1)
		}
		if t.Name == "" {
			tf.Targets[i].Name = t.Address
		}
	}

	return tf.Targets, nil
}

func buildTestResult(target TargetConfig, startTime time.Time, encrypted bool) TestResult {
	autotestStats.mu.RLock()
	defer autotestStats.mu.RUnlock()

	successRate := float64(0)
	if autotestStats.PacketsSent > 0 {
		successRate = float64(autotestStats.PacketsSent-autotestStats.Timeouts) / float64(autotestStats.PacketsSent) * 100
	}

	testType := "single"
	if autotestCfg.Enabled {
		testType = "autotest"
	}

	return TestResult{
		Timestamp:       startTime,
		Target:          target.Address,
		TargetName:      target.Name,
		TestType:        testType,
		Duration:        time.Since(startTime).Round(time.Second).String(),
		PacketsSent:     autotestStats.PacketsSent,
		RepliesReceived: autotestStats.RepliesRecv,
		Timeouts:        autotestStats.Timeouts,
		SuccessRate:     successRate,
		MaxSuccessPPS:   autotestStats.MaxSuccessPPS,
		BreakingPoint:   autotestStats.BreakingPoint,
		RawMode:         consoleConfig.RawMode,
		PWTestMode:      consoleConfig.PWTestMode,
		Encrypted:       encrypted,
	}
}

func printMultiTargetSummary(results []TestResult) {
	w := 72
	fmt.Println()
	fmt.Println("╔" + repeatChar("═", w) + "╗")
	fmt.Println("║" + centerText("MULTI-TARGET SUMMARY", w) + "║")
	fmt.Println("╠" + repeatChar("═", w) + "╣")

	totalPackets := int64(0)
	totalReplies := int64(0)
	totalTimeouts := int64(0)

	for _, r := range results {
		status := "✓"
		if r.SuccessRate < 80 {
			status = "⚠"
		}
		if r.SuccessRate == 0 {
			status = "✗"
		}
		line := fmt.Sprintf("  %s %s: %.1f%% success (%d/%d)",
			status, r.TargetName, r.SuccessRate, r.RepliesReceived, r.PacketsSent)
		fmt.Println("║" + padRight(line, w) + "║")

		totalPackets += r.PacketsSent
		totalReplies += r.RepliesReceived
		totalTimeouts += r.Timeouts
	}

	fmt.Println("╠" + repeatChar("═", w) + "╣")
	overallSuccess := float64(0)
	if totalPackets > 0 {
		overallSuccess = float64(totalPackets-totalTimeouts) / float64(totalPackets) * 100
	}
	fmt.Println("║" + padRight(fmt.Sprintf("  Total: %d packets, %d replies, %.1f%% success",
		totalPackets, totalReplies, overallSuccess), w) + "║")
	fmt.Println("╚" + repeatChar("═", w) + "╝")
}

func saveResultsToJSON(path string, data interface{}) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}

	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// saveSingleTestResult saves results for single-target test
func saveSingleTestResult(target, passphrase string, startTime time.Time) {
	if autotestCfg.OutputFile == "" {
		return
	}

	result := TestResult{
		Timestamp:       startTime,
		Target:          target,
		TestType:        "autotest",
		Duration:        time.Since(startTime).Round(time.Second).String(),
		PacketsSent:     autotestStats.PacketsSent,
		RepliesReceived: autotestStats.RepliesRecv,
		Timeouts:        autotestStats.Timeouts,
		SuccessRate:     float64(autotestStats.PacketsSent-autotestStats.Timeouts) / float64(autotestStats.PacketsSent) * 100,
		MaxSuccessPPS:   autotestStats.MaxSuccessPPS,
		BreakingPoint:   autotestStats.BreakingPoint,
		RawMode:         consoleConfig.RawMode,
		PWTestMode:      consoleConfig.PWTestMode,
		Encrypted:       passphrase != "",
	}

	if err := saveResultsToJSON(autotestCfg.OutputFile, result); err != nil {
		log.Printf("failed to save results: %v", err)
	} else {
		fmt.Printf("\n✓ Results saved to: %s\n", autotestCfg.OutputFile)
	}
}
