import { expect, test, type Page, type TestInfo } from "@playwright/test";

type Box = { x: number; y: number; width: number; height: number };

const runtime = {
  status: "ok",
  version: "1.0.0",
  protocol_version: "2025-06-18",
  commit: "681f55cf20853fa8365485b7f9343953eaaceae8",
  tool_count: 92,
  catalog_hash: "sha256:ea9cc3749c68fcc12b608efbddc259b01eb7868c98bbc1ab35c75f456e118a98",
  authenticated: true,
  surface: "presentation-only",
};

function safeNodes(count = 36) {
  return Array.from({ length: count }, (_, index) => ({
    id: `bn_${index.toString(16).padStart(24, "0")}`,
    console_label: index === 0 ? "P11.2 Relay" : index === 1 ? "OpenCode Provider" : index === 2 ? "Parrot Install" : `Safe Node ${index}`,
    title: index === 0
      ? "P11.2 Remote OpenCode relay production closure"
      : `Complete safe Brain node title ${index}`,
    summary: index === 0
      ? "Production closure validated with bounded safe console metadata."
      : index === 3 ? "" : `Curated safe summary ${index}.`,
    trust: index % 3 === 0 ? "curated" : "working",
    degree: index === 0 ? 8 : Math.max(1, 7 - (index % 7)),
  }));
}

function safeData() {
  const nodes = safeNodes();
  const edges = nodes.slice(1).map((node, index) => ({
    source: index < 8 ? nodes[0].id : nodes[index].id,
    target: node.id,
  }));
  return {
    schema_version: 4,
    system: {
      available: true,
      cpu_count: 2,
      memory_total_bytes: 4_294_967_296,
      memory_available_bytes: 2_147_483_648,
      disk_total_bytes: 85_899_345_920,
      disk_available_bytes: 42_949_672_960,
      load_1: 0.1,
      load_5: 0.2,
      load_15: 0.3,
    },
    payload: {
      process_started_at: "2026-07-17T19:00:00Z",
      tool_call_count: 4,
      estimated_payload_tokens: 1536,
      warning: "estimate, not provider billing",
      request_count: 8,
      input_bytes: 4096,
      output_bytes: 2048,
      input_tokens_estimate: 1024,
      output_tokens_estimate: 512,
      formula: "bytes / 4 (estimate)",
    },
    durable_activity: Object.fromEntries([
      "last_24_hours", "last_7_days", "last_30_days", "last_90_days", "lifetime",
    ].map((key) => [key, {
      requests: 8,
      tool_calls: 4,
      input_bytes: 4096,
      output_bytes: 2048,
      estimated_payload_tokens: 1536,
      client_errors: 0,
      server_errors: 0,
      external_wait_ms: 0,
      updated_at: "2026-07-17T19:32:10Z",
    }])),
    controllers: [{ kind: "http", state: "connected", last_seen_at: "2026-07-17T19:32:10Z", active_operations: 1, active_runtimes: 1 }],
    runtimes: [{ runtime_id: "mr_0123456789abcdef0123456789abcdef", state: "awaiting_model", controller: "http", edge_id: "", last_activity: "2026-07-17T19:32:10Z" }],
    projects: [{ id: "prj_0123456789abcdef01234567", label: "Current project", current: true }],
    storage: { available: true, database_bytes: 1024, wal_bytes: 0, log_bytes: 0, total_bytes: 1024, limit_bytes: 268_435_456, state: "healthy" },
    brain: {
      available: true,
      ready: true,
      schema_version: 1,
      note_count: nodes.length,
      source_bytes: 12_000,
      link_count: edges.length,
      broken_link_count: 0,
      indexed_at: "2026-07-17T19:32:10Z",
      graph_truncated: false,
      nodes,
      edges,
    },
    observability: { enabled: true, failures: 0, routes: [{ route: "console", requests: 8, client_4xx: 0, server_5xx: 0, p95_ms: 12 }] },
    security: {
      oauth_enabled: true,
      bearer_recovery: true,
      query_auth: "rejected",
      free_shell: "absent",
      cookie: "Secure; HttpOnly; SameSite=Strict",
      console_authority: "presentation-only",
    },
    edge: { state: "not_paired", devices: [] },
  };
}

const emptyJournal = {
  schema_version: 2,
  available: true,
  storage: { storage: "healthy", detail: "durable", record_count: 0, database_size_bytes: 0, wal_size_bytes: 0 },
  tasks: [],
  next_cursor: "",
  has_more: false,
};

const emptyEvents = {
  schema_version: 1,
  available: true,
  storage: { storage: "healthy", detail: "durable", record_count: 0, database_size_bytes: 0, wal_size_bytes: 0 },
  events: [],
  next_cursor: "",
  has_more: false,
};

async function boot(page: Page): Promise<void> {
  await page.addInitScript(() => {
    Object.defineProperty(window, "EventSource", { value: undefined, configurable: true });
  });
  await page.route("**/console/preferences", async (route) => {
    await route.fulfill({ json: { timezone: "America/Bogota" } });
  });
  await page.route("**/console/status", async (route) => route.fulfill({ json: runtime }));
  await page.route("**/console/data", async (route) => route.fulfill({ json: safeData() }));
  await page.route("**/console/tasks?**", async (route) => route.fulfill({ json: emptyJournal }));
  await page.route("**/console/event-log?**", async (route) => route.fulfill({ json: emptyEvents }));
  await page.goto("/");
  await page.getByRole("tab", { name: "Graph" }).click();
  await expect(page.locator('[data-graph-node="true"]')).toHaveCount(36);
}

