# rehome

Keep your workstation in sync across every machine you use.

`rehome` keeps one portable config of your setup — apps, CLI tools, shell, app settings, AI tooling — in a private
repo, a folder or an archive. Each machine records what it has there as a node: its apps, their versions and its
settings. From any machine you can browse the others and install what they have, whatever their OS, with the right
package manager: set up a tool on your laptop, and `rehome install` on your desktop offers it. Secrets stay in
Bitwarden.

![Linked machines on Ubuntu, macOS and WSL publish to one private config; a new machine installs from it](.github/assets/rehome-flow.svg)

> Supports Ubuntu, Debian, macOS and Ubuntu/Debian on WSL.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/rchehd/rehome/main/install.sh | sh
```

To install a specific version, including a release candidate, pass a tag from the
[releases page](https://github.com/rchehd/rehome/releases):
`curl -fsSL https://raw.githubusercontent.com/rchehd/rehome/main/install.sh | REHOME_VERSION=<tag> sh`.

After installation, update rehome itself with:

```bash
rehome update --check        # show whether a newer stable release exists
rehome update                # download, verify and install it
rehome version
```

If your installed binary does not recognize `update` (including 0.1.0), rerun the installer above once.
Subsequent releases can use `rehome update`. `--dry-run` only checks, and `--json` returns structured results.
The updater chooses Linux/macOS and amd64/arm64 automatically, verifies SHA-256 and the candidate's version,
then atomically replaces the executable you launched. It preserves file permissions and symlinks. The executable's
directory must be writable; rehome does not elevate privileges. Configs and captured settings are unaffected.
An equal or newer installed version is kept. Development builds should install a release before using self-update.

