#!/bin/bash

# Building the binary that is going to be released 
GOOS=linux GOARCH=amd64 go build -o general-service cmd/general-service/main.go  

