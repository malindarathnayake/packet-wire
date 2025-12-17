#!/bin/bash
# download-deploy.sh - Downloads Packet Wire deployment files

FILES=(
    "README.md|https://raw.githubusercontent.com/malindarathnayake/packet-wire/main/deploy/README.md"
    "dashboard-config.json|https://raw.githubusercontent.com/malindarathnayake/packet-wire/main/deploy/dashboard-config.json"
    "docker-compose.dashboard.yml|https://raw.githubusercontent.com/malindarathnayake/packet-wire/main/deploy/docker-compose.dashboard.yml"
    "docker-compose.udp-listener.yml|https://raw.githubusercontent.com/malindarathnayake/packet-wire/main/deploy/docker-compose.udp-listener.yml"
    "docker-compose.udp-sender.yml|https://raw.githubusercontent.com/malindarathnayake/packet-wire/main/deploy/docker-compose.udp-sender.yml"
    "env.example|https://raw.githubusercontent.com/malindarathnayake/packet-wire/main/deploy/env.example"
    "listener-config.json|https://raw.githubusercontent.com/malindarathnayake/packet-wire/main/deploy/listener-config.json"
)

echo ""
echo "Packet Wire - Deploy Files Downloader"
echo "======================================"
echo ""
echo "Where do you want to download the files?"
echo "  [1] Current folder ($(pwd))"
echo "  [2] Create 'deploy' subfolder"
echo ""
read -p "Enter choice (1 or 2): " choice

case $choice in
    1) TARGET_DIR="." ;;
    2) 
        TARGET_DIR="deploy"
        mkdir -p "$TARGET_DIR"
        echo "Created folder: $TARGET_DIR"
        ;;
    *)
        echo "Invalid choice. Exiting."
        exit 1
        ;;
esac

echo ""
echo "Downloading files..."

for item in "${FILES[@]}"; do
    name="${item%%|*}"
    url="${item#*|}"
    echo "  -> $name"
    curl -sL "$url" -o "$TARGET_DIR/$name"
done

echo ""
echo "✓ Download complete!"
echo ""
echo "=========================================="
echo "NEXT STEPS:"
echo "=========================================="
echo ""
echo "1. Copy the environment sample file:"
echo "   cp $TARGET_DIR/env.example $TARGET_DIR/.env"
echo ""
echo "2. Edit .env with your settings:"
echo "   nano $TARGET_DIR/.env"
echo ""
echo "3. Review and edit config files as needed:"
echo "   - listener-config.json"
echo "   - dashboard-config.json"
echo ""
read -p "Press Enter to exit..."