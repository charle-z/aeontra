(() => {
  "use strict";

  const byId = (id) => document.getElementById(id);
  const health = document.querySelector(".health");
  const healthLabel = byId("health-label");
  const lastUpdated = byId("last-updated");

  const setText = (id, value) => {
    const node = byId(id);
    if (node) {
      node.textContent = String(value ?? "—");
    }
  };

  const render = (status) => {
    setText("version", status.version);
    setText("commit", status.commit);
    setText("protocol", status.protocol_version);
    setText("tool-count", status.tool_count);
    setText("catalog-hash", status.catalog_hash);
    if (health) {
      health.dataset.state = status.status === "ok" ? "ok" : "error";
    }
    if (healthLabel) {
      healthLabel.textContent = status.status === "ok" ? "Runtime healthy" : "Runtime unavailable";
    }
    if (lastUpdated) {
      lastUpdated.textContent = `Updated ${new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
    }
  };

  const fail = () => {
    if (health) {
      health.dataset.state = "error";
    }
    if (healthLabel) {
      healthLabel.textContent = "Status unavailable";
    }
    if (lastUpdated) {
      lastUpdated.textContent = "Unable to refresh status";
    }
  };

  const refresh = async () => {
    try {
      const response = await fetch("/console/status", {
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
        headers: { Accept: "application/json" },
      });
      if (response.status === 401) {
        window.location.assign("/console");
        return;
      }
      if (!response.ok) {
        fail();
        return;
      }
      render(await response.json());
    } catch {
      fail();
    }
  };

  refresh();
  window.setInterval(refresh, 30000);
})();
