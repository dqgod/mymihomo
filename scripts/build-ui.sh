#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/../web/zashboard"
# pnpm reads HTTPS_PROXY/HTTP_PROXY when a download proxy is required.
pnpm install --frozen-lockfile --ignore-scripts
pnpm type-check
pnpm build:no-fonts
