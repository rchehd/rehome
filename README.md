# rehome

Move your whole workstation to a new machine.

`rehome` scans an already configured system — apps, CLI tools, shell setup, app settings, AI tooling — and
generates a portable config. On a fresh machine, feed it the same config and it installs everything from scratch
or fills in whatever is missing, picking the right package manager for the detected OS.

> Supports Ubuntu and Debian. No release yet — build from source.

## Install

```bash
git clone https://github.com/rchehd/rehome.git && cd rehome
CGO_ENABLED=0 go build -o ~/.local/bin/rehome ./cmd/rehome
```

Once releases are published: `curl -fsSL https://raw.githubusercontent.com/rchehd/rehome/main/install.sh | sh`.

## Usage

```bash
rehome                      # interactive menu: Scan / Install / Status / Pull / Push
rehome scan                 # detect software and settings, create or update the config
rehome install              # checklist of what is missing, then install it
rehome status               # compare the config with this system (exit 1 when they differ)
rehome link <git-url>       # keep the config in your own private repo
rehome push / rehome pull   # sync it between machines
```

Every command takes `--dry-run`. `install` takes `--yes` for non-interactive runs, `--all` to reinstall what is
present, `--overwrite-files` to replace differing settings (a `.rehome.bak` is kept).

### Where the config lives

- a linked private git repo (`rehome link`) — the default once linked;
- a folder with `rehome.yml` and `files/`: `--config ~/backup/rehome-config` (USB stick, archive, anything);
- a single file: `--config ./rehome.yml` (settings come from `files/` next to it);
- a URL: `--config https://…/rehome.yml` (app list only; you are warned it is not your repo).

## What it picks up

- **Packages:** apt (without the base OS, linked to third-party repos and their keys), snap, flatpak, global npm
  (nvm), pipx, uv tools, cargo, `go install`
- **Script installs** it knows from its catalog: Claude Code, Codex, oh-my-zsh and its plugins, uv, nvm, Go,
  JetBrains Toolbox, Zed, …
- **Settings:** zsh, oh-my-zsh custom, fonts, git, gh, Sublime Text, JetBrains, kitty, Ghostty, Zed, Claude Code
  and Codex (rules, skills, agents, hooks, MCP servers, plugins)
- **Unknown binaries** are listed disabled, so nothing is lost; add an install method to enable them

Secrets are never written: tokens, passwords and keys in settings files are replaced with `<rehome:redacted>`,
and `rehome push` refuses to push anything that still looks like a secret. Log in again after installing.

## How install behaves

- Installs in dependency order; one failing app never stops the rest, and the report says why it failed.
- Re-running is safe: present apps are skipped, local settings are never overwritten without asking.
- Third-party apt repos are retargeted to the new release; repos that fail are disabled and reported.

## License

TBD
