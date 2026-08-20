# Security Policy

## Supported versions

Security fixes are provided for the latest stable release. Older releases should be upgraded before reporting a version-specific issue.

## Reporting a vulnerability

Please do not open a public issue for a suspected vulnerability.

Use GitHub's **Report a vulnerability** form in the repository Security tab. Include:

- the affected version and deployment mode;
- clear reproduction steps or a minimal proof of concept;
- the expected impact;
- any suggested mitigation, if available.

Do not include production credentials, private keys, access tokens, database copies, customer data, or unredacted diagnostic packages.

We aim to acknowledge a valid report within 72 hours and provide an initial assessment within 7 days. Disclosure should be coordinated until a fix or mitigation is available.

## Security boundaries

- Only test systems and data you own or are explicitly authorized to test.
- Public monitoring details are intentionally limited; enabling `PUBLIC_MONITOR_DETAILS` increases exposed host information.
- Agent installation commands contain a node credential and must be handled as secrets.
- Backups may contain sensitive operational data and must be encrypted with an independent `BACKUP_ENCRYPTION_KEY`.
