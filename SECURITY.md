# Security Policy

PPanel handles payments, sessions and the credentials of proxy nodes, so we
treat security reports as a priority. Thank you for taking the time to report
one responsibly.

## Supported versions

| Line | Branch | Where it ships | Security fixes |
|---|---|---|---|
| Stable | `main` | Tagged releases and the `latest` image tag | Yes, as a patch release |
| Development | `dev` | The rolling `nightly` release and image tag | Yes, in the next nightly |
| Older tagged releases | | | Upgrade to the current stable release |

The frontend lives in [perfect-panel/frontend](https://github.com/perfect-panel/frontend)
and has its own policy; reports about it belong there.

## Reporting a vulnerability

Please do not report security vulnerabilities through public GitHub issues,
discussions or pull requests, and do not attach exploit details to an
existing issue.

Report them privately through GitHub Security Advisories:
[Report a vulnerability](https://github.com/perfect-panel/backend/security/advisories/new)
(the "Security" tab of this repository). The report is visible to the
maintainers only, and the advisory is where the fix is coordinated and the
credit recorded.

A useful report contains:

- the version or commit (`ppanel-server version`, the image tag, or the tag of
  the release archive) and how it is deployed (binary, Docker, behind which
  proxy);
- the affected component (API endpoint, payment provider, node protocol,
  subscription output, configuration);
- steps to reproduce, or a proof of concept, and the impact you see
  (what an attacker gains, which role they need);
- whether the issue is already public or known to you to be exploited.

If you cannot use GitHub's form, open an issue that only asks for a private
contact, without any details of the finding, and a maintainer will reply with
one.

## What to expect

- Acknowledgement within 3 business days of the report.
- An assessment (confirmed, not a vulnerability, duplicate, out of scope) and
  a severity within 7 days, with questions if we cannot reproduce it.
- A fix on the stable line, released as a patch version and a new `latest` image,
  and on `dev`. We aim at 30 days for critical and high severity and 90 days
  otherwise, and we tell you when that is not achievable and why.
- Coordinated disclosure: the advisory is published with the fix, and you are
  credited unless you prefer not to be. Please give us up to 90 days before
  disclosing on your own.

## Scope

In scope: this repository, the release binaries, the container images and the
install script and documentation shipped with them.

Out of scope, please report upstream instead (and tell us, so we can update
the dependency): vulnerabilities in third-party dependencies that this server
does not expose, and in the databases, Redis and reverse proxies it runs
with. Reports about instances operated by third parties belong to their
operators.

Testing against your own installation is welcome. Do not test against
installations you do not own, do not access or modify other people's data,
and do not run denial-of-service tests against public instances.

## Hardening a deployment

[docs/guide/config.md](docs/guide/config.md) documents the settings that
matter for security: a long random `JwtAuth.AccessSecret`, `TrustedProxies`
and `AllowedOrigins` for an installation behind a reverse proxy, TLS to the
database (`sslmode`) and the request limits of the `HTTP` section.
[docs/guide/install.md](docs/guide/install.md) shows a systemd unit running
the server as a dedicated user, and the container image runs as uid 65532.
Keep `etc/ppanel.yaml` readable by the service user only (`0600`): it holds
the JWT secret and the database credentials.
