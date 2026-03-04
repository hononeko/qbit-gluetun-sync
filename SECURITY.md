# Security Policy

## Supported Versions

I currently support the latest **major** release with security updates. I encourage users to stay up-to-date with the latest version.

| Version | Supported          |
| ------- | ------------------ |
| Latest  | :white_check_mark: |
| Older   | :x:                |

## Reporting a Vulnerability

I take the security of `qbit-gluetun-sync` seriously. If you discover a security vulnerability within this project, please follow these steps to report it:

1. **Do not open a public issue.** This could expose the vulnerability to malicious actors before a patch is available.
2. Please report the vulnerability using **GitHub's private vulnerability reporting feature**:
   - Navigate to the **Security** tab of the repository.
   - Click on **Advisories** in the sidebar.
   - Click **Report a vulnerability**.
3. Provide a detailed description of the vulnerability, including:
   - A description of the issue.
   - Steps to reproduce it.
   - Potential impact.
   - Suggested mitigations (if any).

I aim to acknowledge your report within 48 hours and provide an estimated timeline for addressing the issue. I will keep you updated on the progress and notify you when the fix is released.

## Security Best Practices

When deploying this sidecar, please consider the following security best practices:

- **Run as non-root**: Ensure the sidecar container runs as a non-root user (this is the default behavior in our provided Docker image).
- **Internal Network**: Ensure the `QBIT_ADDR` is an internal network address and not exposed to the public internet.
- **Secrets Management**: If you are using authentication, provide the `QBIT_USER` and `QBIT_PASS` environment variables securely (e.g., via Kubernetes Secrets, Docker Configs/Secrets, or secure environment variable injection), rather than hardcoding them in plaintext configuration files.
