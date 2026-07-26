(function () {
  "use strict";

  var reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  var boot = document.getElementById("boot");
  var content = document.getElementById("content");
  var bootTimer = null;

  function enter() {
    if (!boot || boot.hidden) return;
    boot.hidden = true;
    if (bootTimer !== null) window.clearTimeout(bootTimer);
    if (content) content.focus({ preventScroll: true });
  }

  document.getElementById("skipBoot").addEventListener("click", enter);
  if (reduced) enter();
  else bootTimer = window.setTimeout(enter, 1400);

  document.addEventListener("keydown", function (event) {
    if (boot && !boot.hidden) {
      enter();
      return;
    }
    if (event.key === "Escape" && content) {
      content.scrollTo({ top: 0, behavior: reduced ? "auto" : "smooth" });
    }
  });

  var cases = {
    secret: ["DENIED", "denied", "Secret path and content checks reject the read before any bytes are returned.", "SECRET DENY — path and content checks are independent."],
    force: ["NO SUCH CAPABILITY", "denied", "The canonical catalog contains no force-publication operation.", "ABSENT BY CONSTRUCTION — no force-push tool exists."],
    outside: ["DENIED", "denied", "The requested effect is outside the repository jail and outside the command allowlist.", "PATH JAIL + ALLOWLIST — execution is jailed too."],
    shell: ["NO SUCH CAPABILITY", "denied", "A general shell is not part of the public tool contract.", "NO FREE SHELL — only fixed programs with validated arguments."],
    deploy: ["PLAN REQUIRED", "plan", "A deployment becomes an expiring, single-use plan awaiting owner approval.", "TTL PLAN — exact state is revalidated before execution."],
    credential: ["DENIED", "denied", "Trusted adapters can use credentials but cannot return their values.", "NO CREDENTIAL EXFILTRATION — output remains redacted."],
    read: ["ALLOWED", "allowed", "The path is jailed, bounded and scanned before content is returned.", "BOUNDED READ — ordinary repository work remains fast."],
    tests: ["ALLOWED", "allowed", "The canonical project test contract runs with timeout and output bounds.", "BOUNDED EXECUTION — the project cannot enlarge its profile."]
  };

  var buttons = Array.prototype.slice.call(document.querySelectorAll("[data-policy-id]"));
  var stamp = document.getElementById("policyStamp");
  var output = document.getElementById("policyOutput");
  var rule = document.getElementById("policyRule");
  var typing = null;

  function typeText(text) {
    if (typing !== null) window.clearInterval(typing);
    if (reduced) {
      output.textContent = text;
      return;
    }
    output.textContent = "";
    var index = 0;
    typing = window.setInterval(function () {
      index += 1;
      output.textContent = text.slice(0, index);
      if (index >= text.length) {
        window.clearInterval(typing);
        typing = null;
      }
    }, 8);
  }

  buttons.forEach(function (button) {
    button.addEventListener("click", function () {
      var selected = cases[button.getAttribute("data-policy-id")];
      if (!selected) return;
      buttons.forEach(function (candidate) {
        candidate.setAttribute("aria-pressed", "false");
      });
      button.setAttribute("aria-pressed", "true");
      stamp.className = "stamp " + selected[1];
      stamp.textContent = selected[0];
      rule.textContent = selected[3];
      typeText(selected[2]);
    });
  });

  var fields = {
    status: document.getElementById("runtimeStatus"),
    version: document.getElementById("runtimeVersion"),
    commit: document.getElementById("runtimeCommit"),
    built_at: document.getElementById("runtimeBuiltAt"),
    protocol_version: document.getElementById("runtimeProtocol"),
    tool_count: document.getElementById("runtimeToolCount"),
    catalog_hash: document.getElementById("runtimeCatalogHash")
  };
  var message = document.getElementById("runtimeMessage");
  var topStatus = document.getElementById("topStatus");
  var topCommit = document.getElementById("topCommit");

  function valid(payload) {
    return payload && typeof payload.status === "string" &&
      typeof payload.version === "string" && typeof payload.commit === "string" &&
      typeof payload.built_at === "string" && typeof payload.protocol_version === "string" &&
      Number.isInteger(payload.tool_count) && payload.tool_count > 0 &&
      typeof payload.catalog_hash === "string" && payload.catalog_hash.indexOf("sha256:") === 0;
  }

  function unavailable() {
    fields.status.textContent = "unavailable";
    fields.status.className = "warn";
    topStatus.textContent = "unavailable";
    topStatus.className = "warn";
    topCommit.textContent = "identity";
    message.textContent = "Public identity temporarily unavailable. Use /healthz or /version to retry; no private diagnostic detail is exposed here.";
  }

  fetch("/version", {
    method: "GET",
    credentials: "omit",
    cache: "no-store",
    redirect: "error",
    headers: { Accept: "application/json" }
  }).then(function (response) {
    if (!response.ok) throw new Error("unavailable");
    return response.json();
  }).then(function (payload) {
    if (!valid(payload)) throw new Error("invalid");
    Object.keys(fields).forEach(function (key) {
      fields[key].textContent = String(payload[key]);
    });
    fields.status.className = "ok";
    topStatus.textContent = payload.status;
    topStatus.className = "ok";
    topCommit.textContent = payload.commit.length > 12 ? payload.commit.slice(0, 12) : payload.commit;
    message.textContent = "Identity loaded from the public, allowlisted runtime contract.";
  }).catch(unavailable);

  var sections = Array.prototype.slice.call(document.querySelectorAll("main section"));
  var sectionLabel = document.getElementById("sectionLabel");
  var sectionHint = document.getElementById("sectionHint");

  function showSection(section) {
    if (!section) return;
    sectionLabel.textContent = section.id.toUpperCase();
    sectionHint.textContent = section.getAttribute("data-hint") || "";
  }

  if ("IntersectionObserver" in window && sections.length) {
    var observer = new IntersectionObserver(function (entries) {
      var visible = entries.filter(function (entry) { return entry.isIntersecting; });
      visible.sort(function (left, right) { return right.intersectionRatio - left.intersectionRatio; });
      if (visible.length) showSection(visible[0].target);
    }, { root: content, threshold: [0.25, 0.5, 0.75] });
    sections.forEach(function (section) { observer.observe(section); });
  } else if (sections.length) {
    showSection(sections[0]);
  }
})();
