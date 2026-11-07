# Security

## LAN sync threat model

LAN sync (`rehome serve`, `rehome link lan`, `rehome sync`) lets paired machines on one network read each other's
rehome records. Nothing listens unless one of those commands runs.

### What is protected

- **Your node.** Only this machine writes `nodes/<this machine>/`, and every save signs the whole folder (`manifest.sig`)
  with this machine's key in `~/.config/rehome/lan/node.key` (owner-only).
- **Other machines' nodes.** A copy is taken only when it is signed by the key of a machine you paired with directly,
  matches its signed manifest file by file, and has a higher revision than the copy you hold.
- **What runs on install.** Recipes from a peer that run code or add a source (`script`, `post_install`, `deb_url`,
  `tap`, `repo`, `remote_url`) are shown and added only after you say yes; `--yes` and non-interactive runs skip
  them. Capture rules and legacy apt keys are never taken from a peer.
- **Secrets.** Records hold redacted files only; a record or `rehome.yml` with a secret-looking value is not served.

### Who the attacker is and what they get

| Attacker | Can | Cannot |
| --- | --- | --- |
| A device on the same network that never paired | See that rehome runs (mDNS), try 3 pairing codes while `--pair` is open, hold connection slots while you serve | Read anything, pair without the code, pass for a paired machine (TLS 1.3 with pinned keys both ways) |
| Someone relaying traffic during pairing | Make the pairing fail | Pair: the code is never sent, and CPace binds both keys and the TLS channel |
| A machine you paired with | Read your shared records and recipes, offer recipes and removals, refuse to serve | Forge or change another machine's node, overwrite yours, take a name or id that passes for another machine, add code-running recipes without your yes |
| A stolen laptop that was paired | Read what the others serve while it is still trusted | Keep that after `rehome nodes unpair <node>` on each machine (restart a running `serve` to apply it) |

### Limits

- Trust is per pair: a machine you never paired with directly is not relayed to you, even by a paired one.
- Removals (`removed:`) from a paired machine are trusted as sent; `rehome sync` still asks before removing an app.
- Pairing uses `filippo.io/cpace`, an implementation of an early CPace draft.

## Reporting a problem

Open a private security advisory on the repository rather than a public issue.
