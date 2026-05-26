# Security Policy

## Supported Use

This repository is intended for self-hosted deployments. Operators are responsible for securing runtime credentials, network exposure, and upstream proxy access.

## Reporting

Please do not open public issues for suspected security vulnerabilities.

Report them privately to the maintainers through a direct channel before disclosure.

## Minimum Hardening

- Replace the default admin username and password
- Replace the sample encryption key
- Avoid exposing Redis directly to the public network
- Run behind a firewall or trusted ingress where possible
