# Contributing

Thanks for your interest. This project connects a personal WhatsApp account to an AI agent, so changes are reviewed with security and account safety in mind.

## Before you start

- Open an issue to discuss anything larger than a small fix.
- Report vulnerabilities privately, see [SECURITY.md](./SECURITY.md).
- Read the **Security invariants** and **Ban-avoidance rules** in [AGENTS.md](./AGENTS.md). Pull requests that weaken them will not be merged. In particular: no TCP listener, no unauthenticated endpoints, no arbitrary file paths, no bulk sending, broadcasts or messaging strangers.

## Development

```bash
# Bridge (Go)
cd whatsapp-bridge
gofmt -l .        # should print nothing
go vet ./... && go test ./...

# MCP server (Python, managed with uv)
cd whatsapp-mcp-server
uv run python -c "import main, asyncio; print([t.name for t in asyncio.run(main.mcp.list_tools())])"
```

Run the bridge with `go run .`, not `go run main.go`: the package has several files.

## Pull requests

- Keep them focused, with a clear description of what and why.
- Add tests next to the code (`*_test.go`) for anything related to security or send limits.
- Log from Python with `log(...)` (stderr), never `print(...)`: stdout is the MCP protocol channel.
- Never commit anything from `whatsapp-bridge/store/`, tokens, or real message data, and do not paste real chats or phone numbers into issues or tests.

## License

By contributing you agree that your contributions are licensed under the [MIT License](./LICENSE).
