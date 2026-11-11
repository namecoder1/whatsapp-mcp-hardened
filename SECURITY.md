# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for security problems. Use GitHub's private reporting instead: go to the **Security** tab of this repository and choose **Report a vulnerability**.

Include what you found, how to reproduce it, and what an attacker gains. You can expect an acknowledgement within a few days. This is a small volunteer project, so fixes are best effort, but security reports are handled before anything else.

## Scope

In scope:

- The Go bridge (`whatsapp-bridge/`): the Unix socket API, authentication, path handling, send limits.
- The Python MCP server (`whatsapp-mcp-server/`): tool inputs, SQL queries, bridge calls.
- Anything that lets untrusted content (a WhatsApp message, file name, contact name) cause an action the user did not approve, such as sending a message, reading a file outside the outbox, or reaching the API without the token.

Out of scope:

- Vulnerabilities in WhatsApp itself, `whatsmeow`, or your MCP client.
- Prompt injection as a general property of LLM agents. Reports about concrete missing safeguards in this project are welcome.
- Account restrictions or bans by WhatsApp. This is an unofficial client and that risk is documented.

## Security model

The design assumes the local machine and the user account are trusted, and everything coming from WhatsApp is not. The rules that follow from that are listed under "Security invariants" in [AGENTS.md](./AGENTS.md). The most important ones: the API is a Unix socket only, every request needs the token, files are only sent from the outbox, and sending is opt-in and rate limited.

Anyone who can read `whatsapp-bridge/store/` can read all your messages and use your linked session. Keep that folder private and out of backups you do not control.

## Supported versions

Only the latest commit on `main` is supported.
