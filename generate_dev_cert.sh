#!/usr/bin/env bash
set -euo pipefail
OUT_DIR="${1:-./certs}"
mkdir -p "$OUT_DIR"
openssl req -x509 -nodes -newkey rsa:2048 -days 825 \
  -keyout "$OUT_DIR/locallink.key" \
  -out "$OUT_DIR/locallink.crt" \
  -subj "/CN=LocalLink" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"
printf 'Generated development certificate in %s\n' "$OUT_DIR"
printf 'For a phone/LAN deployment, add the server LAN IP to the certificate SAN before trusting it on clients.\n'
