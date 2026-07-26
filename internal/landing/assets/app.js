(function () {
  "use strict";

  var reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  var currentLanguage = (navigator.language || "").toLowerCase().indexOf("es") === 0 ? "es" : "en";
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
    fields.status.textContent = "unavailable";
    fields.status.className = "warn";
    topStatus.textContent = "unavailable";
    topStatus.className = "warn";
    topCommit.textContent = "identity";
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
    });
    fields.status.className = "ok";
    topStatus.textContent = payload.status;
    topStatus.className = "ok";
    topCommit.textContent = payload.commit.length > 12 ? payload.commit.slice(0, 12) : payload.commit;
    setRuntimeMessage("available");
  }).catch(unavailable);

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
