#!/usr/bin/env bash
# scripts/build-web.sh — build the frontend into web/dist.
#
# When web/package.json exists, the package manager follows the lockfile
# (pnpm / yarn / npm; GitHub runners ship Node, pnpm/yarn go through corepack).
# Otherwise a placeholder is written so go embed compiles before the frontend
# exists. To customize the frontend build (framework, output dir, extra
# steps), edit this one file — build.sh and the CI workflows all call it.
set -euo pipefail

if [[ -f web/package.json ]]; then
  cd web
  if [[ -f pnpm-lock.yaml ]]; then
    corepack enable
    pnpm install --frozen-lockfile
    pnpm build
  elif [[ -f yarn.lock ]]; then
    corepack enable
    yarn install --immutable
    yarn build
  elif [[ -f package-lock.json ]]; then
    npm ci
    npm run build
  else
    npm install
    npm run build
  fi
else
  mkdir -p web/dist
  printf '<!-- placeholder: run frontend build to replace -->\n' > web/dist/index.html
fi
