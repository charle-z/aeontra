import { parseRuntimeStatus, type RuntimeStatus } from "./types";

export async function fetchRuntimeStatus(signal?: AbortSignal): Promise<RuntimeStatus> {
  const response = await fetch("/console/status", {
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
    headers: { Accept: "application/json" },
    signal,
  });
  if (response.status === 401) {
    window.location.assign("/console");
    throw new Error("unauthorized");
  }
  if (!response.ok) {
    throw new Error(`runtime status request failed: ${response.status}`);
  }
  return parseRuntimeStatus(await response.json());
}
