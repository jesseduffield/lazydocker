#!/bin/bash

sudo apt-get update

sudo apt-get install -y \
    squashfs-tools \
    python3-pip \
    python3-apt \
    python3-debian \
    python3-pyelftools \
    python3-yaml \
    python3-tabulate \
    python3-jsonschema \
    python3-click \
    python3-pymacaroons \
    python3-simplejson \
    python3-progressbar \
    python3-requests-toolbelt \
    python3-requests-unixsocket

sudo pip3 install \
    petname \
    snapcraft

snapcraft --version
