# WhatsApp MCP (hardened)

**Read and search your WhatsApp messages from Claude, locally, with sending off by default.**

A [Model Context Protocol](https://modelcontextprotocol.io) server that links your personal WhatsApp account as a companion device (like WhatsApp Web) and exposes your chats to an MCP client such as Claude Code, Claude Desktop or Cursor. Messages are stored in a local SQLite database and only reach the model when a tool call reads them.

This is a security-focused fork of [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp), which is no longer maintained. It keeps the original design (Go bridge + Python MCP server) and fixes its main weaknesses. See [What changed from upstream](#what-changed-from-upstream).

![WhatsApp MCP in use](./example-use.png)

## Read this first

> [!WARNING]
> **This is an unofficial client.** It talks to WhatsApp through the multi-device protocol using [whatsmeow](https://github.com/tulir/whatsmeow). It is not affiliated with or endorsed by WhatsApp or Meta, and using unofficial clients can break WhatsApp's Terms of Service. **Your account can be restricted or banned.** Use it at your own risk, and preferably not on an account you cannot afford to lose.

> [!WARNING]
> **Sending is disabled by default, and you should think twice before enabling it.**
> Read access cannot change anything on your account. Write access lets an AI agent send messages *as you*, to real people. Anyone who can put text in front of the model (a message in a chat, a group, a forwarded file) can attempt a prompt injection that makes it send something you never intended. That is the [lethal trifecta](https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/): private data, untrusted content, and an outbound channel. Sending is off unless you set `WHATSAPP_ALLOW_SEND=1`, and even then it is rate limited and restricted (see [Sending messages](#sending-messages-opt-in)).

> [!NOTE]
> Everything in your chats is **untrusted input** to the model. Even in read-only mode, a malicious message can try to steer the model, so review what an agent does with the data you give it.

## Features

- Search contacts and list chats with their last message
- Read messages with filters, pagination and surrounding context
- Download images, videos, documents and voice notes on demand
- Everything stored locally; no third-party server in the path
- Optional, rate-limited sending of text, files and voice messages
- The bridge starts on demand and stops when your session ends

## Quick start

You need [Go](https://go.dev/dl/) (the version in `whatsapp-bridge/go.mod`), [uv](https://docs.astral.sh/uv/) and Python 3.11+. [FFmpeg](https://ffmpeg.org) is optional and only needed to send voice messages. Tested on macOS; Linux should work; Windows is untested.

**1. Clone and link your phone (once)**

```bash
git clone https://github.com/namecoder1/whatsapp-mcp-hardened.git
cd whatsapp-mcp-hardened/whatsapp-bridge
go run .
```

Scan the QR code from WhatsApp on your phone: *Settings → Linked devices → Link a device*. Wait until the initial history sync finishes, then stop the bridge with `Ctrl+C`. This is the only step that needs a terminal.

**2. Add the MCP server**

Claude Code:

```bash
claude mcp add whatsapp --scope user -- "$(which uv)" --directory "$PWD/../whatsapp-mcp-server" run main.py
```

Claude Desktop or Cursor (`claude_desktop_config.json` / `~/.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "/path/to/uv",
      "args": ["--directory", "/path/to/whatsapp-mcp-hardened/whatsapp-mcp-server", "run", "main.py"]
    }
  }
}
```

**3. Restart your client.** The first WhatsApp tool call starts the bridge automatically. It stops again after 15 minutes without requests, or when the session ends.

## Tools

Read-only tools (always available):

| Tool | What it does |
| --- | --- |
| `search_contacts` | Find contacts by name or phone number |
| `list_chats` | List chats with metadata and last message |
| `get_chat` | Details for one chat |
| `get_direct_chat_by_contact` | Find the 1:1 chat with a phone number |
| `get_contact_chats` | All chats involving a contact |
| `get_last_interaction` | Most recent message with a contact |
| `list_messages` | Messages with filters, pagination and context |
| `get_message_context` | Messages around a specific message |
| `download_media` | Download media from a message to local disk |

Sending tools (only registered when `WHATSAPP_ALLOW_SEND=1`): `send_message`, `send_file`, `send_audio_message`.

## Sending messages (opt-in)

Sending is a separate, deliberate step. Enable it by adding the variable to your MCP config:

```bash
claude mcp add whatsapp --scope user -e WHATSAPP_ALLOW_SEND=1 -- "$(which uv)" --directory /path/to/whatsapp-mcp-server run main.py
```

Even when enabled, the bridge enforces these limits regardless of what the model asks for:

- **At most 10 messages per hour, at least 30 seconds apart.** Counted in the database, so restarting does not reset them.
- **Only to chats where the other person has already written to you**, or to yourself. No first contact with strangers.
- **No groups**, unless you also set `WHATSAPP_ALLOW_GROUPS=1`.
- **Files only from an outbox folder** (`~/WhatsApp-outbox`), after resolving symlinks. The model cannot attach arbitrary files from your disk.
- **Automatic stop on restriction.** If WhatsApp reports a temporary ban or messaging restriction, the bridge writes `whatsapp-bridge/store/ACCOUNT_RESTRICTED`, exits, and refuses to start until you delete that file by hand.

These limits reduce the risk of a ban and of an injected message going out. They do not remove it.

## Configuration

All settings are environment variables. Set them in the `env` of your MCP config or with `-e` in `claude mcp add`.

| Variable | Default | Effect |
| --- | --- | --- |
| `WHATSAPP_ALLOW_SEND` | off | Set to `1` to register the sending tools |
| `WHATSAPP_ALLOW_GROUPS` | off | Set to `1` to allow sending to groups |
| `WHATSAPP_SEND_PER_HOUR` | `10` | Maximum sends per hour |
| `WHATSAPP_SEND_MIN_INTERVAL` | `30s` | Minimum time between sends (`30s`, `2m`) |
| `WHATSAPP_OUTBOX_DIR` | `~/WhatsApp-outbox` | The only folder files can be sent from |
| `WHATSAPP_AUTOSTART` | on | Set to `0` to start and stop the bridge yourself |
| `WHATSAPP_IDLE_TIMEOUT` | `15m` | Stop the bridge after this long without requests (`0` disables) |

## What changed from upstream

The original project exposed a local HTTP API with no authentication and read any file path it was given. This fork fixes that:

- **Unix socket instead of a TCP port.** The bridge API listens on `store/bridge.sock` (mode `0600`). A TCP port can be taken over by another local program while the bridge is off.
- **Token authentication on every request**, with a constant-time compare, JSON-only bodies, and rejection of browser requests (`Origin` / `Sec-Fetch-Site`), so a web page cannot drive the bridge.
- **Sending is opt-in** and limited as described above, instead of always available.
- **Outbox-only file access**, with symlink resolution, on both the bridge and the MCP server.
- **Untrusted names are sanitized.** File names, chat IDs and contact names from WhatsApp are cleaned before use in paths or display. Names chosen by the other person are marked with a `~` so they are not mistaken for saved contacts. SQL is parameterized.
- **Private storage.** Everything in `store/` is created with user-only permissions and is git-ignored.
- **On-demand bridge** with a lock that stops two bridges from sharing one session.
- **Logs go to stderr** (stdout is the MCP channel) and `whatsmeow` is updated to a current version.

The invariants these rely on are written down in [AGENTS.md](./AGENTS.md).

## Privacy and data

- Messages, the session, downloaded media and the API token live in `whatsapp-bridge/store/`. Treat that folder like a password: whoever can read it can read your chats and use your linked session.
- Nothing is uploaded anywhere by this project. Data reaches a model only when a tool returns it, and then it is subject to your MCP client's and model provider's policies.
- To remove everything: unlink the device in WhatsApp (*Settings → Linked devices*) and delete `whatsapp-bridge/store/`.

## Troubleshooting

- **A tool returns nothing.** Check `whatsapp-bridge/store/bridge.log`. If the bridge says it is not paired, run `go run .` in `whatsapp-bridge/` and scan the QR code again.
- **Sessions expire.** Linked devices are logged out after roughly two weeks if your phone stays offline. Pair again.
- **"Account restricted" / the bridge will not start.** WhatsApp restricted the account. Read the reason in `bridge.log`, wait it out, then delete `store/ACCOUNT_RESTRICTED` yourself.
- **No messages after pairing.** The first history sync can take several minutes on large accounts.
- **Out of sync.** Stop the bridge, delete `store/messages.db` and `store/whatsapp.db`, and pair again.
- **Device limit reached.** Remove a linked device on your phone first.

## Development

```bash
# Bridge
cd whatsapp-bridge && go vet ./... && go test ./...

# MCP server: list the tools it exposes
cd whatsapp-mcp-server && uv run python -c "import main, asyncio; print([t.name for t in asyncio.run(main.mcp.list_tools())])"
```

Contributions are welcome. Please read [CONTRIBUTING.md](./CONTRIBUTING.md) first, and report vulnerabilities privately as described in [SECURITY.md](./SECURITY.md).

## License and credits

MIT. Original work © Luke Harries; changes in this fork © their authors. See [LICENSE](./LICENSE). Built on [whatsmeow](https://github.com/tulir/whatsmeow) and the [MCP Python SDK](https://github.com/modelcontextprotocol/python-sdk).

WhatsApp is a trademark of Meta Platforms, Inc. This project is not affiliated with it.
