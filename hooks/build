#!/bin/bash

docker build --build-arg BUILD_DATE=`date -u +"%Y-%m-%dT%H:%M:%SZ"` \
             --build-arg VCS_REF=`git rev-parse --short HEAD` \
             --build-arg VERSION=`git describe --abbrev=0 --tag` \
             -t $IMAGE_NAME .