Binaries for Linux and macOS are on the [releases page](https://github.com/rchehd/rehome/releases). To build from source:

```bash
git clone https://github.com/rchehd/rehome.git && cd rehome
CGO_ENABLED=0 go build -o ~/.local/bin/rehome ./cmd/rehome
```

Tab completion for commands, flags and app names:

```bash
echo 'source <(rehome completion zsh)' >> ~/.zshrc     # zsh
echo 'source <(rehome completion bash)' >> ~/.bashrc   # bash
rehome completion fish > ~/.config/fish/completions/rehome.fish
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
rehome install                                      # pick a machine, then the apps to install here
```

After you change something on any machine, run `rehome sync` there: it pulls, records this machine and publishes it.
The other machines see the change in `rehome install`, and their own `rehome sync` updates the settings of the apps
they already have.

## Commands

```bash
rehome                         # interactive menu: Scan / Install / Sync / Status / Pull / Push
rehome scan                    # record this machine: apps, versions, settings; add recipes for new apps
rehome install                 # pick a machine, then its apps to install here
rehome install <app>…          # install by name; --all for every machine's apps, --from <machine> for one machine's
rehome sync                    # pull, update settings of installed apps, record this machine and push
rehome remove <app>            # uninstall here; --everywhere marks it removed on every machine
rehome status                  # compare this machine with its record and the shared config (exit 1 if out of sync)
rehome nodes                   # list the machines; nodes rename / nodes forget
rehome link <repo|folder|archive>
rehome push / rehome pull      # publish this machine / fetch the others
rehome migrate                 # upgrade a config made by an older rehome
```

Every command takes `--dry-run` and `--json`. `install` and `sync` take `--yes` for non-interactive runs and
`--overwrite-files` to replace differing settings (a `.rehome.bak` is kept).

## Where the config lives

`rehome link` takes one of:

- a private git repo — machines pull and push it;
- a folder, written in place — a synced folder, a NAS or a USB stick;
- a `.tar.gz` archive — a local path is rewritten on every push; an `https://` archive is read-only.

For a one-off run, `--config` takes a folder, its `rehome.yml`, or an `https://…/rehome.yml` URL (recipes only;
you are warned it is not your config).

Inside, every machine writes only its own folder, so machines never conflict:

```
rehome.yml                  # recipes: how each app installs on each OS, shared by every machine
nodes/<machine>/node.yml    # one machine: its system, apps with versions, and settings files
nodes/<machine>/files/      # that machine's settings
```

See [examples/rehome.example.yml](examples/rehome.example.yml) for the recipe format.

## Keep several machines in sync

### Machines are nodes

A node is one machine's record, named after its hostname. `rehome scan` (and `rehome sync`) update it; `rehome push`
publishes it. `rehome nodes` lists them with when each last published; `rehome nodes rename <node> <name>` renames
one, and `rehome nodes forget <node>` drops a machine you no longer have. A node silent for 90 days is stale: its
apps stay out of `install --all` unless you pass `--include-stale` (set `stale_after_days` in
`~/.config/rehome/settings.yml` to change that).

### Install from another machine

`rehome install` in a terminal lists the machines, with how many apps each has that this one does not. Open one to
see its apps; Tab switches between all, missing here, can install, installed and no recipe. Space selects, `v`
switches between the same version as that machine and the latest, `s` leaves an app's settings out, and `r` writes a
recipe for an app this OS has no installer for. The line under the list says where the version and settings come
from, or why an app cannot be installed. Enter shows the plan before anything runs; Esc goes back to the machines.

Without the picker:

- `rehome install htop jq` — named apps, from whichever machine has them;
- `rehome install --all` — everything the live machines have;
- `rehome install --from laptop` — everything that machine has, with its versions and settings;
- `--prefer laptop` takes versions and settings from that machine first when several have an app;
- `--no-settings` installs the apps alone; `--reinstall` installs again what is present.

Settings come from `--from`, else the first `--prefer` machine that has them, else the machine that published them
most recently on the same OS. After an install, rehome offers to publish this machine (`--push` or `--no-push`).

### Sync

`rehome sync` pulls, applies the newest settings of the apps already installed here (asking before it replaces a
file you changed), records this machine and publishes it. It never installs new apps; `rehome install` does that.
`--no-apply` only exchanges the records, and `--no-push` keeps this machine's record local.

### Remove an app

`rehome remove <app>` uninstalls it here and drops it from this machine's record. With `--everywhere` it is also
marked removed for every machine: `install --all` skips it, and `rehome sync` on the others offers to remove it.
Installing it again by name lifts the mark.

### Check status

`rehome status` reports the config source, this machine's record and the local system:

- **This node** says whether the system differs from the record (`rehome scan` updates it) and whether the record
  has changes not published yet (`rehome push`);
- **Gone from this node** lists apps the record has that are no longer installed.

The default view shows a short table of counts and next steps, plus up to four version differences. Use
`rehome status --interactive` to browse categories with search and scrolling. The Status item in the main menu opens
the same browser.

| Command | View |
| --- | --- |
| `rehome status` | Summary with counts and next steps |
| `rehome status --interactive` | Categories, app search, versions and grouped file details |
| `rehome status --details` | Every app and file in plain text |
| `rehome status --json` | Complete data for scripts and agents, including installed and recorded versions |

In the browser, Tab / Shift+Tab switches categories, ↑ / ↓ and Page Up / Page Down scroll, `/` searches the
current category (including file paths), Enter opens an item's details, Esc goes back, and `q` quits. It is a
read-only snapshot. `--interactive` requires a terminal and cannot be combined with `--details` or `--json`.

Version columns are **ON MACHINE** and **IN CONFIG** (the newest any machine recorded). An older installed
version offers `rehome install --upgrade`; a newer local version offers `rehome scan` to record it before
publishing. Explicit pins are marked `(pin)`, Git installs show commits, and unreadable versions show `unknown`.

Missing-app details include the expected package manager or detection path. An APT zsh plugin and an Oh My Zsh
clone are separate installs when the config expects different paths.

## What it picks up

- **Packages:** apt (without the base OS, linked to third-party repos and their keys), snap, flatpak, Homebrew
  formulae, casks and taps, Mac App Store (with `mas`), global npm (nvm), pipx, uv tools, cargo, `go install`
- **Script installs** it knows from its catalog: Claude Code, Codex, oh-my-zsh and its plugins, uv, nvm, Go,
  JetBrains Toolbox, Zed, …
- **Settings:** zsh, oh-my-zsh custom, fonts, git, gh, Sublime Text, JetBrains, kitty, Ghostty, Zed, Claude Code
  and Codex (rules, hooks, MCP servers, plugins), SSH config and `known_hosts`
- **GNOME:** on Linux with `dconf-cli`, desktop, Shell, Mutter, keyboard shortcut and GNOME Terminal preferences
- **Unknown binaries** are kept in this machine's record, so nothing is lost; add a recipe
  (`rehome config add-install`) to install them elsewhere

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
app has no recipe for the current OS, rehome finds one: the built-in catalog first, then the same npm, pipx, uv,
cargo or Go package the other OS uses, then the same name in Homebrew or apt, then known renames (`fd-find` → `fd`),
then [Repology](https://repology.org). The plan marks these as auto-mapped, and the next `rehome scan` on that
machine records them. `--offline` keeps the lookup to the local package index. Homebrew is installed first if it is
missing.

An app that cannot be installed is listed with the reason: no recipe for this OS (with the command to add one, or
`r` in the picker), an apt repo it needs, a package built for another architecture, a package manager missing here,
or a graphical app on WSL.

WSL uses the same recipes as Ubuntu and Debian. rehome restores terminal tools and their settings and points you to
Windows for catalogued graphical apps and fonts; for your own graphical apps, set `group: desktop`. GNOME settings
are skipped on WSL, and Snap and Flatpak are used only when systemd is running.

## Versions

Every machine's record holds the version of each app it can read. On install you choose the same versions as the
other machines or the latest (`--version same|latest`; asked in a terminal, otherwise latest):

- the same version is installed only where the package manager can install a fixed one — npm, pipx, uv, cargo, Go,
  and apt while the repo still has it; elsewhere the plan says why it takes the latest;
- installed apps are never downgraded; older ones are listed, and `--upgrade` (or `u` in the picker) raises them;
- `version: 1.2.3` on an app pins it permanently.

Keep apps off an OS or a machine:

```yaml
apps:
  - id: steam
    skip: [macos]
ignore:
  my-laptop: [docker, php8.1]
```

## Upgrade a config from rehome 0.1

`rehome update` runs `rehome migrate` once the new version is installed, and any command on an old config offers to.
`rehome migrate --dry-run` lists the changes first. The migration pulls first: if another machine has already
migrated the shared config, this one only adds itself as a machine. Otherwise rehome turns the app list into recipes,
moves this machine's versions, settings files and unknown binaries into its node, and publishes it as one commit
(`git revert` undoes it); a folder or archive is backed up next to it as `.v1.bak`. If two machines migrate at the
same time, the second keeps its own migration on a `rehome-migration-…` branch and takes the published one.

## Secrets from Bitwarden

Secrets never go into the config. Install reads them from the Bitwarden CLI instead; log in and run:

```sh
BW_SESSION="$(bw unlock --raw)" rehome install --bitwarden
```

Without `--bitwarden`, install skips the mappings below and reports them. A differing local file or an existing key
is replaced only with `--overwrite-files`.

### A setting

In your trusted local config, add a `bitwarden` reference to the file's existing mapping in the machine's
`nodes/<machine>/node.yml` (paths are relative to that folder):

```yaml
files:
  myapp:
    - src: files/myapp/settings.env
      dest: {linux: ~/.config/myapp/settings.env, macos: ~/.config/myapp/settings.env}
      bitwarden: bw:12345678-1234-1234-1234-123456789abc/api_token
```

Put one `<rehome:redacted>` marker in the template, for example `API_TOKEN='<rehome:redacted>'`.
The reference uses the item's UUID and either a custom field name or `login.password`. Inline values support
letters, digits and `._~+/=@%:-`. Once you add the reference, edit the template in the config directly;
future scans leave it alone. The rendered file is written outside the config folder with permissions `0600`.

### An SSH key

Use the UUID of a Bitwarden SSH Key item. The app goes in `rehome.yml`, and the mapping, which stores only a
reference and a filename, in the machine's `node.yml`:

```yaml
# rehome.yml
apps:
  - id: ssh
    files: ssh
```

```yaml
# nodes/<machine>/node.yml
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
