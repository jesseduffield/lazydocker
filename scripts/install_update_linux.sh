#!/bin/bash

# change the default destination directory to be executable as a command
DIR="${DIR:-"/usr/local/bin"}"

# check if requires sudo when the directory is not the default value
if [ ! -w "$DIR" ]; then
    if [ "$EUID" -ne 0 ]; then
        echo "Error: You need permissions to write in $DIR"
        echo "Run the script with sudo or define DIR as a directory with write permissions:"
        echo "  sudo $0"
        echo "  DIR=\"\$HOME/.local/bin\" $0"
        exit 1
    fi
fi

# map different architecture variations to the available binaries
ARCH=$(uname -m)
case $ARCH in
    i386|i686) ARCH=x86 ;;
    armv6*) ARCH=armv6 ;;
    armv7*) ARCH=armv7 ;;
    aarch64*) ARCH=arm64 ;;
esac

# prepare the download URL
GITHUB_LATEST_VERSION=$(curl -L -s -H 'Accept: application/json' https://github.com/jesseduffield/lazydocker/releases/latest | sed -e 's/.*"tag_name":"\([^"]*\)".*/\1/')
GITHUB_FILE="lazydocker_${GITHUB_LATEST_VERSION//v/}_$(uname -s)_${ARCH}.tar.gz"
GITHUB_URL="https://github.com/jesseduffield/lazydocker/releases/download/${GITHUB_LATEST_VERSION}/${GITHUB_FILE}"

# install/update the local binary
curl -L -o lazydocker.tar.gz $GITHUB_URL
tar xzvf lazydocker.tar.gz lazydocker

# create the directory if it doesn't exist, but requires sudo
if [ ! -d "$DIR" ]; then
    if [ "$EUID" -eq 0 ]; then
        mkdir -p "$DIR"
    else
        sudo mkdir -p "$DIR"
    fi
fi

# install with correct permissions
if [ "$EUID" -eq 0 ]; then
    install -Dm 755 lazydocker -t "$DIR"
else
    sudo install -Dm 755 lazydocker -t "$DIR"
fi

rm lazydocker lazydocker.tar.gz

echo "lazydocker installed successfully in $DIR"
