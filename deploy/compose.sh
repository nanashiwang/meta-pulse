#!/usr/bin/env bash
set -Eeuo pipefail
source "$(dirname "$0")/lib.sh"
require_existing_env_file
validate_infrastructure_modes
compose "$@"
