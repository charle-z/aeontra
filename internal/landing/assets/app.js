(() => {
  "use strict";

  const copies = Array.from(document.querySelectorAll("[data-en][data-es]"));
  const languageToggle = document.querySelector("[data-language-toggle]");
  const modeTabs = Array.from(document.querySelectorAll('[role="tab"][data-mode]'));
  const modePanels = Array.from(document.querySelectorAll('[role="tabpanel"]'));
  const copyButton = document.querySelector("[data-copy-command]");
  const copyStatus = document.querySelector("[data-copy-status]");
  const installCommand = document.querySelector("[data-install-command]");
  let language = navigator.language.toLowerCase().startsWith("es") ? "es" : "en";

  function translated(node, lang) {
    return lang === "es" ? node.dataset.es : node.dataset.en;
  }

  function setLanguage(nextLanguage) {
    language = nextLanguage === "es" ? "es" : "en";
    document.documentElement.lang = language;
    copies.forEach((node) => {
      const value = translated(node, language);
      if (typeof value === "string") node.textContent = value;
    });
    if (languageToggle) {
      languageToggle.textContent = language === "en" ? "ES" : "EN";
      languageToggle.setAttribute(
        "aria-label",
        language === "en" ? "Cambiar idioma a español" : "Switch language to English"
      );
    }
  }

  if (languageToggle) {
    languageToggle.addEventListener("click", () => {
      setLanguage(language === "en" ? "es" : "en");
    });
  }

  function selectMode(tab, moveFocus) {
    if (!tab) return;
    const panelID = tab.getAttribute("aria-controls");
    modeTabs.forEach((candidate) => {
      const selected = candidate === tab;
      candidate.setAttribute("aria-selected", selected ? "true" : "false");
      candidate.setAttribute("tabindex", selected ? "0" : "-1");
    });
    modePanels.forEach((panel) => {
      panel.hidden = panel.id !== panelID;
    });
    if (moveFocus) tab.focus();
  }

  modeTabs.forEach((tab, index) => {
    tab.addEventListener("click", () => selectMode(tab, false));
    tab.addEventListener("keydown", (event) => {
      let targetIndex = index;
      if (event.key === "ArrowRight") targetIndex = (index + 1) % modeTabs.length;
      else if (event.key === "ArrowLeft") targetIndex = (index - 1 + modeTabs.length) % modeTabs.length;
      else if (event.key === "Home") targetIndex = 0;
      else if (event.key === "End") targetIndex = modeTabs.length - 1;
      else return;
      event.preventDefault();
      selectMode(modeTabs[targetIndex], true);
    });
  });

  if (copyButton && copyStatus && installCommand) {
    copyButton.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(installCommand.textContent.trim());
        copyStatus.textContent = language === "es" ? "Comandos copiados." : "Commands copied.";
      } catch (_error) {
        copyStatus.textContent = language === "es" ? "Selecciona y copia los comandos manualmente." : "Select and copy the commands manually.";
      }
    });
  }

  function safeText(value, pattern, fallback) {
    if (typeof value !== "string" || !pattern.test(value)) return fallback;
    return value;
  }

  function setRuntimeState(state, payload) {
    const status = document.querySelector("[data-runtime-status]");
    const version = document.querySelector("[data-runtime-version]");
    const tools = document.querySelector("[data-runtime-tools]");
    const commit = document.querySelector("[data-runtime-commit]");
    if (!status || !version || !tools || !commit) return;

    status.dataset.state = state;
    if (state !== "ready") {
      status.textContent = language === "es" ? "NO DISPONIBLE" : "UNAVAILABLE";
      version.textContent = "—";
      tools.textContent = "—";
      commit.textContent = "—";
      return;
    }

    status.textContent = "READY";
    version.textContent = safeText(payload.version, /^[0-9A-Za-z._+-]{1,32}$/, "—");
    tools.textContent = Number.isSafeInteger(payload.tool_count) && payload.tool_count >= 0 ? String(payload.tool_count) : "—";
    const exactCommit = safeText(payload.commit, /^[0-9a-f]{40}$/, "");
    commit.textContent = exactCommit ? exactCommit.slice(0, 8) : "—";
  }

  async function loadRuntime() {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 4500);
    try {
      const response = await fetch("/version", {
        credentials: "omit",
        headers: { Accept: "application/json" },
        signal: controller.signal,
      });
      if (!response.ok) throw new Error("runtime unavailable");
      const payload = await response.json();
      if (!payload || typeof payload !== "object" || Array.isArray(payload)) throw new Error("invalid runtime identity");
      setRuntimeState("ready", payload);
    } catch (_error) {
      setRuntimeState("unavailable", {});
    } finally {
      window.clearTimeout(timeout);
    }
  }

  setLanguage(language);
  loadRuntime();
})();
