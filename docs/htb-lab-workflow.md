# HTB lab workflow

The Trusted Linux Workcell separates two kinds of secrets:

- operator and host credentials remain denied globally;
- credentials recovered from the single authorized lab target may be consumed
  locally without being copied into model turns, logs, argv, the VPS, Brain, Events,
  or telemetry.

## One-command setup after VPN

Connect the HTB VPN first. Then initialize or update one machine with:

```bash
mcp-edge lab init \
  --platform htb \
  --machine Cap \
  --target 10.129.63.65 \
  --difficulty easy
```

`lab init`:

1. validates that the target route uses a `tun*` or `tap*` interface;
2. discovers the VPN interface and LHOST;
3. creates `$HOME/htb-machines/<machine>` with private permissions;
4. initializes Git and a minimal README when missing;
5. registers or reuses one `linux-workcell` workspace;
6. applies immutable `htb-linux` metadata;
7. validates the local tool inventory;
8. prints the opaque workspace ID.

The command is idempotent. A changed target IP updates the existing machine
workspace instead of creating a second repository.

## Local credential handles

Do not place a recovered password in a prompt, command argument, environment
variable, checkpoint, report, or tool response. Keep the original evidence under
`loot/`, `scans/`, or `tmp/` and refer to it by path and literal prefix.

Example artifact:

```text
USER nathan
PASS recovered-value-is-local-only
```

Use it against the workspace's immutable target:

```bash
mcp-edge lab ssh-exec \
  --username nathan \
  --source loot/capture-0-strings.txt \
  --extract-after PASS \
  --command 'id'

# Add --port 2222 when SSH is exposed on a non-default port of the same target.
```

The command inside Bubblewrap is only a client for a private Unix-socket broker. The broker runs in the Edge process outside the model sandbox and owns the immutable workspace registration. It:

- accepts no target field and always connects to the registered target;
- revalidates the registered VPN route before every SSH operation;
- extracts exactly one value from a workspace-local artifact on the host side;
- stages it under the private Edge state root in a one-use askpass file that is not mounted into Bubblewrap;
- never puts the value in argv, environment, model turns, logs, or the VPS;
- permits one SSH password prompt;
- bounds time and output;
- can save sensitive stdout directly to the workspace and return only path, size, and SHA-256.

For flags or sensitive command output, keep content local:

```bash
mcp-edge lab ssh-exec \
  --username nathan \
  --source loot/capture-0-strings.txt \
  --extract-after PASS \
  --command 'cat /home/nathan/user.txt' \
  --save-output loot/user.txt
```

The model receives only target, username, byte count, SHA-256 digest, and saved
relative path. The operator may read the file directly on Parrot.

When a remote command legitimately needs the same password on stdin, add
`--password-stdin`. This is intended for a bounded command such as
`sudo -S -p '' -l`; it does not grant local sudo and still targets only the selected
machine.

## Checkpoint rules

The private workcell control file `/workspace/.mcp-devbox/current-state.md` may contain:

```text
- Credential handle: source=loot/capture-0-strings.txt prefix=PASS user=nathan
- user.txt: obtained; saved=loot/user.txt
```

It must not contain the credential or flag value. Before a checkpoint is rendered
into a model turn, MCP Devbox also redacts provider tokens, generic password fields,
and 32–64-character hexadecimal flag-like values.

## Remaining boundary

The workcell still shares the host network. Target locking is enforced for brokered
credential use and by the HTB preflight, not by a universal packet filter. Tools that
open arbitrary network connections must continue to follow the single-target local
contract. Rootless container authority remains limited to the Edge user's namespace.
