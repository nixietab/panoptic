#!/usr/bin/env bash
set -e

CERT_DIR="certs"
DAYS=3650
USERNAME="${1:-panoptic}"

mkdir -p "$CERT_DIR"

echo "Generating certificate for '$USERNAME'..."

openssl req -x509 -newkey rsa:2048 \
    -nodes \
    -days "$DAYS" \
    -keyout "$CERT_DIR/mumble.key" \
    -out "$CERT_DIR/mumble.crt" \
    -subj "/CN=$USERNAME"

echo ""
echo "Generated: $CERT_DIR/mumble.crt + $CERT_DIR/mumble.key"

CONFIG="config.toml"
if [ ! -f "$CONFIG" ]; then
    if [ -f "config.toml.example" ]; then
        cp config.toml.example "$CONFIG"
        echo "Created $CONFIG from example"
    else
        echo "No config.toml found, skipping config update"
        exit 0
    fi
fi

sed -i 's|^cert_file = .*|cert_file = "certs/mumble.crt"|' "$CONFIG"
sed -i 's|^key_file = .*|key_file = "certs/mumble.key"|' "$CONFIG"

echo "Updated $CONFIG with certificate paths"
