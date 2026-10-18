(function () {
  "use strict";

  var reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  var currentLanguage = (navigator.language || "").toLowerCase().indexOf("es") === 0 ? "es" : "en";
  var boot = document.getElementById("boot");
  var machine = document.getElementById("machine");
  var content = document.getElementById("content");
  var skipBoot = document.getElementById("skipBoot");
  var bootTimer = null;

  function isolateBoot(active) {
    if (!machine) return;
    if (active) {
      machine.setAttribute("inert", "");
      machine.setAttribute("aria-hidden", "true");
    } else {
      machine.removeAttribute("inert");
      machine.removeAttribute("aria-hidden");
    }
  }

  function enter() {
    if (!boot || boot.hidden) return;
    boot.hidden = true;
    if (bootTimer !== null) window.clearTimeout(bootTimer);
    isolateBoot(false);
    if (content) content.focus({ preventScroll: true });
  }

  if (skipBoot) skipBoot.addEventListener("click", enter);
  if (reduced) {
    enter();
  } else {
    isolateBoot(true);
    if (skipBoot) skipBoot.focus({ preventScroll: true });
    bootTimer = window.setTimeout(enter, 1400);
  }

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

  var policyTranslations = {
    secret: { explanation: { es: "Las comprobaciones de ruta y contenido secreto rechazan la lectura antes de devolver bytes.", en: "Secret path and content checks reject the read before any bytes are returned." }, rule: { es: "SECRET DENY — las comprobaciones de ruta y contenido son independientes.", en: "SECRET DENY — path and content checks are independent." } },
    force: { explanation: { es: "El catálogo canónico no contiene ninguna operación de publicación forzada.", en: "The canonical catalog contains no force-publication operation." }, rule: { es: "ABSENT BY CONSTRUCTION — no existe una herramienta de force push.", en: "ABSENT BY CONSTRUCTION — no force-push tool exists." } },
    outside: { explanation: { es: "El efecto solicitado está fuera de la jaula del repositorio y de la allowlist de comandos.", en: "The requested effect is outside the repository jail and outside the command allowlist." }, rule: { es: "PATH JAIL + ALLOWLIST — la ejecución también está confinada.", en: "PATH JAIL + ALLOWLIST — execution is jailed too." } },
    shell: { explanation: { es: "Un shell general no forma parte del contrato público de herramientas.", en: "A general shell is not part of the public tool contract." }, rule: { es: "NO FREE SHELL — solo programas fijos con argumentos validados.", en: "NO FREE SHELL — only fixed programs with validated arguments." } },
    deploy: { explanation: { es: "Un despliegue se convierte en un plan expirable y de un solo uso que espera aprobación del propietario.", en: "A deployment becomes an expiring, single-use plan awaiting owner approval." }, rule: { es: "TTL PLAN — el estado exacto se revalida antes de ejecutar.", en: "TTL PLAN — exact state is revalidated before execution." } },
    credential: { explanation: { es: "Los adaptadores confiables pueden usar credenciales, pero no devolver sus valores.", en: "Trusted adapters can use credentials but cannot return their values." }, rule: { es: "NO CREDENTIAL EXFILTRATION — la salida permanece redactada.", en: "NO CREDENTIAL EXFILTRATION — output remains redacted." } },
    read: { explanation: { es: "La ruta está confinada, acotada y escaneada antes de devolver contenido.", en: "The path is jailed, bounded and scanned before content is returned." }, rule: { es: "BOUNDED READ — el trabajo ordinario del repositorio sigue siendo directo.", en: "BOUNDED READ — ordinary repository work remains fast." } },
    tests: { explanation: { es: "El contrato canónico de pruebas se ejecuta con tiempo y salida acotados.", en: "The canonical project test contract runs with timeout and output bounds." }, rule: { es: "BOUNDED EXECUTION — el proyecto no puede ampliar su perfil.", en: "BOUNDED EXECUTION — the project cannot enlarge its profile." } }
  };

  var buttons = Array.prototype.slice.call(document.querySelectorAll("[data-policy-id]"));
  var stamp = document.getElementById("policyStamp");
  var output = document.getElementById("policyOutput");
  var rule = document.getElementById("policyRule");
  var typing = null;
  var activePolicyID = "";

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

  function renderPolicy(policyID) {
    var selected = cases[policyID];
    var translation = policyTranslations[policyID];
    if (!selected || !translation) return;
    activePolicyID = policyID;
    buttons.forEach(function (candidate) {
      candidate.setAttribute("aria-pressed", candidate.getAttribute("data-policy-id") === policyID ? "true" : "false");
    });
    stamp.className = "stamp " + selected[1];
    stamp.textContent = selected[0];
    rule.textContent = translation.rule[currentLanguage];
    var explanation = translation.explanation[currentLanguage];
    typeText(explanation);
  }

  buttons.forEach(function (button) {
    button.addEventListener("click", function () {
      renderPolicy(button.getAttribute("data-policy-id"));
    });
  });

  var modeButtons = Array.prototype.slice.call(document.querySelectorAll("[data-mode-id]"));
  var modePanels = Array.prototype.slice.call(document.querySelectorAll("[data-mode-panel]"));

  function selectMode(modeID, moveFocus) {
    var selectedButton = null;
    modeButtons.forEach(function (button) {
      var selected = button.getAttribute("data-mode-id") === modeID;
      button.setAttribute("aria-selected", selected ? "true" : "false");
      button.setAttribute("tabindex", selected ? "0" : "-1");
      if (selected) selectedButton = button;
    });
    modePanels.forEach(function (panel) {
      panel.hidden = panel.getAttribute("data-mode-panel") !== modeID;
    });
    if (moveFocus && selectedButton) selectedButton.focus();
  }

  modeButtons.forEach(function (button, index) {
    button.addEventListener("click", function () {
      selectMode(button.getAttribute("data-mode-id"), false);
    });
    button.addEventListener("keydown", function (event) {
      var next = index;
      if (event.key === "ArrowRight" || event.key === "ArrowDown") next = (index + 1) % modeButtons.length;
      else if (event.key === "ArrowLeft" || event.key === "ArrowUp") next = (index + modeButtons.length - 1) % modeButtons.length;
      else if (event.key === "Home") next = 0;
      else if (event.key === "End") next = modeButtons.length - 1;
      else return;
      event.preventDefault();
      selectMode(modeButtons[next].getAttribute("data-mode-id"), true);
    });
  });
  if (modeButtons.length && modePanels.length) selectMode("read-only", false);

  var demoSection = document.getElementById("demo");
  var demoStatus = document.getElementById("demoStatus");
  var demoMessageKey = "loading";
  var demoMessages = {
    loading: {
      es: "Cargando el manifiesto público integrado...",
      en: "Loading the embedded public manifest..."
    },
    available: {
      es: "Evidencia pública cargada. Recorrido de solo lectura listo.",
      en: "Public evidence loaded. Read-only walkthrough ready."
    },
    unavailable: {
      es: "La evidencia guiada no está disponible temporalmente. La página no intentará consultar GitHub ni mostrar diagnósticos privados.",
      en: "The guided evidence is temporarily unavailable. The page will not query GitHub or expose private diagnostics."
    }
  };

  function setDemoMessage(key) {
    demoMessageKey = key;
    if (!demoStatus) return;
    demoStatus.textContent = demoMessages[key][currentLanguage];
    demoStatus.className = "demo-status " + (key === "available" ? "ok" : "warn");
    demoStatus.setAttribute("data-demo-state", key);
    if (demoSection) demoSection.setAttribute("aria-busy", key === "loading" ? "true" : "false");
  }

  function demoNode(id) {
    return document.getElementById(id);
  }

  function setDemoText(id, value) {
    var node = demoNode(id);
    if (node) node.textContent = String(value);
  }

  function setDemoBilingualText(id, es, en) {
    var node = demoNode(id);
    if (!node) return;
    node.setAttribute("data-es", es);
    node.setAttribute("data-en", en);
    node.textContent = currentLanguage === "es" ? es : en;
  }

  function clearDemoNode(node) {
    if (!node) return;
    while (node.firstChild) node.removeChild(node.firstChild);
  }

  function setBilingualNode(node, es, en) {
    node.setAttribute("data-es", es);
    node.setAttribute("data-en", en);
    node.textContent = currentLanguage === "es" ? es : en;
  }

  function safeEvidenceURL(value) {
    if (typeof value !== "string") return null;
    try {
      var parsed = new URL(value);
      if (parsed.protocol !== "https:" || parsed.username || parsed.password) return null;
      if (parsed.hostname === "github.com" &&
          (parsed.pathname === "/charle-z/pixelgrama" || parsed.pathname.indexOf("/charle-z/pixelgrama/") === 0)) return parsed.href;
      if (parsed.hostname === "pixelgrama.mcp-devbox-charlez.duckdns.org") return parsed.href;
      return null;
    } catch (_) {
      return null;
    }
  }

  function validEvidenceSHA(value) {
    return typeof value === "string" && /^[0-9a-f]{40}$/.test(value);
  }

  function validEvidence(payload) {
    if (!payload || payload.schema_version !== 1) return false;
    if (!payload.project || payload.project.name !== "Pixelgrama" ||
        payload.project.repository !== "https://github.com/charle-z/pixelgrama" ||
        payload.project.base_branch !== "main" || typeof payload.project.request_summary !== "string") return false;
    if (!safeEvidenceURL(payload.project.repository) || !safeEvidenceURL(payload.project.production_url) ||
        !safeEvidenceURL(payload.project.primary_public_route) || !safeEvidenceURL(payload.project.version_url)) return false;
    if (!payload.historical_execution || !Array.isArray(payload.historical_execution.pull_requests) ||
        payload.historical_execution.pull_requests.length === 0 || !Array.isArray(payload.historical_execution.source_commits) ||
        payload.historical_execution.source_commits.length === 0) return false;
    var pullRequestsValid = payload.historical_execution.pull_requests.every(function (pullRequest) {
      return pullRequest && Number.isInteger(pullRequest.number) && pullRequest.number > 0 &&
        safeEvidenceURL(pullRequest.url) && validEvidenceSHA(pullRequest.head_sha) &&
        typeof pullRequest.purpose === "string" && pullRequest.purpose.length > 0 &&
        Array.isArray(pullRequest.checks) && pullRequest.checks.length > 0 &&
        pullRequest.checks.every(function (check) {
          return check && typeof check.name === "string" && check.conclusion === "success" && safeEvidenceURL(check.url);
        });
    });
    if (!pullRequestsValid) return false;
    var production = payload.production_observation;
    if (!production || !validEvidenceSHA(production.observed_commit) || !validEvidenceSHA(production.source_main_commit) ||
        production.observed_commit !== production.source_main_commit || production.matches_source_main !== true ||
        typeof production.verified_on !== "string" || !Array.isArray(production.routes)) return false;
    var requiredRoutes = ["/", "/wall", "/version"];
    if (!requiredRoutes.every(function (path) {
      return production.routes.some(function (route) {
        return route && route.path === path && route.observed_status === 200 && safeEvidenceURL(route.final_url);
      });
    })) return false;
    if (!production.infrastructure || production.infrastructure.provider !== "CubePath" || production.infrastructure.platform !== "Coolify") return false;
    if (!payload.authority_posture || payload.authority_posture.status !== "not_publicly_verified" ||
        typeof payload.authority_posture.statement !== "string") return false;
    if (!payload.operations || !Array.isArray(payload.operations.direct) || !Array.isArray(payload.operations.plan_protected) ||
        payload.operations.direct.length === 0 || payload.operations.plan_protected.length === 0) return false;
    var operationsValid = payload.operations.direct.concat(payload.operations.plan_protected).every(function (operation) {
      return operation && typeof operation.name === "string" && operation.name.length > 0 &&
        typeof operation.verification === "string" && operation.verification.length > 0 &&
        safeEvidenceURL(operation.evidence_url);
    });
    if (!operationsValid) return false;
    if (!payload.perimeter || !Array.isArray(payload.perimeter.includes) || !Array.isArray(payload.perimeter.excludes) ||
        payload.perimeter.includes.length === 0 || payload.perimeter.excludes.length === 0 ||
        !payload.perimeter.includes.concat(payload.perimeter.excludes).every(function (entry) {
          return typeof entry === "string" && entry.trim().length > 0;
        })) return false;
    return true;
  }

  function setDemoLink(id, url, label) {
    var node = demoNode(id);
    var safe = safeEvidenceURL(url);
    if (!node || !safe) return;
    node.setAttribute("href", safe);
    node.textContent = label;
  }

  function appendDemoLink(parent, url, es, en) {
    var safe = safeEvidenceURL(url);
    if (!safe) return;
    var link = document.createElement("a");
    link.setAttribute("href", safe);
    setBilingualNode(link, es, en);
    parent.appendChild(link);
  }

  function renderDemoStringList(id, values) {
    var list = demoNode(id);
    clearDemoNode(list);
    values.forEach(function (value) {
      var item = document.createElement("li");
      item.textContent = String(value);
      list.appendChild(item);
    });
  }

  function renderDemoPullRequests(pullRequests) {
    var list = demoNode("demoPullRequests");
    clearDemoNode(list);
    pullRequests.forEach(function (pullRequest) {
      var item = document.createElement("li");
      var title = document.createElement("span");
      title.className = "demo-record-title";
      title.textContent = "PR #" + pullRequest.number + " — " + pullRequest.purpose;
      item.appendChild(title);

      var meta = document.createElement("span");
      meta.className = "demo-record-meta";
      meta.textContent = "head " + pullRequest.head_sha;
      item.appendChild(meta);

      var links = document.createElement("span");
      links.className = "demo-record-links";
      appendDemoLink(links, pullRequest.url, "Abrir PR", "Open PR");
      appendDemoLink(links, pullRequest.url + "/files", "Ver archivos modificados", "View changed files");
      item.appendChild(links);
      list.appendChild(item);
    });
  }

  function renderDemoChecks(pullRequests) {
    var list = demoNode("demoChecks");
    clearDemoNode(list);
    var count = 0;
    pullRequests.forEach(function (pullRequest) {
      pullRequest.checks.forEach(function (check) {
        count += 1;
        var item = document.createElement("li");
        var title = document.createElement("span");
        title.className = "demo-record-title ok";
        title.textContent = "PR #" + pullRequest.number + " · " + check.name + " · SUCCESS";
        item.appendChild(title);
        var links = document.createElement("span");
        links.className = "demo-record-links";
        appendDemoLink(links, check.url, "Abrir check", "Open check");
        item.appendChild(links);
        list.appendChild(item);
      });
    });
    setDemoBilingualText("demoValidationSummary",
      pullRequests.length + " PR públicos · " + count + " checks exitosos enlazados.",
      pullRequests.length + " public PRs · " + count + " successful checks linked.");
  }

  function renderDemoOperations(id, operations) {
    var list = demoNode(id);
    clearDemoNode(list);
    operations.forEach(function (operation) {
      var item = document.createElement("li");
      var title = document.createElement("span");
      title.className = "demo-record-title";
      title.textContent = operation.name;
      item.appendChild(title);
      var meta = document.createElement("span");
      meta.className = "demo-record-meta";
      meta.textContent = operation.verification;
      item.appendChild(meta);
      var links = document.createElement("span");
      links.className = "demo-record-links";
      appendDemoLink(links, operation.evidence_url, "Abrir resultado público", "Open public result");
      item.appendChild(links);
      list.appendChild(item);
    });
  }

  function renderDemoEvidence(payload) {
    setDemoText("demoRequestSummary", payload.project.request_summary);
    setDemoLink("demoRepository", payload.project.repository, "charle-z/pixelgrama");
    setDemoText("demoBaseBranch", payload.project.base_branch);
    setDemoBilingualText("demoAuthorityPosture", "No verificado públicamente", "Not publicly verified");
    renderDemoStringList("demoPerimeterIncludes", payload.perimeter.includes);
    renderDemoStringList("demoPerimeterExcludes", payload.perimeter.excludes);
    renderDemoPullRequests(payload.historical_execution.pull_requests);
    renderDemoChecks(payload.historical_execution.pull_requests);
    renderDemoOperations("demoDirectOperations", payload.operations.direct);
    renderDemoOperations("demoPlanOperations", payload.operations.plan_protected);

    setDemoLink("demoProductionURL", payload.project.production_url, payload.project.production_url);
    setDemoLink("demoWallURL", payload.project.primary_public_route, "/wall");
    setDemoLink("demoVersionURL", payload.project.version_url, "/version");
    setDemoText("demoObservedCommit", payload.production_observation.observed_commit);
    setDemoText("demoSourceCommit", payload.production_observation.source_main_commit);
    setDemoText("demoVerifiedOn", payload.production_observation.verified_on);
    setDemoText("demoInfrastructure", payload.production_observation.infrastructure.provider + " + " + payload.production_observation.infrastructure.platform);
    setDemoBilingualText("demoProductionMatch",
      "COINCIDENCIA VERIFICADA: producción y source main reportan el mismo commit.",
      "VERIFIED MATCH: production and source main report the same commit.");
    var match = demoNode("demoProductionMatch");
    if (match) match.className = "demo-result-match ok";
    setDemoMessage("available");
  }

  function demoUnavailable() {
    setDemoMessage("unavailable");
    setDemoBilingualText("demoProductionMatch", "COMPARACIÓN NO DISPONIBLE", "COMPARISON UNAVAILABLE");
    var match = demoNode("demoProductionMatch");
    if (match) match.className = "demo-result-match warn";
  }

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

  var runtimeMessageKey = "loading";
  var activeSection = null;
  var languageButtons = Array.prototype.slice.call(document.querySelectorAll("[data-language]"));
  var runtimeMessages = {
    loading: {
      es: "Cargando la identidad pública del runtime.",
      en: "Loading public runtime identity."
    },
    available: {
      es: "Identidad cargada desde el contrato público y allowlisted del runtime.",
      en: "Identity loaded from the public, allowlisted runtime contract."
    },
    unavailable: {
      es: "La identidad pública no está disponible temporalmente. Usa /healthz o /version para reintentar; aquí no se expone ningún diagnóstico privado.",
      en: "Public identity temporarily unavailable. Use /healthz or /version to retry; no private diagnostic detail is exposed here."
    }
  };

  function setRuntimeMessage(key) {
    runtimeMessageKey = key;
    message.textContent = runtimeMessages[key][currentLanguage];
  }

  function applyLanguage(language) {
    currentLanguage = language === "es" ? "es" : "en";
    document.documentElement.lang = currentLanguage;
    Array.prototype.slice.call(document.querySelectorAll("[data-es][data-en]")).forEach(function (node) {
      if (node.getAttribute("data-i18n-role") === "hint") return;
      var value = currentLanguage === "es" ? node.getAttribute("data-es") : node.getAttribute("data-en");
      var attribute = node.getAttribute("data-i18n-attr");
      if (attribute) node.setAttribute(attribute, value);
      else node.textContent = value;
    });
    languageButtons.forEach(function (button) {
      button.setAttribute("aria-pressed", button.getAttribute("data-language") === currentLanguage ? "true" : "false");
    });
    if (activePolicyID) renderPolicy(activePolicyID);
    setRuntimeMessage(runtimeMessageKey);
    setDemoMessage(demoMessageKey);
    if (activeSection) showSection(activeSection);
  }

  languageButtons.forEach(function (button) {
    button.addEventListener("click", function () {
      applyLanguage(button.getAttribute("data-language"));
    });
  });

  applyLanguage(currentLanguage);

  function valid(payload) {
    return payload && typeof payload.status === "string" &&
      typeof payload.version === "string" && typeof payload.commit === "string" &&
      typeof payload.built_at === "string" && typeof payload.protocol_version === "string" &&
      Number.isInteger(payload.tool_count) && payload.tool_count > 0 &&
      typeof payload.catalog_hash === "string" && payload.catalog_hash.indexOf("sha256:") === 0;
  }

  function unavailable() {
    Object.keys(fields).forEach(function (key) {
      fields[key].textContent = "unavailable";
      fields[key].className = "warn";
    });
    topStatus.textContent = "unavailable";
    topStatus.className = "warn";
    topCommit.textContent = "identity";
    message.className = "runtime-error warn";
    message.setAttribute("data-runtime-state", "unavailable");
    setRuntimeMessage("unavailable");
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
      fields[key].className = key === "status" ? "ok" : "";
    });
    topStatus.textContent = payload.status;
    topStatus.className = "ok";
    topCommit.textContent = payload.commit.length > 12 ? payload.commit.slice(0, 12) : payload.commit;
    message.className = "runtime-error";
    message.setAttribute("data-runtime-state", "available");
    setRuntimeMessage("available");
  }).catch(unavailable);

  fetch("/showcase/pixelgrama-evidence.json", {
    method: "GET",
    credentials: "omit",
    cache: "no-store",
    redirect: "error",
    headers: { Accept: "application/json" }
  }).then(function (response) {
    if (!response.ok) throw new Error("unavailable");
    return response.json();
  }).then(function (payload) {
    if (!validEvidence(payload)) throw new Error("invalid");
    renderDemoEvidence(payload);
  }).catch(demoUnavailable);

  var sections = Array.prototype.slice.call(document.querySelectorAll("main section"));
  var sectionLabel = document.getElementById("sectionLabel");
  var sectionHint = document.getElementById("sectionHint");

  function showSection(section) {
    if (!section) return;
    activeSection = section;
    var labelES = section.getAttribute("data-label-es") || section.id.toUpperCase();
    var labelEN = section.getAttribute("data-label-en") || section.id.toUpperCase();
    sectionLabel.setAttribute("data-es", labelES);
    sectionLabel.setAttribute("data-en", labelEN);
    sectionLabel.textContent = currentLanguage === "es" ? labelES : labelEN;
    sectionHint.textContent = section.getAttribute("data-" + currentLanguage) || "";
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
