# Workcell publication and runner activation — 2026-10-09

Status: image publication and real-host runner activation verified. Backend and
Edge source rollout are separate acceptance steps.

The protected sandbox-image-release run 37961724579 succeeded for
e73ccea716e518531737db013b4caea3a4a9d298. Registry response hashes, the
Linux/amd64 manifest and OCI config were independently checked before download.

| Identity | SHA-256 |
| --- | --- |
| Workcell index | a57995ced48e5d50c48b49934e078a4c865dba356e32a2f59ec26b7b27c15223 |
| Workcell amd64 manifest | e5368a7ff87330b4b3b42088a0927f9c5036d8e473c68b2ccc51ddf1d008186e |
| Workcell OCI config | 1106476e2a9fce831f1f5abd6aafd9cf9770b0771ba065934de4aa03a2444c3d |
| Runner index | bfeea265d2faf3665d3a68050d85fe6cd5ac688afd80069462c23a14879ecf17 |
| Runner amd64 manifest | c9683054a1275d42b0504171685cb3dff9147a8a5058fd02db16273e42f90c1a |
| Workcell recipe | 11ebaa13b523eac95e2471341e4289025571e903b5059446e3d61452b422111f |

The VPS rootless policy retained its default rejection and prior rules; only the
two exact workcell digest references were added. Private registry credentials
were used in memory, without login files or workcell credential injection.
Root-only policy and service backups were retained before changes.

A temporary intended-host probe ran as UID 10001 with no network, read-only root,
no capabilities, no-new-privileges, bounded CPU/memory/PIDs and temporary /tmp.
It reported Go 1.26.9, Python's loaded OpenSSL 4.0.3 and bundled
http-cache-semantics 4.3.0. Exit was zero; its container was removed.

After verifying no active rootless containers, one normal Coolify service restart
activated the exact runner index and workcell index above. Inspection confirmed
running/healthy, revision e73ccea716e518531737db013b4caea3a4a9d298,
UID 10001:10001, read-only root, no published ports, unchanged repository volume
and rootless socket bindings, and no GitHub, Coolify or public MCP token in the
runner environment.

The old inventory watch run 37967007622 correctly failed on the previously
deployed image's Python and Go findings. It was not suppressed. The inventory
now records the observed new image; the watch uses its strict High/Critical gate
without the historical bundled-npm risk exception. The corrected-zlib VEX,
database freshness validation and immutable identity checks remain unchanged.

Protected source CI, registry publication, actual runner activation, watch scan,
backend deployment and installed Edge acceptance are distinct facts. This
baseline does not claim completion of the latter three steps.
