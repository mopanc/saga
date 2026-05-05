# Security policy

## Supported versions

Saga is in pre-release. The latest tagged release receives security updates.

| Version       | Supported |
|---------------|-----------|
| v1.0.0-beta.x | ✅        |
| < v1.0.0      | ❌        |

## Reporting a vulnerability

**Do not** open a public issue or pull request for security vulnerabilities.

Email **jorgemopanc@icloud.com** with:

- Description of the vulnerability
- Steps to reproduce
- Affected version(s)
- Suggested fix (optional)

You will receive an acknowledgement within **7 days**.

## Disclosure timeline

| Day      | Action                                                     |
|----------|------------------------------------------------------------|
| 0        | Report received                                            |
| 1–7      | Initial triage and acknowledgement                         |
| 7–60     | Investigation and fix development                          |
| 60–90    | Coordinated disclosure with reporter                       |
| 90       | Public disclosure (with credit unless reporter opts out)   |

We follow a **90-day embargo** by default. Extensions may be agreed for complex issues.

## Scope

In scope:

- The `saga` binary and its source code at `github.com/mopanc/saga`
- The MCP server (`saga mcp`) and tool implementations
- The Claude Code hook (`saga hook`)

Out of scope:

- Third-party MCP clients (Claude Code, Cursor, Windsurf, etc.) — report to those projects directly.
- Vulnerabilities introduced by user-supplied topic markdown files. Treat topics as code; review before merging into a project's `.saga/`.
- Issues that require local filesystem access already granted to the user.

## Trust boundaries

- Saga runs as the local user. There is **no authentication** between MCP clients and the Saga server. Any process running as the user can connect via stdio and read all topics.
- Personal layer (`~/.saga/personal/`) and project layers (`<project>/.saga/`) are stored as plain markdown. **Do not store credentials, tokens, or secrets in topic content.**
- Git remotes used to sync the personal layer should be **private** repositories. Saga does not enforce this; it is the user's responsibility.

## Hall of fame

Reporters who follow responsible disclosure are credited here (with permission).

*(empty so far)*
