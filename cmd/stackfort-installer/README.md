# Stackfort installer

`stackfort-installer` is the release installer entry point. Its I-001 surface is
intentionally limited to a read-only preflight:

```sh
stackfort-installer preflight
stackfort-installer preflight --format=json
stackfort-installer version
```

There is currently no install or apply subcommand. See
[Installer preflight](../../docs/installer-preflight.md).
