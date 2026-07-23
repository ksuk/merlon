# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Merlon, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

### How to Report

Use [GitHub Private Vulnerability Reporting](../../security/advisories/new) to submit a report. This private channel is enabled on the repository, so reports stay confidential until a fix is released.

Please include:

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### Response Timeline

- **Acknowledgment**: within 48 hours
- **Initial assessment**: within 5 business days
- **Fix timeline**: depends on severity, typically within 30 days for critical issues

### Response Exercise

Before the first production release and at least annually thereafter, run a
tabletop exercise using a fictional advisory. Record detection and
acknowledgment times, owner, severity decision, affected-version analysis,
containment, patch or exception decision, coordinated communications, elapsed
time against the targets above, and follow-up actions. A security owner and an
independent observer must approve the sanitized record. Do not include exploit
details, credentials, private advisory content, or customer data in the public
repository.

### Scope

This policy covers the Merlon software as distributed in this repository. It does not cover:

- Vulnerabilities in dependencies (report those to the upstream project)
- Issues in deployment infrastructure (the responsibility of the deploying organization)
- Social engineering attacks

### Disclosure Policy

We follow coordinated disclosure. We will:

1. Confirm the vulnerability and determine its impact
2. Develop and test a fix
3. Release the fix and publish a security advisory
4. Credit the reporter (unless they prefer anonymity)

We ask that you:

- Allow reasonable time for us to address the issue before public disclosure
- Make a good faith effort to avoid privacy violations, data destruction, and service disruption
