#!/bin/bash

# Building the binary that is going to be released 
GOOS=linux GOARCH=amd64 go build -o bin/nebula-on-premise-linux cmd/general-service/main.go  
   
# Exit script on any error
set -e

# Get the current date in the desired format
current_date=$(date +"%Y.%m.%d")

# Ensure that we are up to date with remote 
git pull origin main

# Get the current commit hash (shortened)
commit_hash=$(git rev-parse --short HEAD)

# Variable for the tag 

tag="v${current_date}-sha.${commit_hash}"

# Output the future release tag 

echo "The version tag is: $tag"

# Create and push the tag
git tag -a "$tag" -m "Release $tag"
git push origin "$tag"
echo "Tag $tag created and pushed successfully."
