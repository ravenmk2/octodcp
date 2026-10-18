# OctoDCP

[![Test](https://github.com/ravenmk2/octodcp/actions/workflows/test.yml/badge.svg)](https://github.com/ravenmk2/octodcp/actions/workflows/test.yml) [![Release](https://img.shields.io/github/v/release/ravenmk2/octodcp)](https://github.com/ravenmk2/octodcp/releases) [![License](https://img.shields.io/github/license/ravenmk2/octodcp)](LICENSE)

Octopus-assisted Development & Collaboration Platform

## Quick Start

Prepare an empty MySQL 8.x database (8.0.16 or later) and a database user, then run:

```sh
docker run -d \
  --name octodcp \
  --restart unless-stopped \
  -p 8080:8080 \
  -e OCTODCP_PUBLIC_URL=http://localhost:8080 \
  -v octodcp-data:/app/data \
  ghcr.io/ravenmk2/octodcp:dev
```

Open [http://localhost:8080](http://localhost:8080) and complete the installation wizard.

For remote access, replace `localhost` in `OCTODCP_PUBLIC_URL` with your server IP or domain. Application data is persisted in the `octodcp-data` volume.
