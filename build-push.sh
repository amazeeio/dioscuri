#!/bin/bash
REPO=${2:-amazeeio}
TAG=${1:-latest}
echo "Creating image for $REPO/discouri:$TAG and pushing to docker hub"
make IMG=$REPO/dioscuri:$TAG docker-build
make IMG=$REPO/dioscuri:$TAG docker-push
