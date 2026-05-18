# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

We take security vulnerabilities seriously. If you discover a security issue, please follow responsible disclosure practices.

### How to Report

1. **Do NOT open a public issue** for security vulnerabilities
2. Email the maintainers directly with:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

### What to Expect

- **Acknowledgment**: Within 48 hours of your report
- **Status Update**: Within 7 days with an assessment
- **Resolution**: Security patches are prioritized and released as quickly as possible

### Scope

The following are in scope for security reports:

- Authentication credential exposure
- WebSocket injection vulnerabilities
- Denial of service in client code
- Information disclosure

The following are out of scope:

- Polymarket API security issues (report to Polymarket directly)
- Issues in development dependencies
- Social engineering

## Security Best Practices

When using this library:

### Credential Management

```typescript
// ✅ Good: Use environment variables
const client = new ClobUserClient({
  apiKey: process.env.POLYMARKET_API_KEY!,
  secret: process.env.POLYMARKET_SECRET!,
  passphrase: process.env.POLYMARKET_PASSPHRASE!,
});

// ❌ Bad: Hardcoded credentials
const client = new ClobUserClient({
  apiKey: 'abc123',
  secret: 'secret',
  passphrase: 'pass',
});
```

### Transport Security

- Always use `wss://` (WebSocket Secure) in production
- Never use `ws://` with credentials
- The default URLs use WSS

### Logging

- Avoid logging full message payloads in production
- Never log credentials or authentication headers
- Use the `rawMessage` event carefully

## Acknowledgments

We appreciate security researchers who help keep this project safe. Contributors who report valid security issues will be acknowledged (with permission) in our security advisories.
