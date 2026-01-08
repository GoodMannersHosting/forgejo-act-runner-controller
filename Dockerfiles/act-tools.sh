#!/bin/bash
set -e

# Install Docker CLI and dependencies
apt-get update 

# Docker and required tooling
apt-get install -y \
    docker.io \
    ca-certificates \
    curl 

# Tools
apt-get install -y \
    wget \
    jq \
    git \
    ssh \
    gawk \
    sudo \
    gnupg-agent \
    software-properties-common \
    apt-transport-https \
    zstd \
    zip unzip \
    xz-utils

apt-get install -y \
	python3.12-full

# Clean up
rm -rf /var/lib/apt/lists/*
