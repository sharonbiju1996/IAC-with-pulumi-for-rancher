#!/usr/bin/env bash
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
cd "$INFRA_DIR"
echo "URL      : $(pulumi stack output adminConsoleUrl --stack "$STACK")"
echo "Username : $(pulumi stack output adminUsername --stack "$STACK")"
echo "Password : $(pulumi stack output adminPassword --show-secrets --stack "$STACK")"
