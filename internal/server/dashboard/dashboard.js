// SPDX-License-Identifier: Apache-2.0

(() => {
  "use strict";

  const translations = {
    en: {
      "brand.homeAria": "Error-Tracer home",
      "brand.tagline": "browser failure observatory",
      "language.label": "Language",
      "connection.connected": "Connected",
      "connection.demo": "Read-only demo",
      "login.eyebrow": "Private control plane",
      "login.title": "See the failures your users cannot explain.",
      "login.body": "Connect with the admin token configured on this Error-Tracer instance. The token stays in this tab's memory and is never persisted.",
      "login.tokenLabel": "Admin token",
      "login.tokenPlaceholder": "Paste a 24+ character token",
      "login.connect": "Connect",
      "login.demo": "Explore the read-only demo",
      "login.noCredentials": "No credentials are stored by the dashboard.",
      "login.tokenTooShort": "The admin token must contain at least 24 characters.",
      "login.connecting": "Connecting…",
      "login.loadingDemo": "Loading the read-only demo…",
      "login.disconnected": "Disconnected. Paste the admin token to reconnect.",
      "login.tokenChanged": "The admin token was rejected or has changed.",
      "login.tokenRejected": "The admin token was rejected.",
      "workspace.aria": "Issue dashboard",
      "workspace.eyebrow": "Issue stream",
      "workspace.title": "Failures, grouped by root signal.",
      "workspace.refresh": "Refresh",
      "workspace.disconnect": "Disconnect",
      "workspace.readOnly": "The demo is read-only. Connect with an admin token to change issue status.",
      "demo.title": "Demo workspace",
      "demo.body": "Built-in sample events only. The live database and management API remain private.",
      "metrics.aria": "Current page summary",
      "metrics.matching": "Matching issues",
      "metrics.occurrences": "Page occurrences",
      "metrics.latest": "Latest signal",
      "metrics.quiet": "Quiet",
      "issues.title": "Issues",
      "issues.waiting": "Waiting for data",
      "issues.status": "Status",
      "issues.columnIssue": "Issue",
      "issues.columnStatus": "Status",
      "issues.columnEvents": "Events",
      "issues.columnLastSeen": "Last seen",
      "issues.emptyTitle": "No matching issues",
      "issues.emptyBody": "The quiet is real—for this filter, at least.",
      "issues.pageInitial": "Page 1",
      "issues.previous": "Previous",
      "issues.next": "Next",
      "issues.showing": "Showing {start}–{end} of {total}",
      "issues.noResults": "No issues match this view",
      "issues.page": "Page {page}",
      "status.all": "All",
      "status.open": "Open",
      "status.resolved": "Resolved",
      "status.ignored": "Ignored",
      "detail.kind": "Issue detail",
      "detail.loading": "Loading…",
      "detail.close": "Close issue detail",
      "detail.location": "Location",
      "detail.latestStack": "Latest stack",
      "detail.noStack": "No stack was captured.",
      "detail.context": "Context",
      "detail.setStatus": "Set status",
      "detail.event": "{count} event",
      "detail.events": "{count} events",
      "detail.first": "First {time}",
      "detail.last": "Last {time}",
      "detail.page": "Page",
      "detail.release": "Release",
      "detail.environment": "Environment",
      "detail.userAgent": "User agent",
      "detail.occurred": "Occurred",
      "detail.eventID": "Event ID",
      "detail.tags": "Tags",
      "kind.error": "error",
      "kind.unhandled_rejection": "unhandled rejection",
      "kind.resource_error": "resource error",
      "location.unknownSource": "Unknown source",
      "time.unknown": "Unknown time",
      "time.unknownShort": "Unknown",
      "errors.unreachable": "Cannot reach Error-Tracer. Check the service and try again.",
      "errors.adminUnavailable": "The management API is not configured.",
      "errors.issueNotFound": "That issue no longer exists.",
      "errors.invalidPagination": "The requested issue page is invalid.",
      "errors.invalidStatus": "That issue status is not supported.",
      "errors.demoUnavailable": "The demo is no longer available on this instance.",
      "errors.internal": "Error-Tracer could not complete the request.",
      "errors.http": "Error-Tracer returned HTTP {status}.",
      "errors.generic": "Something went wrong. Try again.",
    },
    "zh-CN": {
      "brand.homeAria": "Error-Tracer 首页",
      "brand.tagline": "浏览器错误观测台",
      "language.label": "语言",
      "connection.connected": "已连接",
      "connection.demo": "只读演示",
      "login.eyebrow": "私有控制台",
      "login.title": "看见用户难以描述的故障。",
      "login.body": "使用当前 Error-Tracer 实例配置的管理员令牌连接。令牌只保存在此标签页的内存中，绝不会持久化。",
      "login.tokenLabel": "管理员令牌",
      "login.tokenPlaceholder": "粘贴至少 24 个字符的令牌",
      "login.connect": "连接",
      "login.demo": "查看只读演示",
      "login.noCredentials": "Dashboard 不会保存任何凭据。",
      "login.tokenTooShort": "管理员令牌必须至少包含 24 个字符。",
      "login.connecting": "正在连接…",
      "login.loadingDemo": "正在加载只读演示…",
      "login.disconnected": "已断开。粘贴管理员令牌可重新连接。",
      "login.tokenChanged": "管理员令牌被拒绝或已发生变更。",
      "login.tokenRejected": "管理员令牌被拒绝。",
      "workspace.aria": "问题 Dashboard",
      "workspace.eyebrow": "问题流",
      "workspace.title": "按根信号聚合故障。",
      "workspace.refresh": "刷新",
      "workspace.disconnect": "断开连接",
      "workspace.readOnly": "演示模式为只读。请使用管理员令牌连接后再修改问题状态。",
      "demo.title": "演示工作区",
      "demo.body": "这里只显示内置样例事件，真实数据库和管理 API 保持私有。",
      "metrics.aria": "当前页面摘要",
      "metrics.matching": "匹配的问题",
      "metrics.occurrences": "本页发生次数",
      "metrics.latest": "最新信号",
      "metrics.quiet": "暂无信号",
      "issues.title": "问题",
      "issues.waiting": "等待数据",
      "issues.status": "状态",
      "issues.columnIssue": "问题",
      "issues.columnStatus": "状态",
      "issues.columnEvents": "事件数",
      "issues.columnLastSeen": "最后出现",
      "issues.emptyTitle": "没有匹配的问题",
      "issues.emptyBody": "至少在当前筛选条件下，一切安静。",
      "issues.pageInitial": "第 1 页",
      "issues.previous": "上一页",
      "issues.next": "下一页",
      "issues.showing": "显示第 {start}–{end} 项，共 {total} 项",
      "issues.noResults": "当前视图没有匹配的问题",
      "issues.page": "第 {page} 页",
      "status.all": "全部",
      "status.open": "待处理",
      "status.resolved": "已解决",
      "status.ignored": "已忽略",
      "detail.kind": "问题详情",
      "detail.loading": "正在加载…",
      "detail.close": "关闭问题详情",
      "detail.location": "位置",
      "detail.latestStack": "最新堆栈",
      "detail.noStack": "没有捕获到堆栈。",
      "detail.context": "上下文",
      "detail.setStatus": "设置状态",
      "detail.event": "{count} 个事件",
      "detail.events": "{count} 个事件",
      "detail.first": "首次：{time}",
      "detail.last": "最近：{time}",
      "detail.page": "页面",
      "detail.release": "版本",
      "detail.environment": "环境",
      "detail.userAgent": "用户代理",
      "detail.occurred": "发生时间",
      "detail.eventID": "事件 ID",
      "detail.tags": "标签",
      "kind.error": "错误",
      "kind.unhandled_rejection": "未处理的 Promise 拒绝",
      "kind.resource_error": "资源加载错误",
      "location.unknownSource": "未知来源",
      "time.unknown": "未知时间",
      "time.unknownShort": "未知",
      "errors.unreachable": "无法连接 Error-Tracer，请检查服务后重试。",
      "errors.adminUnavailable": "管理 API 尚未配置。",
      "errors.issueNotFound": "该问题已不存在。",
      "errors.invalidPagination": "请求的问题页面无效。",
      "errors.invalidStatus": "不支持该问题状态。",
      "errors.demoUnavailable": "此实例已不再提供演示模式。",
      "errors.internal": "Error-Tracer 无法完成请求。",
      "errors.http": "Error-Tracer 返回了 HTTP {status}。",
      "errors.generic": "出现错误，请重试。",
    },
  };

  const elements = {
    language: document.querySelector("#language-select"),
    connection: document.querySelector("#connection-status"),
    connectionLabel: document.querySelector("#connection-label"),
    loginPanel: document.querySelector("#login-panel"),
    tokenForm: document.querySelector("#token-form"),
    tokenInput: document.querySelector("#admin-token"),
    loginMessage: document.querySelector("#login-message"),
    demo: document.querySelector("#demo-button"),
    demoBanner: document.querySelector("#demo-banner"),
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
    language: detectLanguage(),
    token: "",
    demo: false,
    status: "",
    limit: 25,
    offset: 0,
    page: null,
    selected: null,
    loading: false,
  };

  const messageRenderers = new Map();
  let integerFormat;
  let dateFormat;
  let relativeFormat;

  elements.language.addEventListener("change", () => {
    setLanguage(elements.language.value, true);
  });

  elements.tokenForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const token = elements.tokenInput.value.trim();
    if (token.length < 24) {
      showMessage(elements.loginMessage, message("login.tokenTooShort"), true);
      return;
    }
    state.demo = false;
    state.token = token;
    state.offset = 0;
    setLoginBusy(true);
    showMessage(elements.loginMessage, message("login.connecting"), false);
    try {
      await loadIssues();
      elements.tokenInput.value = "";
      showWorkspace();
    } catch (error) {
      state.token = "";
      showMessage(elements.loginMessage, () => readableError(error), true);
      elements.tokenInput.focus();
    } finally {
      setLoginBusy(false);
    }
  });

  elements.demo.addEventListener("click", async () => {
    state.token = "";
    state.demo = true;
    state.offset = 0;
    setLoginBusy(true);
    showMessage(elements.loginMessage, message("login.loadingDemo"), false);
    try {
      await loadIssues();
      showWorkspace();
    } catch (error) {
      state.demo = false;
      showMessage(elements.loginMessage, () => readableError(error), true);
    } finally {
      setLoginBusy(false);
    }
  });

  elements.refresh.addEventListener("click", () => {
    loadIssues().catch(showWorkspaceError);
  });

  elements.logout.addEventListener("click", () => {
    disconnect("login.disconnected");
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
    if (state.loading || (!state.token && !state.demo)) {
      return;
    }
    state.loading = true;
    setWorkspaceBusy(true);
    showMessage(elements.workspaceMessage, () => "", false);
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
    if (state.demo) {
      showMessage(elements.workspaceMessage, message("workspace.readOnly"), false);
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
    const headers = { Accept: "application/json" };
    if (!state.demo) {
      headers.Authorization = `Bearer ${state.token}`;
    }
    if (options.body) {
      headers["Content-Type"] = "application/json";
    }

    const requestPath = state.demo
      ? path.replace(/^\/api\/v1\/issues/, "/api/v1/demo/issues")
      : path;

    let response;
    try {
      response = await fetch(requestPath, {
        method: options.method || "GET",
        body: options.body,
        credentials: "omit",
        headers,
      });
    } catch (_) {
      throw dashboardError("errors.unreachable");
    }

    const payload = await response.json().catch(() => ({}));
    if (response.status === 401 && !state.demo) {
      disconnect("login.tokenChanged");
      throw dashboardError("login.tokenRejected");
    }
    if (!response.ok) {
      throw dashboardError(errorKey(payload.error), { status: response.status });
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
    elements.metricLatest.textContent = issues.length
      ? relativeTime(issues[0].last_seen)
      : t("metrics.quiet");

    const start = page.total ? state.offset + 1 : 0;
    const end = Math.min(state.offset + issues.length, page.total || 0);
    elements.resultCopy.textContent = page.total
      ? t("issues.showing", {
        start: integerFormat.format(start),
        end: integerFormat.format(end),
        total: integerFormat.format(page.total),
      })
      : t("issues.noResults");
    elements.pageCopy.textContent = t("issues.page", {
      page: integerFormat.format(Math.floor(state.offset / state.limit) + 1),
    });
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
    badge.textContent = statusLabel(issue.status);
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
    const count = Number(issue.occurrences || 0);
    elements.detailKind.textContent = kindLabel(issue.kind);
    elements.detailTitle.textContent = issue.message || kindLabel(issue.kind);
    elements.detailMeta.replaceChildren(
      detailMeta(t(count === 1 ? "detail.event" : "detail.events", {
        count: integerFormat.format(count),
      })),
      detailMeta(t("detail.first", { time: absoluteTime(issue.first_seen) })),
      detailMeta(t("detail.last", { time: absoluteTime(issue.last_seen) })),
      statusBadge(issue.status),
    );
    elements.detailLocation.textContent = locationLabel(issue);
    elements.detailStack.textContent = latest.stack || t("detail.noStack");
    elements.detailContext.replaceChildren();
    appendContext(t("detail.page"), latest.page_url);
    appendContext(t("detail.release"), latest.release);
    appendContext(t("detail.environment"), latest.environment);
    appendContext(t("detail.userAgent"), latest.user_agent);
    appendContext(
      t("detail.occurred"),
      latest.occurred_at ? absoluteTime(latest.occurred_at) : "",
    );
    appendContext(t("detail.eventID"), latest.id);
    if (latest.tags && typeof latest.tags === "object") {
      appendContext(
        t("detail.tags"),
        Object.entries(latest.tags).map(([key, value]) => `${key}=${value}`).join(", "),
      );
    }
    for (const button of elements.statusActions.querySelectorAll("[data-status]")) {
      button.setAttribute("aria-pressed", String(button.dataset.status === issue.status));
    }
    setStatusButtonsBusy(false);
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
    const badge = detailMeta(statusLabel(status));
    badge.className = `badge badge-${safeStatus(status)}`;
    return badge;
  }

  function showWorkspace() {
    elements.loginPanel.hidden = true;
    elements.workspace.hidden = false;
    elements.connection.hidden = false;
    elements.demoBanner.hidden = !state.demo;
    updateConnectionLabel();
    showMessage(elements.loginMessage, message("login.noCredentials"), false);
  }

  function disconnect(messageKey) {
    state.token = "";
    state.demo = false;
    state.status = "";
    state.offset = 0;
    state.page = null;
    state.selected = null;
    closeIssueDialog();
    elements.workspace.hidden = true;
    elements.connection.hidden = true;
    elements.demoBanner.hidden = true;
    elements.statusFilter.value = "";
    updateConnectionLabel();
    elements.loginPanel.hidden = false;
    elements.tokenInput.value = "";
    showMessage(elements.loginMessage, message(messageKey), false);
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
    showMessage(elements.workspaceMessage, () => readableError(error), true);
  }

  function message(key, values) {
    return () => t(key, values);
  }

  function showMessage(element, renderer, isError) {
    const safeRenderer = typeof renderer === "function" ? renderer : () => String(renderer);
    messageRenderers.set(element, { renderer: safeRenderer, isError: Boolean(isError) });
    renderMessage(element, safeRenderer, isError);
  }

  function renderMessage(element, renderer, isError) {
    element.textContent = renderer();
    element.dataset.error = String(Boolean(isError));
  }

  function refreshMessages() {
    for (const [element, rendered] of messageRenderers) {
      renderMessage(element, rendered.renderer, rendered.isError);
    }
  }

  function setLoginBusy(busy) {
    elements.tokenInput.disabled = busy;
    for (const button of elements.tokenForm.querySelectorAll("button")) {
      button.disabled = busy;
    }
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
      button.disabled = busy || state.demo;
    }
  }

  function locationLabel(issue) {
    const source = issue.source_url || t("location.unknownSource");
    const line = Number(issue.line || 0);
    const column = Number(issue.column || 0);
    return line ? `${source}:${line}${column ? `:${column}` : ""}` : source;
  }

  function kindLabel(kind) {
    const supported = ["error", "unhandled_rejection", "resource_error"];
    return t(`kind.${supported.includes(kind) ? kind : "error"}`);
  }

  function safeStatus(status) {
    return ["open", "resolved", "ignored"].includes(status) ? status : "open";
  }

  function statusLabel(status) {
    return t(`status.${safeStatus(status)}`);
  }

  function absoluteTime(value) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? t("time.unknown") : dateFormat.format(date);
  }

  function relativeTime(value) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return t("time.unknownShort");
    }
    const seconds = Math.round((date.getTime() - Date.now()) / 1000);
    const magnitude = Math.abs(seconds);
    if (magnitude < 60) {
      return relativeFormat.format(seconds, "second");
    }
    if (magnitude < 3600) {
      return relativeFormat.format(Math.round(seconds / 60), "minute");
    }
    if (magnitude < 86_400) {
      return relativeFormat.format(Math.round(seconds / 3600), "hour");
    }
    return relativeFormat.format(Math.round(seconds / 86_400), "day");
  }

  function errorKey(code) {
    const labels = {
      admin_unavailable: "errors.adminUnavailable",
      issue_not_found: "errors.issueNotFound",
      invalid_pagination: "errors.invalidPagination",
      invalid_status: "errors.invalidStatus",
      demo_unavailable: "errors.demoUnavailable",
      internal_error: "errors.internal",
    };
    return labels[code] || "errors.http";
  }

  function dashboardError(key, values) {
    const error = new Error(key);
    error.translationKey = key;
    error.translationValues = values;
    return error;
  }

  function readableError(error) {
    if (error && error.translationKey) {
      return t(error.translationKey, error.translationValues);
    }
    return t("errors.generic");
  }

  function detectLanguage() {
    const requested = new URLSearchParams(window.location.search).get("lang");
    const fromQuery = supportedLanguage(requested);
    if (fromQuery) {
      return fromQuery;
    }
    const browserLanguages = Array.isArray(navigator.languages)
      ? navigator.languages
      : [navigator.language];
    for (const language of browserLanguages) {
      const supported = supportedLanguage(language);
      if (supported) {
        return supported;
      }
    }
    return "en";
  }

  function supportedLanguage(language) {
    const normalized = String(language || "").toLowerCase();
    if (normalized === "en" || normalized.startsWith("en-")) {
      return "en";
    }
    if (normalized === "zh" || normalized === "zh-cn" || normalized === "zh-sg" ||
        normalized.startsWith("zh-hans")) {
      return "zh-CN";
    }
    return "";
  }

  function setLanguage(language, updateURL) {
    state.language = supportedLanguage(language) || "en";
    if (updateURL) {
      try {
        const currentURL = new URL(window.location.href);
        currentURL.searchParams.set("lang", state.language);
        window.history.replaceState(null, "", currentURL);
      } catch (_) {
        // The dashboard still switches languages when history is unavailable.
      }
    }
    configureFormatters();
    applyTranslations();
  }

  function configureFormatters() {
    integerFormat = new Intl.NumberFormat(state.language);
    dateFormat = new Intl.DateTimeFormat(state.language, {
      dateStyle: "medium",
      timeStyle: "short",
    });
    relativeFormat = new Intl.RelativeTimeFormat(state.language, { numeric: "auto" });
  }

  function applyTranslations() {
    document.documentElement.lang = state.language;
    elements.language.value = state.language;
    for (const element of document.querySelectorAll("[data-i18n]")) {
      element.textContent = t(element.dataset.i18n);
    }
    for (const element of document.querySelectorAll("[data-i18n-placeholder]")) {
      element.setAttribute("placeholder", t(element.dataset.i18nPlaceholder));
    }
    for (const element of document.querySelectorAll("[data-i18n-aria-label]")) {
      element.setAttribute("aria-label", t(element.dataset.i18nAriaLabel));
    }
    updateConnectionLabel();
    refreshMessages();
    if (state.page) {
      renderPage(state.page);
    }
    if (state.selected) {
      renderDetail(state.selected);
    }
  }

  function updateConnectionLabel() {
    elements.connectionLabel.textContent = t(
      state.demo ? "connection.demo" : "connection.connected",
    );
  }

  function t(key, values) {
    const table = translations[state.language] || translations.en;
    const template = table[key] || translations.en[key] || key;
    return template.replace(/\{([a-zA-Z0-9_]+)\}/g, (_, name) => {
      if (!values || values[name] === undefined || values[name] === null) {
        return `{${name}}`;
      }
      return String(values[name]);
    });
  }

  async function discoverDemo() {
    try {
      const response = await fetch("/api/v1/meta", {
        credentials: "omit",
        headers: { Accept: "application/json" },
      });
      if (!response.ok) {
        return;
      }
      const metadata = await response.json();
      elements.demo.hidden = metadata.demo_mode !== true;
    } catch (_) {
      elements.demo.hidden = true;
    }
  }

  configureFormatters();
  applyTranslations();
  showMessage(elements.loginMessage, message("login.noCredentials"), false);
  discoverDemo();
})();
