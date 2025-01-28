#!/bin/bash

# Stop execution on error
set -e

# Run secrets.sh
./secrets.sh

# Execute specified hydra command with all arguments
exec hydra "$@"
