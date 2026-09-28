# Security policy

Chowki holds provider keys and sees every prompt that passes through it, so we treat security
reports as a priority.

## Supported versions

| Version | Supported |
|---|---|
| 0.1.x | Yes |
| Earlier builds from source | No |

Security fixes go into the latest release. Until v1.0, only the latest minor release gets them, so
upgrade to receive a fix.

## Report a vulnerability

Report a vulnerability privately through GitHub private vulnerability reporting:
<https://github.com/852hamza/chowki/security/advisories/new>.
Don't open a public issue, pull request or discussion about it.

Include:

- The affected version or commit. `chowki version` prints both.
- Steps to reproduce the problem, or a proof of concept.
- The impact you expect, such as a leaked key or a bypassed budget.
- Whether the details are public anywhere.

Never include real provider keys, virtual keys or other people's data in a report.

## What happens next

We confirm that we received your report, investigate it, and keep you informed while we work on a
fix. After the fix is released, we publish a GitHub security advisory and credit you, unless you
prefer to stay anonymous.
