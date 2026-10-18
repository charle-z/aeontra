export type RuntimeStatus = {
  status: string;
  version: string;
  protocol_version: string;
  commit: string;
  tool_count: number;
  catalog_hash: string;
  authenticated: boolean;
  surface: string;
};

const runtimeStatusKeys = [
  "authenticated",
  "catalog_hash",
  "commit",
  "protocol_version",
  "status",
  "surface",
  "tool_count",
  "version",
] as const;

export function parseRuntimeStatus(value: unknown): RuntimeStatus {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("invalid runtime status");
  }
  const record = value as Record<string, unknown>;
  const keys = Object.keys(record).sort();
  if (keys.join("\n") !== [...runtimeStatusKeys].sort().join("\n")) {
    throw new Error("unexpected runtime status schema");
  }
  for (const key of ["status", "version", "protocol_version", "commit", "catalog_hash", "surface"] as const) {
    if (typeof record[key] !== "string") {
      throw new Error(`invalid runtime status field: ${key}`);
    }
  }
  if (typeof record.tool_count !== "number" || !Number.isInteger(record.tool_count) || record.tool_count < 0) {
    throw new Error("invalid runtime status field: tool_count");
  }
  if (typeof record.authenticated !== "boolean") {
    throw new Error("invalid runtime status field: authenticated");
  }
  return record as RuntimeStatus;
}
