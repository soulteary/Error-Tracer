// SPDX-License-Identifier: Apache-2.0

(() => {
  "use strict";

  const elements = {
    connection: document.querySelector("#connection-status"),
    loginPanel: document.querySelector("#login-panel"),
    tokenForm: document.querySelector("#token-form"),
    tokenInput: document.querySelector("#admin-token"),
    loginMessage: document.querySelector("#login-message"),
    workspace: document.querySelector("#workspace"),
    workspaceMessage: document.querySelector("#workspace-message"),
    refresh: document.querySelector("#refresh-button"),
    logout: document.querySelector("#logout-button"),
    statusFilter: document.querySelector("#status-filter"),
    issueList: document.querySelector("#issue-list"),
    emptyState: document.querySelector("#empty-state"),
    resultCopy: document.querySelector("#result-copy"),
    metricTotal: document.querySelector("#metric-total"),
    metricOccurrences: document.querySelector("#metric-occurrences"),
    metricLatest: document.querySelector("#metric-latest"),
    pageCopy: document.querySelector("#page-copy"),
    previous: document.querySelector("#previous-button"),
    next: document.querySelector("#next-button"),
    dialog: document.querySelector("#issue-dialog"),
    closeDialog: document.querySelector("#close-dialog"),
    detailKind: document.querySelector("#detail-kind"),
    detailTitle: document.querySelector("#detail-title"),
    detailMeta: document.querySelector("#detail-meta"),
    detailLocation: document.querySelector("#detail-location"),
    detailStack: document.querySelector("#detail-stack"),
    detailContext: document.querySelector("#detail-context"),
    statusActions: document.querySelector("#status-actions"),
  };

  const state = {
    token: "",
    status: "",
    limit: 25,
    offset: 0,
    page: null,
    selected: null,
    loading: false,
  };

  const integerFormat = new Intl.NumberFormat();
  const dateFormat = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });

  elements.tokenForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const token = elements.tokenInput.value.trim();
    if (token.length < 24) {
      showMessage(elements.loginMessage, "The admin token must contain at least 24 characters.", true);
      return;
    }
    state.token = token;
    state.offset = 0;
    setLoginBusy(true);
    showMessage(elements.loginMessage, "Connecting…", false);
    try {
      await loadIssues();
      elements.tokenInput.value = "";
      showWorkspace();
    } catch (error) {
      state.token = "";
      showMessage(elements.loginMessage, readableError(error), true);
      elements.tokenInput.focus();
    } finally {
      setLoginBusy(false);
    }
  });

  elements.refresh.addEventListener("click", () => {
    loadIssues().catch(showWorkspaceError);
  });

  elements.logout.addEventListener("click", () => {
    disconnect("Disconnected. Paste the admin token to reconnect.");
  });

  elements.statusFilter.addEventListener("change", () => {
    state.status = elements.statusFilter.value;
    state.offset = 0;
    loadIssues().catch(showWorkspaceError);
  });

  elements.previous.addEventListener("click", () => {
    state.offset = Math.max(0, state.offset - state.limit);
    loadIssues().catch(showWorkspaceError);
  });

  elements.next.addEventListener("click", () => {
    if (state.page && state.offset + state.limit < state.page.total) {
      state.offset += state.limit;
      loadIssues().catch(showWorkspaceError);
    }
  });

  elements.issueList.addEventListener("click", (event) => {
    const trigger = event.target && typeof event.target.closest === "function"
      ? event.target.closest("[data-fingerprint]")
      : null;
    if (trigger) {
      openIssue(trigger.dataset.fingerprint).catch(showWorkspaceError);
    }
  });

  elements.closeDialog.addEventListener("click", closeIssueDialog);

  elements.statusActions.addEventListener("click", (event) => {
    const trigger = event.target && typeof event.target.closest === "function"
      ? event.target.closest("[data-status]")
      : null;
    if (trigger && state.selected) {
      updateIssueStatus(trigger.dataset.status).catch(showWorkspaceError);
    }
  });

  async function loadIssues() {
    if (state.loading || !state.token) {
      return;
    }
    state.loading = true;
    setWorkspaceBusy(true);
    showMessage(elements.workspaceMessage, "", false);
    try {
      const query = new URLSearchParams({
        limit: String(state.limit),
        offset: String(state.offset),
      });
      if (state.status) {
        query.set("status", state.status);
      }
      state.page = await requestJSON(`/api/v1/issues?${query}`);
      if (state.offset > 0 && state.page.total <= state.offset) {
        state.offset = Math.max(
          0,
          Math.floor(Math.max(0, state.page.total - 1) / state.limit) * state.limit,
        );
        query.set("offset", String(state.offset));
        state.page = await requestJSON(`/api/v1/issues?${query}`);
      }
      renderPage(state.page);
    } finally {
      state.loading = false;
      setWorkspaceBusy(false);
    }
  }

  async function openIssue(fingerprint) {
    const result = await requestJSON(`/api/v1/issues/${encodeURIComponent(fingerprint)}`);
    state.selected = result.issue;
    renderDetail(result.issue);
    if (typeof elements.dialog.showModal === "function") {
      elements.dialog.showModal();
    } else {
      elements.dialog.setAttribute("open", "");
    }
  }

  async function updateIssueStatus(status) {
    if (!state.selected || state.selected.status === status) {
      return;
    }
    setStatusButtonsBusy(true);
    try {
      const result = await requestJSON(
        `/api/v1/issues/${encodeURIComponent(state.selected.fingerprint)}`,
        { method: "PATCH", body: JSON.stringify({ status }) },
      );
      state.selected = result.issue;
      renderDetail(result.issue);
      await loadIssues();
    } finally {
      setStatusButtonsBusy(false);
    }
  }

  async function requestJSON(path, options) {
    options = options || {};
    const headers = {
      Accept: "application/json",
      Authorization: `Bearer ${state.token}`,
    };
    if (options.body) {
      headers["Content-Type"] = "application/json";
    }

    let response;
    try {
      response = await fetch(path, {
        method: options.method || "GET",
        body: options.body,
        credentials: "omit",
        headers,
      });
    } catch (_) {
      throw new Error("Cannot reach Error-Tracer. Check the service and try again.");
    }

    const payload = await response.json().catch(() => ({}));
    if (response.status === 401) {
      disconnect("The admin token was rejected or has changed.");
      throw new Error("The admin token was rejected.");
    }
    if (!response.ok) {
      throw new Error(errorLabel(payload.error, response.status));
    }
    return payload;
  }

  function renderPage(page) {
    const issues = Array.isArray(page.issues) ? page.issues : [];
    elements.issueList.replaceChildren(...issues.map(issueRow));
    elements.emptyState.hidden = issues.length !== 0;
    elements.metricTotal.textContent = integerFormat.format(page.total || 0);
    elements.metricOccurrences.textContent = integerFormat.format(
      issues.reduce((total, issue) => total + Number(issue.occurrences || 0), 0),
    );
    elements.metricLatest.textContent = issues.length ? relativeTime(issues[0].last_seen) : "Quiet";

    const start = page.total ? state.offset + 1 : 0;
    const end = Math.min(state.offset + issues.length, page.total || 0);
    elements.resultCopy.textContent = page.total
      ? `Showing ${integerFormat.format(start)}–${integerFormat.format(end)} of ${integerFormat.format(page.total)}`
      : "No issues match this view";
    elements.pageCopy.textContent = `Page ${Math.floor(state.offset / state.limit) + 1}`;
    elements.previous.disabled = state.offset === 0;
    elements.next.disabled = state.offset + state.limit >= (page.total || 0);
  }

  function issueRow(issue) {
    const row = document.createElement("tr");
    const descriptionCell = document.createElement("td");
    const trigger = document.createElement("button");
    trigger.type = "button";
    trigger.className = "issue-link";
    trigger.dataset.fingerprint = issue.fingerprint;
    trigger.textContent = issue.message || kindLabel(issue.kind);
    const subtitle = document.createElement("span");
    subtitle.className = "issue-subtitle";
    subtitle.textContent = locationLabel(issue);
    descriptionCell.append(trigger, subtitle);

    const statusCell = document.createElement("td");
    const badge = document.createElement("span");
    badge.className = `badge badge-${safeStatus(issue.status)}`;
    badge.textContent = safeStatus(issue.status);
    statusCell.append(badge);

    const occurrencesCell = document.createElement("td");
    occurrencesCell.className = "occurrence";
    occurrencesCell.textContent = integerFormat.format(Number(issue.occurrences || 0));

    const seenCell = document.createElement("td");
    seenCell.className = "last-seen";
    seenCell.textContent = relativeTime(issue.last_seen);
    seenCell.title = absoluteTime(issue.last_seen);

    row.append(descriptionCell, statusCell, occurrencesCell, seenCell);
    return row;
  }

  function renderDetail(issue) {
    const latest = issue.last_event || {};
    elements.detailKind.textContent = kindLabel(issue.kind);
    elements.detailTitle.textContent = issue.message || kindLabel(issue.kind);
    elements.detailMeta.replaceChildren(
      detailMeta(`${integerFormat.format(Number(issue.occurrences || 0))} events`),
      detailMeta(`First ${absoluteTime(issue.first_seen)}`),
      detailMeta(`Last ${absoluteTime(issue.last_seen)}`),
      statusBadge(issue.status),
    );
    elements.detailLocation.textContent = locationLabel(issue);
    elements.detailStack.textContent = latest.stack || "No stack was captured.";
    elements.detailContext.replaceChildren();
    appendContext("Page", latest.page_url);
    appendContext("Release", latest.release);
    appendContext("Environment", latest.environment);
    appendContext("User agent", latest.user_agent);
    appendContext("Occurred", latest.occurred_at ? absoluteTime(latest.occurred_at) : "");
    appendContext("Event ID", latest.id);
    if (latest.tags && typeof latest.tags === "object") {
      appendContext(
        "Tags",
        Object.entries(latest.tags).map(([key, value]) => `${key}=${value}`).join(", "),
      );
    }
    for (const button of elements.statusActions.querySelectorAll("[data-status]")) {
      button.setAttribute("aria-pressed", String(button.dataset.status === issue.status));
    }
  }

  function appendContext(label, value) {
    if (!value) {
      return;
    }
    const term = document.createElement("dt");
    term.textContent = label;
    const description = document.createElement("dd");
    description.textContent = String(value);
    elements.detailContext.append(term, description);
  }

  function detailMeta(value) {
    const item = document.createElement("span");
    item.textContent = value;
    return item;
  }

  function statusBadge(status) {
    const badge = detailMeta(safeStatus(status));
    badge.className = `badge badge-${safeStatus(status)}`;
    return badge;
  }

  function showWorkspace() {
    elements.loginPanel.hidden = true;
    elements.workspace.hidden = false;
    elements.connection.hidden = false;
    showMessage(elements.loginMessage, "No credentials are stored by the dashboard.", false);
  }

  function disconnect(message) {
    state.token = "";
    state.page = null;
    state.selected = null;
    closeIssueDialog();
    elements.workspace.hidden = true;
    elements.connection.hidden = true;
    elements.loginPanel.hidden = false;
    elements.tokenInput.value = "";
    showMessage(elements.loginMessage, message, false);
    elements.tokenInput.focus();
  }

  function closeIssueDialog() {
    if (typeof elements.dialog.close === "function" && elements.dialog.open) {
      elements.dialog.close();
    } else {
      elements.dialog.removeAttribute("open");
    }
  }

  function showWorkspaceError(error) {
    showMessage(elements.workspaceMessage, readableError(error), true);
  }

  function showMessage(element, message, isError) {
    element.textContent = message;
    element.dataset.error = String(Boolean(isError));
  }

  function setLoginBusy(busy) {
    elements.tokenInput.disabled = busy;
    elements.tokenForm.querySelector("button").disabled = busy;
  }

  function setWorkspaceBusy(busy) {
    elements.refresh.disabled = busy;
    elements.statusFilter.disabled = busy;
    elements.previous.disabled = busy || state.offset === 0;
    elements.next.disabled = busy || !state.page ||
      state.offset + state.limit >= state.page.total;
  }

  function setStatusButtonsBusy(busy) {
    for (const button of elements.statusActions.querySelectorAll("button")) {
      button.disabled = busy;
    }
  }

  function locationLabel(issue) {
    const source = issue.source_url || "Unknown source";
    const line = Number(issue.line || 0);
    const column = Number(issue.column || 0);
    return line ? `${source}:${line}${column ? `:${column}` : ""}` : source;
  }

  function kindLabel(kind) {
    return String(kind || "error").replaceAll("_", " ");
  }

  function safeStatus(status) {
    return ["open", "resolved", "ignored"].includes(status) ? status : "open";
  }

  function absoluteTime(value) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? "Unknown time" : dateFormat.format(date);
  }

  function relativeTime(value) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return "Unknown";
    }
    const seconds = Math.round((date.getTime() - Date.now()) / 1000);
    const magnitude = Math.abs(seconds);
    const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
    if (magnitude < 60) {
      return formatter.format(seconds, "second");
    }
    if (magnitude < 3600) {
      return formatter.format(Math.round(seconds / 60), "minute");
    }
    if (magnitude < 86_400) {
      return formatter.format(Math.round(seconds / 3600), "hour");
    }
    return formatter.format(Math.round(seconds / 86_400), "day");
  }

  function errorLabel(code, status) {
    const labels = {
      admin_unavailable: "The management API is not configured.",
      issue_not_found: "That issue no longer exists.",
      invalid_pagination: "The requested issue page is invalid.",
      invalid_status: "That issue status is not supported.",
      internal_error: "Error-Tracer could not complete the request.",
    };
    return labels[code] || `Error-Tracer returned HTTP ${status}.`;
  }

  function readableError(error) {
    return error && error.message ? error.message : "Something went wrong. Try again.";
  }
})();
