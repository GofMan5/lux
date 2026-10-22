# Security policy

## Supported versions

Until v1.0, only the latest `0.x` minor release and `main` receive security
fixes, shipped as a patch release of that minor. Older minors are not
patched; upgrade with [docs/MIGRATION.md](docs/MIGRATION.md).

| Version | Supported |
| --- | --- |
| `main` | yes |
| latest `0.x` minor (currently v0.1) | yes |
| older `0.x` minors | no |

## Reporting a vulnerability

Use GitHub's **Report a vulnerability** flow if private vulnerability reporting
is enabled for this repository. Otherwise, contact a maintainer privately
through the repository owner's GitHub profile. Do not open a public issue for
an unpatched vulnerability.

Include:

- affected revision and platform;
- terminal, multiplexer, and transport where relevant;
- minimal reproducer or malformed byte sequence;
- observed and expected behavior;
- impact and any known workaround.

Expect an acknowledgement within seven days. Timeline and disclosure will be
coordinated after reproduction and severity assessment.

## Security-sensitive surfaces

Terminal software crosses several trust boundaries:

- input escape sequences and bracketed-paste payloads;
- terminal capability replies and environment hints;
- OSC/DCS control strings, links, clipboard, notifications, and graphics;
- dimensions and coordinates from remote PTYs;
- terminal mode setup, suspend/resume, and restoration;
- output writers that may perform partial writes or fail.

Changes to these areas require bounded parsing, integer/size validation,
control-sequence tests, failure cleanup, and a conservative fallback. Never
place secrets in terminal titles, links, clipboard sequences, logs, snapshots,
or issue reports.

Implemented controls include the OSC 8 output seam: link URIs and IDs are
validated before encoding (printable runes only, no id field separators,
capped field sizes), and an untransmittable destination degrades to plain
text rather than reach the wire. Reports of a bypass belong here.