function overlaps(left: Box, right: Box, padding = 0.5): boolean {
  return left.x < right.x + right.width + padding
    && left.x + left.width + padding > right.x
    && left.y < right.y + right.height + padding
    && left.y + left.height + padding > right.y;
}

async function verifyCollisionContract(page: Page): Promise<void> {
  const snapshot = await page.evaluate(() => {
    const toBox = (element: Element) => {
      const rect = element.getBoundingClientRect();
      return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
    };
    const controls = document.querySelector('[role="group"][aria-label="Graph controls"]');
    const detail = document.querySelector("#graph-detail-panel");
    if (!controls || !detail) throw new Error("graph controls or detail panel missing");
    return {
      controls: toBox(controls),
      detail: toBox(detail),
      labels: Array.from(document.querySelectorAll('[data-graph-label="true"]')).map((element) => ({
        nodeId: element.getAttribute("data-node-id") ?? "",
        box: toBox(element),
      })),
      nodes: Array.from(document.querySelectorAll('[data-graph-node="true"]')).map((element) => {
        const visual = element.querySelector(".graph-node-visual");
        if (!visual) throw new Error("graph node visual missing");
        return { nodeId: element.getAttribute("data-node-id") ?? "", box: toBox(visual) };
      }),
    };
  });

  for (const [index, label] of snapshot.labels.entries()) {
    expect(label.nodeId).not.toBe("");
    expect(label.box.width).toBeGreaterThan(0);
    expect(label.box.height).toBeGreaterThan(0);
    expect(overlaps(label.box, snapshot.controls)).toBe(false);
    expect(overlaps(label.box, snapshot.detail)).toBe(false);
    for (const prior of snapshot.labels.slice(0, index)) expect(overlaps(label.box, prior.box)).toBe(false);
    for (const node of snapshot.nodes) {
      if (node.nodeId === label.nodeId) continue;
      expect(overlaps(label.box, node.box)).toBe(false);
    }
  }
}

async function saveArtifact(page: Page, testInfo: TestInfo): Promise<void> {
  const path = testInfo.outputPath(`${testInfo.project.name}-brain-graph.png`);
  await page.screenshot({ path, fullPage: true });
  await testInfo.attach("brain-graph-screenshot", { path, contentType: "image/png" });
}

test("responsive graph has collision-free labels and bounded selection", async ({ page }, testInfo) => {
  await boot(page);
  await verifyCollisionContract(page);

  const selected = page.getByRole("button", { name: /P11\.2 Remote OpenCode relay production closure/ });
  if (testInfo.project.use.hasTouch) await selected.tap();
  else await selected.click();
  await expect(selected).toHaveAttribute("aria-pressed", "true");
  await expect(selected.locator(".graph-node-halo")).toHaveCount(1);
  await expect(selected.locator("rect")).toHaveCount(0);
  await expect(page.locator("#graph-detail-panel")).toContainText("P11.2 Relay");
  await expect(page.locator("#graph-detail-panel")).toContainText("P11.2 Remote OpenCode relay production closure");
  await expect(page.locator("#graph-detail-panel")).toContainText("Production closure validated with bounded safe console metadata.");
  await expect(page.locator("#graph-detail-panel")).toContainText("selected");
  await expect(page.locator(".graph-edge-related")).not.toHaveCount(0);
  await expect(page.locator(".graph-edge-muted")).not.toHaveCount(0);
  await verifyCollisionContract(page);

  const horizontalOverflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
  expect(horizontalOverflow).toBe(false);
  await saveArtifact(page, testInfo);
});

test("zoom density, keyboard and touch interaction remain stable", async ({ page }, testInfo) => {
  await boot(page);
  const target = page.getByRole("button", { name: /P11\.2 Remote OpenCode relay production closure/ });
  const initialTransform = await target.getAttribute("transform");

  await page.getByRole("button", { name: "Zoom out" }).click();
  await page.getByRole("button", { name: "Zoom out" }).click();
  await expect(page.locator(".graph-canvas")).toHaveAttribute("data-zoom-level", "far");
  const farCapacity = Number(await page.locator(".graph-canvas").getAttribute("data-label-capacity"));
  expect(farCapacity).toBeGreaterThan(0);

  await page.getByRole("button", { name: "Reset graph" }).click();
  await page.getByRole("button", { name: "Zoom in" }).click();
  await page.getByRole("button", { name: "Zoom in" }).click();
  await page.getByRole("button", { name: "Zoom in" }).click();
  await expect(page.locator(".graph-canvas")).toHaveAttribute("data-zoom-level", "near");
  const nearCapacity = Number(await page.locator(".graph-canvas").getAttribute("data-label-capacity"));
  expect(nearCapacity).toBeGreaterThan(farCapacity);
  await verifyCollisionContract(page);

  if (testInfo.project.use.hasTouch) {
    await target.tap();
    await expect(target).toHaveAttribute("aria-pressed", "true");
    await target.tap();
    await expect(page.locator("#graph-detail-panel")).toBeFocused();
  } else {
    await target.focus();
    await page.keyboard.press("Enter");
    await expect(target).toHaveAttribute("aria-pressed", "true");
    await page.keyboard.press("Escape");
    await expect(target).toHaveAttribute("aria-pressed", "false");
    await page.keyboard.press("Space");
    await expect(target).toHaveAttribute("aria-pressed", "true");
  }

  await page.getByRole("button", { name: "Reset graph" }).click();
  expect(await target.getAttribute("transform")).toBe(initialTransform);
  await verifyCollisionContract(page);
});
