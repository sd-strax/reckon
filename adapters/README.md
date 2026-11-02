# adapters/

First-party adapter binaries. Each subdirectory is its own Go-buildable adapter binary that reckon's capability layer spawns as a separate process and communicates with over JSON-RPC (MCP-compatible transport).

See `design/03-capability-layer.md §5.4` for the adapter contract and class taxonomy (`MCP`, `NATIVE_API`, `CUSTOM`, `FIXTURE`, `SOAR_PLAYBOOK`).

## Status

Empty. First-party adapter binaries live under `cmd/reckon-adapter-*`, and the fixture adapter runs in process (`capability/`).

Scaffold new adapters with the `/generate-adapter-scaffold` slash command.
