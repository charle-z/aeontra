# Rootless client cold-runtime regression

The packaged Edge keeps the host user runtime read-only. Remote Podman may initialize
`libpod` there before contacting its socket. Host CLI use can mask that cold-start
failure. The client scratch lifecycle is defined in [configuration](../configuration.md)
and the authority boundary in [security](../security.md).

Engine commands select Podman's remote mode explicitly with `--remote --url`.
Podman 3.4 does not infer remote mode from `--url` alone. Changing client scratch
without selecting remote mode can initialize local engine state and break `exec`.
The tagged rootless CI exercises build, Compose, exec and cleanup on Podman 3.4;
the cold-runtime acceptance below exercises the installed host version.

Ordinary regression tests:

```sh
go test ./internal/edgeclient -run 'TestExecContainerCommandRunner|TestRootlessContainerClientEnvironment' -count=1
```

Real acceptance requires Linux, Bubblewrap, a running user-owned Podman socket and a
non-root engine owner. Compile the test binary as that owner, then run the following
operator-only fixture. It creates a disposable mount namespace with a cold read-only
user runtime, preserving the existing socket. It neither stops containers nor deletes
host runtime directories. Review the resolved owner, UID and socket before using sudo.

```sh
uid=$(id -u)
gid=$(id -g)
owner_home=$HOME
binary=$(mktemp /tmp/aeontra-cold-runtime-test.XXXXXX)
trap 'rm -f -- "$binary"' EXIT
go test -c -o "$binary" ./internal/edgeclient
sudo bwrap --ro-bind / / --dev /dev --proc /proc \
  --tmpfs "/run/user/$uid" \
  --ro-bind "/run/user/$uid/podman" "/run/user/$uid/podman" \
  --remount-ro "/run/user/$uid" \
  --tmpfs /tmp --chmod 1777 /tmp --ro-bind "$binary" /tmp/client-test \
  --die-with-parent setpriv --reuid="$uid" --regid="$gid" --init-groups \
  env -i HOME="$owner_home" PATH=/usr/bin:/bin \
  MCP_DEVBOX_TEST_COLD_CONTAINER_RUNTIME=1 \
  /tmp/client-test -test.run=TestRootlessContainerColdRuntimeE2E -test.v -test.timeout=30s
```

PASS requires the unmodified client environment to reproduce a read-only failure,
the managed command runner to list the existing toolbox inventory successfully, and
the host runtime to remain unpopulated. This acceptance is separate from CI: the
ordinary suite skips the host-specific test unless explicitly enabled.

After a signed update, verify the installed release and query the original toolbox
through `project_toolbox_status`. Preserve its identity and generation. Correlate any
failure with the bounded `toolbox inspection failed` journal entry; do not recreate a
toolbox to make a status check pass. The source fix can be rolled back through the
normal signed Edge rollback; it does not migrate toolbox records or service policy.
