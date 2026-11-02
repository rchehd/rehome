# rehome

Move your whole workstation to a new machine.

`rehome` scans an already configured system — apps, CLI tools, shell setup, app settings, AI tooling — and
generates a portable YAML config. On a fresh machine, feed it the same config and it installs everything
from scratch or fills in whatever is missing, picking the right package manager for the detected OS.

> Status: early design. Nothing to install yet.

## Planned

- `rehome scan` — detect installed software and settings, generate or update the config
- `rehome install` — TUI checklist; install from scratch or only what is missing
- `rehome push` / `rehome pull` — sync the config through your own private git repo
- Config from a local file, a URL, or a linked private repository

## Roadmap

1. Ubuntu / Debian
2. macOS (Homebrew)
3. Secrets via Bitwarden
4. Fedora, Arch

## License

TBD
