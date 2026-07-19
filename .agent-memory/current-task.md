# Current task — HTB lab autonomy without operator-secret exposure

Branch: `htb-lab-authorized-credentials`
Base: `origin/main` at `f501064597b750533010ad706249f9447c07d6f2`.

Historical deployed foundations preserved:
- P8.1 Console 2.0 is deployed and tagged `p8.1` at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P9 Brain is deployed at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.
- P11.2 Remote OpenCode Relay remains the sandbox and relay foundation.
- P12 Trusted Linux Workcell and Parrot onboarding hardening are deployed at `f501064597b750533010ad706249f9447c07d6f2`.
- Production remains at 85 tools with the documented catalog hash.

Real Cap runtime `mr_82813d7f90db44bac2d79c2b693a5ec6` proved the HTB workcell could route over `tun0`, enumerate the target, download a PCAP and identify a valid FTP credential, but the remote model/tool bridge would not place the recovered password into SSH or FTP tool arguments. The runtime stopped before user access.

Fix the product rather than disabling global secret protections:

1. Add an idempotent `mcp-edge lab init` command that creates the Git workspace, README, registration, `htb-linux` metadata, VPN route validation and tool inventory in one operator command.
2. Align the default state root with the packaged service (`$XDG_STATE_HOME/mcp-edge` or `$HOME/.local/state/mcp-edge`) while preserving a safe legacy fallback.
3. Add a target-locked local `mcp-edge lab ssh-exec` helper available only inside an active `htb-linux` runtime. It must extract one password from an existing workspace artifact by a literal prefix, use it through SSH_ASKPASS without putting the password in argv, environment, logs or the model turn, connect only to the immutable `TARGET`, and execute one bounded remote command.
4. Add optional local output capture so later credentials/loot can remain in the workspace without crossing the bridge.
5. Export machine/VPN metadata into the Bubblewrap environment and document the helper in the rendered HTB contract.
6. Preserve global bans on operator secrets, host credentials, other targets, CIDRs, rootful Docker, Windows mounts and Internet solution lookup.
7. Add regression tests for state selection, lab init idempotency, target locking, source-file jail/symlink rejection, unique extraction, askpass secrecy and bounded SSH invocation.

Do not merge or deploy until exact-head CI is green and a real Parrot/Cap continuation proves the credential path.
