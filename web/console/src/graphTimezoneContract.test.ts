import { describe, expect, it } from "vitest";
import graphSource from "./GraphView.tsx?raw";
import shellSource from "./AppShell.tsx?raw";

describe("Graph and timestamp contract", () => {
  it("keeps all timezone conversion outside GraphView", () => {
    for (const forbidden of ["TimeDisplay", "Intl.DateTimeFormat", ".toLocale", "new Date("]) {
      expect(graphSource).not.toContain(forbidden);
    }
  });

  it("keeps every visible console timestamp on the shared Timestamp component", () => {
    for (const required of [
      "Timestamp value={payload?.process_started_at",
      "Timestamp value={window.updated_at",
      "Timestamp value={controller.last_seen_at",
      "Timestamp value={runtime.last_activity",
      "Timestamp value={task.created_at",
      "Timestamp value={task.updated_at",
      "Timestamp value={task.heartbeat_at",
      "Timestamp value={task.terminal_at",
      "Timestamp value={brain?.indexed_at",
      "Timestamp value={device.paired_at",
      "Timestamp value={data?.durable_activity.lifetime.updated_at",
      "Timestamp value={event.occurred_at",
    ]) {
      expect(shellSource).toContain(required);
    }
  });
});
