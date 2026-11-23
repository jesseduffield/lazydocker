#!/bin/bash

set -e  # Exit on any error

# Allow custom installation directory
DIR="${DIR:-"$HOME/.local/bin"}"

# Map architecture to GitHub's binary naming convention
ARCH=$(uname -m)
case "$ARCH" in
  i386|i686) ARCH="x86" ;;
  armv6*) ARCH="armv6" ;;
  armv7*) ARCH="armv7" ;;
  aarch64*) ARCH="arm64" ;;
  x86_64) ARCH="x86_64" ;;  # Explicitly handle common 64-bit
esac

OS=$(uname -s | tr '[:upper:]' '[:lower:]')  # Ensure lowercase (e.g., "darwin")

# Fetch latest version using GitHub API
GITHUB_LATEST_VERSION=$(curl -sfL https://api.github.com/repos/jesseduffield/lazydocker/releases/latest | grep -oP '"tag_name": "\K(.*)(?=")')
if [ -z "$GITHUB_LATEST_VERSION" ]; then
  echo "Failed to fetch latest version."
  exit 1
fi

# Build URL and download
GITHUB_FILE="lazydocker_${GITHUB_LATEST_VERSION//v/}_${OS}_${ARCH}.tar.gz"
GITHUB_URL="https://github.com/jesseduffield/lazydocker/releases/download/${GITHUB_LATEST_VERSION}/${GITHUB_FILE}"

echo "Downloading lazydocker $GITHUB_LATEST_VERSION for ${OS}_${ARCH}..."
if ! curl -sfL -o lazydocker.tar.gz "$GITHUB_URL"; then
  echo "Download failed. Check your OS/architecture or network connection."
  exit 1
fi

# Extract and install
tar -xzf lazydocker.tar.gz lazydocker
if [ ! -f "lazydocker" ]; then
  echo "Extraction failed. Corrupted download?"
  exit 1
fi

mkdir -p "$DIR"  # Ensure directory exists
install -v -m 755 lazydocker -t "$DIR"
rm -f lazydocker lazydocker.tar.gz

echo "Installed lazydocker to $DIR"
