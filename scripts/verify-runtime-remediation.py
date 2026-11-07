#!/usr/bin/env python3
"""Verify the exported final filesystem; never extract an image onto the host."""
import argparse
import datetime
import hashlib
import io
import json
import pathlib
import posixpath
import tarfile


def digest(data):
    return hashlib.sha256(data).hexdigest()


def cli_signature(data):
    return b"SQLite version %s" in data and b"sqlite> " in data


def inspect_archive(archive, canary):
    entries, hashes, captured, candidates = {}, {}, {}, []
    wanted = {"usr/lib/apk/db/installed", "usr/share/aeontra/security/glibc-backport.json"}
    options = {"fileobj": archive} if hasattr(archive, "read") else {"name": archive}
    with tarfile.open(mode="r:", **options) as tar:
        for member in tar:
            name = posixpath.normpath(member.name.removeprefix("./"))
            if name.startswith(("/", "../")) or name == ".." or name in entries:
                raise ValueError("invalid or duplicate rootfs member")
            entries[name] = member
            if len(entries) > 150000 or member.size > 1024**3:
                raise ValueError("rootfs inventory exceeds its budget")
            if not member.isfile():
                continue
            h, previous, first, second, body = hashlib.sha256(), b"", False, False, bytearray()
            with tar.extractfile(member) as f:
                while chunk := f.read(1024*1024):
                    h.update(chunk)
                    view = previous + chunk
                    first |= b"SQLite version %s" in view
                    second |= b"sqlite> " in view
                    previous = view[-64:]
                    if name in wanted:
                        body.extend(chunk)
                        if len(body) > 8*1024*1024:
                            raise ValueError("rootfs metadata exceeds its budget")
            hashes[name] = h.hexdigest()
            if hashes[name] == canary or (first and second):
                candidates.append(name)
            if name in wanted:
                captured[name] = bytes(body)
    if candidates:
        raise ValueError("SQLite shell component found; library-only classification forbidden")
    return entries, hashes, captured


def verify_renamed_control(canary_bytes):
    fixture = io.BytesIO()
    with tarfile.open(fileobj=fixture, mode="w") as tar:
        entry = tarfile.TarInfo("arbitrary/renamed-library.bin")
        entry.size = len(canary_bytes)
        entry.mode = 0o644
        tar.addfile(entry, io.BytesIO(canary_bytes))
    fixture.seek(0)
    try:
        inspect_archive(fixture, digest(canary_bytes))
    except ValueError as error:
        if str(error) == "SQLite shell component found; library-only classification forbidden":
            return
        raise
    raise ValueError("renamed SQLite shell control was not rejected")


def resolved_hash(path, entries, hashes):
    path = path.lstrip("/")
    for _ in range(40):
        parts = path.split("/")
        for i in range(1, len(parts)+1):
            prefix = "/".join(parts[:i])
            entry = entries.get(prefix)
            if entry and (entry.issym() or entry.islnk()):
                target = entry.linkname
                if entry.issym() and not target.startswith("/"):
                    target = posixpath.join(posixpath.dirname(prefix), target)
                path = posixpath.normpath(posixpath.join(target.lstrip("/"), *parts[i:]))
                if path.startswith("../"):
                    raise ValueError("rootfs link leaves the image")
                break
        else:
            return hashes.get(path)
    raise ValueError("rootfs link cycle")


def verify(args):
    policy_bytes = pathlib.Path(args.policy).read_bytes()
    policy = json.loads(policy_bytes)
    now = datetime.datetime.now(datetime.timezone.utc)
    if now >= datetime.datetime.fromisoformat(policy["expires_at"].replace("Z", "+00:00")):
        raise ValueError("temporary runtime remediation expired")
    canary_bytes = (pathlib.Path(args.control)/"sqlite3-control").read_bytes()
    if not cli_signature(canary_bytes):
        raise ValueError("official SQLite shell positive control not recognized")
    # A copied or renamed CLI has identical content and must remain detectable.
    verify_renamed_control(canary_bytes)
    entries, hashes, captured = inspect_archive(args.rootfs, digest(canary_bytes))
    marker_bytes = captured["usr/share/aeontra/security/glibc-backport.json"]
    marker = json.loads(marker_bytes)
    if marker["source"] != policy["glibc_source"] or marker["patch_sha256"] != policy["glibc_patch_sha256"] or not marker["abi_checked"] or not marker["intl_checked"]:
        raise ValueError("unreviewed libc build")
    required = {"/usr/lib/libc.so.6", "/usr/lib/libc.so-2.44", "/usr/lib/libc.a", "/usr/lib/libm.so-2.44", "/usr/include/features.h", "/usr/lib/ld-linux-x86-64.so.2"}
    if not required <= marker["files"].keys():
        raise ValueError("incomplete libc/loader/development manifest")
    for path, expected in marker["files"].items():
        if resolved_hash(path, entries, hashes) != expected:
            raise ValueError("installed libc file differs from the tested build: " + path)
    if resolved_hash('/lib64/ld-linux-x86-64.so.2', entries, hashes) != marker['files']['/usr/lib/ld-linux-x86-64.so.2']:
        raise ValueError('runtime loader path does not resolve to the tested loader')
    packages = captured["usr/lib/apk/db/installed"].decode().split("\n\n")
    versions = []
    for block in packages:
        fields = dict(line.split(":", 1) for line in block.splitlines() if ":" in line)
        if fields.get("P") == "sqlite":
            raise ValueError("SQLite CLI package is installed")
        if fields.get("P") == "sqlite-libs":
            versions.append(fields.get("V"))
    if versions != ["3.54.1-r0"]:
        raise ValueError("unexpected SQLite library identity")
    proof = {
        "schema_version": 1, "image_id": args.image_id,
        "definition_sha256": digest(pathlib.Path(args.recipe).read_bytes()),
        "policy_sha256": digest(policy_bytes), "observed_at": now.isoformat().replace("+00:00", "Z"),
        "glibc": {"source": marker["source"], "patch_sha256": marker["patch_sha256"], "manifest_sha256": digest(marker_bytes),
                  "files_verified": len(marker["files"]), "abi_checked": True, "regression_checked": True, "normal_checked": True},
        "sqlite": {"version": versions[0], "inventory_sha256": digest(json.dumps(hashes, sort_keys=True).encode()), "files_scanned": len(hashes),
                   "cli_absent": True, "renamed_cli_control": True, "canary_sha256": digest(canary_bytes)},
    }
    print(json.dumps(proof, sort_keys=True))


if __name__ == "__main__":
    p = argparse.ArgumentParser()
    for name in ("rootfs", "control", "image-id", "recipe", "policy"):
        p.add_argument("--"+name, required=True)
    verify(p.parse_args())
