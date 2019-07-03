#!/bin/bash

GITHUB_LATEST_VERSION=$(curl -L -s -H 'Accept: application/json' https://github.com/jesseduffield/lazydocker/releases/latest | sed -e 's/.*"tag_name":"\([^"]*\)".*/\1/')
GITHUB_FILE="lazydocker_${GITHUB_LATEST_VERSION//v/}_$(uname -s)_$(uname -m | sed 's/aarch64/arm64/').tar.gz"
GITHUB_URL="https://github.com/jesseduffield/lazydocker/releases/download/${GITHUB_LATEST_VERSION}/${GITHUB_FILE}"

wget -O lazydocker.tar.gz $GITHUB_URL
tar xzvf lazydocker.tar.gz lazydocker
sudo mv -f lazydocker /usr/local/bin/
rm lazydocker.tar.gz
