# Sloptools Vault Config

Brain tools read vault roots from a user-owned TOML file. The default path is
`~/.config/sloptools/vaults.toml`; `sloptools mcp-server --vault-config PATH`
sets the server default, and every MCP brain tool also accepts an optional
`config_path` argument.

The repository ships no default vault paths. Users create their own config:

```toml
[[vault]]
sphere = "work"
root = "/path/to/work-vault"
brain = "brain"
label = "Work"
hub = true
exclude = ["personal"]

[[vault]]
sphere = "private"
root = "/path/to/private-vault"
brain = "brain"
label = "Private"
```

Fields:

- `sphere`: required. Either `work` or `private`.
- `root`: required. Absolute or relative path to the vault root; normalized to
  an absolute path when loaded.
- `brain`: optional. Brain directory inside the vault; defaults to `brain`.
- `label`: optional display label.
- `hub`: optional marker for the default hub workspace.
- `exclude`: optional vault-relative directory names that link resolution,
  search, and validation must not enter. The work sphere always treats
  `personal` as excluded even if omitted.

Keep paths user-local. Do not commit a populated `vaults.toml`.
