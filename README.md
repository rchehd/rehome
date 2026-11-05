# rehome

Keep your workstation in sync across every machine you use.

`rehome` keeps one portable config of your setup — apps, CLI tools, shell, app settings, AI tooling — in a private
repo. Every linked machine, whatever its OS, can push its changes there and pull everyone else's: install a tool on
your laptop, and `rehome sync` brings it to your desktop with the right package manager. A fresh machine gets the
whole setup in one `sync`. Secrets stay in Bitwarden.

![Linked machines on Ubuntu, macOS and WSL push to and sync from one private config; a new machine syncs from it](.github/assets/rehome-flow.svg)

> Supports Ubuntu, Debian, macOS and Ubuntu/Debian on WSL.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/rchehd/rehome/main/install.sh | sh
```

To install a specific version, including a release candidate, pass a tag from the
[releases page](https://github.com/rchehd/rehome/releases):
`curl -fsSL https://raw.githubusercontent.com/rchehd/rehome/main/install.sh | REHOME_VERSION=<tag> sh`.

Binaries for Linux and macOS are on the [releases page](https://github.com/rchehd/rehome/releases). To build from source:

```bash
git clone https://github.com/rchehd/rehome.git && cd rehome
CGO_ENABLED=0 go build -o ~/.local/bin/rehome ./cmd/rehome
```

## Quick start

On the machine you have already set up:

```bash
rehome link git@github.com:you/rehome-config.git   # an empty private repo
rehome scan                                         # review what was found
rehome push
```

On every other machine, new or already in use:

```bash
rehome link git@github.com:you/rehome-config.git
rehome sync                                         # pull the config and install what is missing
```

After you change something on any machine, share it with `rehome pull && rehome scan && rehome push`; the others pick
it up with `rehome sync`.

## Commands

```bash
rehome                      # interactive menu: Scan / Install / Sync / Status / Pull / Push
rehome scan                 # detect software and settings, create or update the config
rehome install              # checklist of what is missing, then install it
rehome sync                 # pull the linked config and apply it with the install workflow
rehome status               # check the linked source and local system (exit 1 if out of sync)
rehome link <git-url>       # keep the config in your own private repo
rehome push / rehome pull   # sync it between machines
```

Every command takes `--dry-run` and `--json`. `install` takes `--yes` for non-interactive runs, `--all` to reinstall
what is present, `--overwrite-files` to replace differing settings (a `.rehome.bak` is kept).

## Where the config lives

- a linked private git repo (`rehome link`) — the default once linked;
- a folder with `rehome.yml` and `files/`: `--config ~/backup/rehome-config` (USB stick, archive, anything);
- a single file: `--config ./rehome.yml` (settings come from `files/` next to it);
- a URL: `--config https://…/rehome.yml` (app list only; you are warned it is not your repo).

See [examples/rehome.example.yml](examples/rehome.example.yml) for the format.

## Keep several machines in sync

Any machine can update the config after you install a new tool, and the others apply the update.

- **With Git**, run `rehome link <git-url>` once on each machine. On the machine that changed, run `rehome pull`,
  `rehome scan`, then `rehome push`. On the others, `rehome status` shows whether there is an update and
  `rehome sync` pulls and applies it.
- **With a file or folder**, start from the latest copy and run `rehome scan --config ./rehome-config` on the machine
  with changes. Share the updated `rehome.yml` with the `files/` next to it (settings) and `rehome.lock` (versions).
  On the receiving machine, run `rehome sync --config ./rehome-config` (the folder or its `rehome.yml`).

If scan lists a tool as an unknown binary, add an install method and enable it before sharing the config.
`rehome status` reports the linked Git branch and the local system separately; with `--config` it compares the
supplied config with the local system. The default view shows a short table of counts and next steps, plus up to
four version differences. Use `rehome status --interactive` to browse categories with search and scrolling.
The Status item in the main menu opens the same browser.

| Command | View |
| --- | --- |
| `rehome status` | Summary with counts and next steps |
| `rehome status --interactive` | Categories, app search, versions and grouped file details |
| `rehome status --details` | Every app and file in plain text |
| `rehome status --json` | Complete data for scripts and agents, including installed and recorded versions |

In the browser, Tab / Shift+Tab switches categories, ↑ / ↓ and Page Up / Page Down scroll, `/` searches the
current category (including file paths), Enter opens an item's details, Esc goes back, and `q` quits. It is a
read-only snapshot. `--interactive` requires a terminal and cannot be combined with `--details` or `--json`.

Version columns are **ON MACHINE** and **IN CONFIG**. An older installed version offers `rehome install --upgrade`;
a newer local version offers `rehome scan` to record it before publishing. Explicit pins are marked `(pin)`, Git
installs show commits, and unreadable versions show `unknown`. `status` reads version data without changing the
config; `scan` writes the versions it can read to `rehome.lock`.

Missing-app details include the expected package manager or detection path. An APT zsh plugin and an Oh My Zsh
clone are separate installs when the config expects different paths.

To publish local tools and settings, run `rehome scan`, review the result, then `rehome push` for a linked repo.
To apply the shared config on this machine, run `rehome sync`. For a file or folder, `rehome scan --config <path>`
saves local changes. Share the updated folder yourself, then apply that copy with `rehome sync --config <path>`.

## What it picks up

- **Packages:** apt (without the base OS, linked to third-party repos and their keys), snap, flatpak, Homebrew
  formulae, casks and taps, Mac App Store (with `mas`), global npm (nvm), pipx, uv tools, cargo, `go install`
- **Script installs** it knows from its catalog: Claude Code, Codex, oh-my-zsh and its plugins, uv, nvm, Go,
  JetBrains Toolbox, Zed, …
- **Settings:** zsh, oh-my-zsh custom, fonts, git, gh, Sublime Text, JetBrains, kitty, Ghostty, Zed, Claude Code
  and Codex (rules, hooks, MCP servers, plugins), SSH config and `known_hosts`
- **GNOME:** on Linux with `dconf-cli`, desktop, Shell, Mutter, keyboard shortcut and GNOME Terminal preferences
- **Unknown binaries** are listed disabled, so nothing is lost; add an install method to enable them

Settings of apps outside the catalog are found automatically in the usual places (`~/.config/<app>`, `~/.<app>`,
`~/.<app>rc`, flatpak and snap config dirs), skipping caches, logs and databases. They are recorded under `capture`
with `origin: auto`. Current automatic exclusions also apply to rules saved by older releases, covering runtime
state, Desktop session stores, logs, caches and credential files. Existing stored files are kept; review old captures
before sharing them. To take control of a rule, remove `origin: auto` and select the paths explicitly. For example:

```yaml
capture:
  myapp:
    linux: { base: ~/.config/myapp, include: ["*.toml", "themes/**"] }
```

Anything rehome cannot take by itself — a settings folder too large to copy whole, a `~/.config` folder no app claims,
an unknown binary — is listed under **needs your attention** with what to do about it.

If an APT system has neither an installer package snapshot nor a desktop/server metapackage, scan and status
report that the baseline is limited. Additional system packages can appear as new; review them before publishing.

Captured settings replace detected tokens, passwords and keys with `<rehome:redacted>`, and `rehome push` refuses to
push a config that still contains secrets. To restore them, see [Secrets from Bitwarden](#secrets-from-bitwarden).

### Transfer AI skills and subagents

Use `rehome scan --harness-data` to include personal skills and subagents from Claude Code, Codex, and Cursor,
including shared skills under `~/.agents/skills`. Review the captured files before sharing the config.
On the receiving machine, use `rehome sync --harness-data` or `rehome install --harness-data --config <path>`.

### Transfer local agent sessions

Use `rehome scan --sessions` on the source machine, share the config, then run `rehome sync --sessions`
on the receiving machine. For a file or folder, use `rehome install --sessions --config <path>`.
Combine `--sessions --harness-data` to include personal skills and subagents too.

This carries Claude Code JSONL conversations, Codex active and archived rollouts (including Zstandard archives),
and Cursor CLI and ACP stores. Claude Desktop's `claude-code-sessions` store is unsupported, excluded from
automatic settings capture even with `--sessions`, and reported under **needs your attention**.
Snapshots keep session IDs and rebuild Cursor's blob references after secret redaction. Cursor snapshots retain
their per-session encryption metadata. Import adds sessions and preserves existing destination histories.

Paths under the original home directory adapt to the receiving home directory. For a project in a different
location, pass `--session-path-map /old/project=/new/project` when installing or syncing. Run the agent in the
restored project and continue with `claude --resume <id>`, `codex resume <id>`, or `cursor-agent --resume <id>`.

Session capture uses the catalog's rules and reports incomplete records, unresolved Cursor workspaces, and sessions
above 128 MiB. For an older Cursor store without its workspace path, supply its original project path through
`--session-path-map` during the scan as well. Review the snapshots before sharing them.

## How install behaves

- Installs in dependency order; one failing app never stops the rest, and the report says why it failed.
- Re-running is safe: present apps are skipped, local settings are never overwritten without asking.
- Third-party apt repos are retargeted to the new release; repos that fail are disabled and reported.
- GNOME settings are restored through `dconf-service` over D-Bus. A snapshot that conflicts with local values is kept
  unapplied unless you pass `--overwrite-files`, which saves a private backup and keeps settings outside the snapshot.

## Linux, macOS and WSL

A config made on Ubuntu installs on a Mac (and the other way round) without you looking up package names. When an
app has no installer for the current OS, rehome finds one: the built-in catalog first, then the same name in Homebrew
or apt, then known renames (`fd-find` → `fd`), then [Repology](https://repology.org). The plan marks these as
auto-mapped, and the next `rehome scan` on that machine records them. Apps that do not exist on the other OS are listed
with what to add; `--offline` keeps the lookup to the local package index. Homebrew is installed first if it is missing.

WSL uses the same config as Ubuntu and Debian. rehome restores terminal tools and their settings and points you to
Windows for catalogued graphical apps and fonts; for your own graphical apps, set `group: desktop`. GNOME settings are
skipped on WSL, and Snap and Flatpak are used only when systemd is running. Scanning WSL does not mark intentionally
skipped apps as missing in the shared config.

## Versions

`rehome scan` also writes `rehome.lock` next to the config, with the version of every app it can read. On install:

- with a lock you choose the same versions or the latest (`--versions locked|latest`); without a lock you get the latest;
- a pinned version that is no longer available is replaced by the latest, and the report says so;
- installed apps are never downgraded; older ones are listed, and `--upgrade` (or `u` in the checklist) raises them;
- `version: 1.2.3` on an app pins it permanently; `--lock <file>` uses a lock kept elsewhere.

Keep apps off an OS or a machine:

```yaml
apps:
  - id: steam
    skip: [macos]
ignore:
  my-laptop: [docker, php8.1]
```

## Secrets from Bitwarden

Secrets never go into the config. Install reads them from the Bitwarden CLI instead; log in and run:

```sh
BW_SESSION="$(bw unlock --raw)" rehome install --bitwarden
```

Without `--bitwarden`, install skips the mappings below and reports them. A differing local file or an existing key
is replaced only with `--overwrite-files`.

### A setting

In your trusted local config, add a `bitwarden` reference to the file's existing mapping:

```yaml
files:
  myapp:
    - src: files/myapp/settings.env
      dest: {linux: ~/.config/myapp/settings.env, macos: ~/.config/myapp/settings.env}
      bitwarden: bw:12345678-1234-1234-1234-123456789abc/api_token
```

Put one `<rehome:redacted>` marker in the template, for example `API_TOKEN='<rehome:redacted>'`.
The reference uses the item's UUID and either a custom field name or `login.password`. Inline values support
letters, digits and `._~+/=@%:-`. Once you add the reference, edit the template in the config repo directly;
future scans leave it alone. The rendered file is written outside the config folder with permissions `0600`.

### An SSH key

Use the UUID of a Bitwarden SSH Key item. The mapping stores only a reference and a filename:

```yaml
apps:
  - id: ssh
    files: ssh
files:
  ssh:
    - dest: {linux: ~/.ssh/id_work, macos: ~/.ssh/id_work}
      bitwarden: bw:12345678-1234-1234-1234-123456789abc/sshKey/privateKey
```

The private key is written with mode `0600`, and `~/.ssh` is created with mode `0700` if needed. Replacing an
existing key with `--overwrite-files` keeps a private backup.

## Change the config from the command line

```bash
rehome config add-install hermes --platform any --script 'curl -fsSL https://…/install.sh | bash' --bin hermes
rehome config add-capture myapp --base ~/.config/myapp --include '*.toml'
rehome config set steam enabled=false skip=macos
```

Comments in `rehome.yml` are kept, and a change that would make the config invalid is refused.

## Use it with an AI agent

`rehome mcp` serves rehome to agents over MCP. At the end of an interactive `rehome install` or `rehome sync`, rehome
offers to register it with detected AI agents that do not have it yet. To register it with every agent found on this
machine (Claude Code, Codex, Cursor) at any time:

```bash
rehome mcp install
```

For any other MCP client, add a stdio server with command `rehome` and argument `mcp`:

```json
{ "mcpServers": { "rehome": { "command": "rehome", "args": ["mcp"] } } }
```

The agent can scan, read status and what needs your attention, and fix the config (`add_install`, `add_capture`,
`set_app`). It never installs: installing needs `sudo` in your terminal, so the agent shows you the plan and the exact
command to run.

## License

[MIT](LICENSE)
