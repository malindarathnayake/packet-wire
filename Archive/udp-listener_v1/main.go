package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr string `json:"listen_addr"`
	CaptureDir string `json:"capture_dir"`
	BufferSize int    `json:"buffer_size"`
	ReplyMode  bool   `json:"reply_mode"`
	IDName     string `json:"id_name"`
	AckMessage string `json:"ack_message"`
}

func main() {
	cfg := loadConfig()

	epoch := time.Now().Unix()
	filename := fmt.Sprintf("%d_udp_capture.csv", epoch)

	captureDir := cfg.CaptureDir
	outputPath := filepath.Join(captureDir, filename)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		log.Fatalf("failed to create capture directory %q: %v", filepath.Dir(outputPath), err)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		log.Fatalf("failed to create capture file %q: %v", outputPath, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("failed to close capture file: %v", err)
		}
	}()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"timestamp", "remote_addr", "size_bytes", "message"}); err != nil {
		log.Fatalf("failed to write CSV header: %v", err)
	}
	writer.Flush()

	addr, err := net.ResolveUDPAddr("udp", cfg.ListenAddr)
	if err != nil {
		log.Fatalf("failed to resolve UDP address %q: %v", cfg.ListenAddr, err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("failed to listen on UDP address %q: %v", cfg.ListenAddr, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("failed to close UDP listener: %v", err)
		}
	}()

	log.Printf("listening for UDP packets on %s, writing CSV to %s", cfg.ListenAddr, outputPath)
	if cfg.ReplyMode {
		log.Printf("reply mode enabled: will respond with %q", cfg.AckMessage)
	}

	buf := make([]byte, cfg.BufferSize)

	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("error reading from UDP: %v", err)
			continue
		}

		message := string(buf[:n])
		record := []string{
			time.Now().Format(time.RFC3339Nano),
			remote.String(),
			strconv.Itoa(n),
			message,
		}

		if err := writer.Write(record); err != nil {
			log.Printf("failed to write CSV record: %v", err)
			continue
		}
		writer.Flush()

		if cfg.ReplyMode {
			if _, err := conn.WriteToUDP([]byte(cfg.AckMessage), remote); err != nil {
				log.Printf("failed to send ACK to %s: %v", remote, err)
			}
		}
	}
}

func loadConfig() Config {
	configFile := os.Getenv("CONFIG_FILE")
	
	if configFile != "" {
		if data, err := os.ReadFile(configFile); err == nil {
			var cfg Config
			if err := json.Unmarshal(data, &cfg); err == nil {
				log.Printf("loaded configuration from %s", configFile)
				if cfg.BufferSize <= 0 {
					cfg.BufferSize = 2048
				}
				if cfg.AckMessage == "" {
					cfg.AckMessage = fmt.Sprintf("ACK-PLTEST_%s", cfg.IDName)
				}
				cfg.AckMessage = processPlaceholders(cfg.AckMessage, cfg.IDName)
				return cfg
			}
			log.Printf("failed to parse config file %q: %v, falling back to environment variables", configFile, err)
		} else {
			log.Printf("failed to read config file %q: %v, falling back to environment variables", configFile, err)
		}
	}

	bufSize := 2048
	if value := os.Getenv("UDP_BUFFER_SIZE"); value != "" {
		if size, err := strconv.Atoi(value); err == nil && size > 0 {
			bufSize = size
		}
	}

	idName := getEnv("ID_NAME", "UNKNOWN")
	ackMessage := getEnv("ACK_MESSAGE", "")
	if ackMessage == "" {
		ackMessage = fmt.Sprintf("ACK-PLTEST_%s", idName)
	}
	ackMessage = processPlaceholders(ackMessage, idName)

	return Config{
		ListenAddr: getEnv("UDP_LISTEN_ADDR", ":9000"),
		CaptureDir: getEnv("CAPTURE_DIR", "captures"),
		BufferSize: bufSize,
		ReplyMode:  getEnv("REPLY_MODE", "false") == "true",
		IDName:     idName,
		AckMessage: ackMessage,
	}
}

func processPlaceholders(message, idName string) string {
	message = strings.ReplaceAll(message, "{id_name}", idName)
	return message
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

