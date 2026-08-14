# Security policy

Do not report vulnerabilities through a public issue when the report contains credentials, private infrastructure details, or an unreleased exploit.

Use GitHub's private vulnerability reporting feature for this repository when it is available. Include the affected image digest, version, minimal reproduction, expected result, and observed result. Remove API keys, registration tokens, certificates, private addresses, and personal data before submission.

Compatibility credentials are supplied by the control plane at runtime. The controller must not print their values or persist them into the image.
