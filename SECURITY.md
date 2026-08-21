# Security policy

## Supported versions

Only the latest published stable Rule-Bot Client version receives security
fixes. The default branch can contain unreleased changes and is not a supported
release artifact.

## Reporting a vulnerability

Use this repository's private vulnerability reporting form. Do not open a
public Issue for a suspected vulnerability.

Include the affected version, deployment type, reachable entry point, expected
security boundary, and a minimal reproduction when available. Do not include
Mihomo controller credentials, Rule-Bot tokens, proxy credentials, private
endpoints, domain output, private network addresses, or complete configuration
files.

Operational questions and ordinary defects that do not cross a security or
privacy boundary can use GitHub Issues after sensitive values are removed.

## Security boundary

Rule-Bot Client rejects HTTP redirects, ignores environment proxy variables for
controller traffic, and exposes no listening port. These controls reduce
credential leakage and attack surface; they do not make an unencrypted remote
controller connection confidential. Use HTTPS or a trusted private network for
controller access.
