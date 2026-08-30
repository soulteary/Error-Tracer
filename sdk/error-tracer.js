// SPDX-License-Identifier: Apache-2.0

(function exposeErrorTracer(root, factory) {
  const api = factory(root);
  if (typeof module === "object" && module.exports) {
    module.exports = api;
  } else {
    root.ErrorTracer = api;
  }
})(typeof globalThis === "object" ? globalThis : this, function createAPI(defaultRuntime) {
  "use strict";

  const LIMITS = Object.freeze({
    message: 4 * 1024,
    stack: 64 * 1024,
    url: 2 * 1024,
    release: 128,
    environment: 128,
    tagCount: 32,
    tagKey: 64,
    tagValue: 256,
  });
  const KEEPALIVE_BODY_LIMIT = 60 * 1024;
  const EVENT_KINDS = new Set(["error", "unhandled_rejection", "resource_error"]);

  class Client {
    constructor(options) {
      options = options || {};
      if (typeof options !== "object") {
        throw new TypeError("ErrorTracer options must be an object");
      }

      this.runtime = options.runtime || defaultRuntime;
      this.endpoint = cleanString(options.endpoint || "/api/v1/events");
      this.projectKey = cleanString(options.projectKey);
      if (!this.endpoint) {
        throw new TypeError("ErrorTracer endpoint is required");
      }
      if (utf8Length(this.projectKey) < 16) {
        throw new TypeError("ErrorTracer projectKey must contain at least 16 bytes");
      }

      this.sampleRate = options.sampleRate === undefined ? 1 : Number(options.sampleRate);
      if (!Number.isFinite(this.sampleRate) || this.sampleRate < 0 || this.sampleRate > 1) {
        throw new TypeError("ErrorTracer sampleRate must be between 0 and 1");
      }
      this.maxEventsPerMinute = options.maxEventsPerMinute === undefined
        ? 30
        : Number(options.maxEventsPerMinute);
      if (!Number.isInteger(this.maxEventsPerMinute) ||
          this.maxEventsPerMinute < 1 ||
          this.maxEventsPerMinute > 1000) {
        throw new TypeError("ErrorTracer maxEventsPerMinute must be an integer between 1 and 1000");
      }
      if (options.beforeSend !== undefined && typeof options.beforeSend !== "function") {
        throw new TypeError("ErrorTracer beforeSend must be a function");
      }
      if (options.transport !== undefined && typeof options.transport !== "function") {
        throw new TypeError("ErrorTracer transport must be a function");
      }
      if (options.autoCapture !== undefined && typeof options.autoCapture !== "boolean") {
        throw new TypeError("ErrorTracer autoCapture must be a boolean");
      }
      this.batchSize = integerOption(options.batchSize, 10, 1, 100, "batchSize");
      this.flushInterval = integerOption(
        options.flushInterval, 1000, 0, 60_000, "flushInterval",
      );
      this.maxQueueSize = integerOption(
        options.maxQueueSize, 100, this.batchSize, 1000, "maxQueueSize",
      );
      this.maxRetries = integerOption(options.maxRetries, 2, 0, 5, "maxRetries");
      this.retryBaseDelay = integerOption(
        options.retryBaseDelay, 250, 1, 10_000, "retryBaseDelay",
      );
      this.maxBatchBytes = integerOption(
        options.maxBatchBytes, KEEPALIVE_BODY_LIMIT, 1024, 900 * 1024, "maxBatchBytes",
      );

      this.release = truncateUTF8(cleanString(options.release), LIMITS.release);
      this.environment = truncateUTF8(cleanString(options.environment), LIMITS.environment);
      this.tags = normalizeTags(options.tags);
      this.beforeSend = options.beforeSend || null;
      this.random = typeof options.random === "function" ? options.random : Math.random;
      this.clock = typeof options.clock === "function" ? options.clock : () => new Date();
      this.sentAt = [];
      this.batchEndpoint = cleanString(
        options.batchEndpoint || defaultBatchEndpoint(this.endpoint),
      );
      if (!this.batchEndpoint) {
        throw new TypeError("ErrorTracer batchEndpoint is required");
      }
      this.transport = options.transport ||
        defaultTransport(this.runtime, this.batchEndpoint);
      this.queue = [];
      this.reservedCount = 0;
      this.flushTimer = null;
      this.flushPromise = null;
      this.stats = {
        sent: 0,
        dropped: 0,
        failed: 0,
        batches: 0,
        retries: 0,
      };
      this.installed = false;
      this.errorListener = (event) => this.captureWindowError(event);
      this.rejectionListener = (event) => this.captureUnhandledRejection(event);
      this.pagehideListener = () => this.settle(this.flush());
      this.visibilityTarget = safeRead(this.runtime, "document") || null;
      this.visibilityListener = () => {
        if (safeRead(this.visibilityTarget, "visibilityState") === "hidden") {
          this.settle(this.flush());
        }
      };
      if (options.autoCapture !== false) {
        this.install();
      }
    }

    install() {
      if (this.installed || !this.runtime) {
        return false;
      }
      if (typeof this.runtime.addEventListener !== "function" ||
          typeof this.runtime.removeEventListener !== "function") {
        return false;
      }
      const installed = [];
      try {
        this.runtime.addEventListener("error", this.errorListener, true);
        installed.push([this.runtime, "error", this.errorListener, true]);
        this.runtime.addEventListener("unhandledrejection", this.rejectionListener, false);
        installed.push([this.runtime, "unhandledrejection", this.rejectionListener, false]);
        this.runtime.addEventListener("pagehide", this.pagehideListener, false);
        installed.push([this.runtime, "pagehide", this.pagehideListener, false]);
        if (this.visibilityTarget &&
            typeof safeRead(this.visibilityTarget, "addEventListener") === "function" &&
            typeof safeRead(this.visibilityTarget, "removeEventListener") === "function") {
          this.visibilityTarget.addEventListener(
            "visibilitychange", this.visibilityListener, false,
          );
          installed.push([
            this.visibilityTarget, "visibilitychange", this.visibilityListener, false,
          ]);
        }
      } catch (_) {
        for (const registration of installed.reverse()) {
          removeListener(...registration);
        }
        return false;
      }
      this.installed = true;
      return true;
    }

    destroy() {
      if (this.installed && this.runtime &&
          typeof this.runtime.removeEventListener === "function") {
        removeListener(this.runtime, "error", this.errorListener, true);
        removeListener(this.runtime, "unhandledrejection", this.rejectionListener, false);
        removeListener(this.runtime, "pagehide", this.pagehideListener, false);
        removeListener(
          this.visibilityTarget, "visibilitychange", this.visibilityListener, false,
        );
      }
      this.clearFlushTimer();
      this.settle(this.flush());
      this.installed = false;
    }

    captureWindowError(errorEvent) {
      try {
        const target = safeRead(errorEvent, "target");
        if (target && target !== this.runtime) {
          const sourceURL = safeRead(target, "currentSrc") ||
            safeRead(target, "src") || safeRead(target, "href");
          const tagName = cleanString(safeRead(target, "tagName")).toLowerCase() || "resource";
          this.settle(this.capture({
            kind: "resource_error",
            message: `Failed to load ${tagName}`,
            source_url: sourceURL,
          }));
          return;
        }

        const error = safeRead(errorEvent, "error");
        const details = errorDetails(error);
        this.settle(this.capture({
          kind: "error",
          message: cleanString(safeRead(errorEvent, "message")) || details.message,
          stack: details.stack,
          source_url: safeRead(errorEvent, "filename"),
          line: safeRead(errorEvent, "lineno"),
          column: safeRead(errorEvent, "colno"),
        }));
      } catch (_) {
        // Browser error handlers must never throw into the host page.
      }
    }

    captureUnhandledRejection(rejectionEvent) {
      try {
        const details = rejectionDetails(safeRead(rejectionEvent, "reason"));
        this.settle(this.capture({
          kind: "unhandled_rejection",
          message: details.message,
          stack: details.stack,
        }));
      } catch (_) {
        // Browser rejection handlers must never create another rejection.
      }
    }

    settle(result) {
      if (result && typeof result.catch === "function") {
        result.catch(() => {});
      }
    }

    captureException(error, context) {
      context = context || {};
      const details = errorDetails(error);
      return this.capture({
        kind: "error",
        message: details.message,
        stack: details.stack,
        source_url: safeRead(context, "sourceURL"),
        line: safeRead(context, "line"),
        column: safeRead(context, "column"),
        tags: safeRead(context, "tags"),
      });
    }

    captureMessage(message, context) {
      context = context || {};
      return this.capture({
        kind: "error",
        message,
        source_url: safeRead(context, "sourceURL"),
        line: safeRead(context, "line"),
        column: safeRead(context, "column"),
        tags: safeRead(context, "tags"),
      });
    }

    capture(candidate) {
      if (this.sampleRate === 0) {
        return Promise.resolve(false);
      }
      let randomValue;
      try {
        randomValue = this.random();
      } catch (_) {
        return Promise.resolve(false);
      }
      if (!Number.isFinite(randomValue) || randomValue < 0 || randomValue >= this.sampleRate) {
        return Promise.resolve(false);
      }

      let captured;
      try {
        captured = this.normalize(candidate, true);
      } catch (_) {
        return Promise.resolve(false);
      }
      if (!captured) {
        return Promise.resolve(false);
      }
      if (this.beforeSend) {
        try {
          captured = this.beforeSend(cloneEvent(captured));
        } catch (_) {
          return Promise.resolve(false);
        }
        if (captured === null || captured === false) {
          return Promise.resolve(false);
        }
        try {
          captured = this.normalize(captured, false);
        } catch (_) {
          return Promise.resolve(false);
        }
        if (!captured) {
          return Promise.resolve(false);
        }
      }

      let now;
      try {
        now = validDate(this.clock());
      } catch (_) {
        return Promise.resolve(false);
      }
      captured.occurred_at = validISODate(captured.occurred_at) || now.toISOString();

      const singleBody = safeSerialize(this.projectKey, [captured]);
      if (singleBody === null || utf8Length(singleBody) > this.maxBatchBytes) {
        this.stats.failed++;
        this.stats.dropped++;
        return Promise.resolve(false);
      }
      if (!this.hasBudget(now.getTime())) {
        return Promise.resolve(false);
      }

      if (!this.enqueue(captured)) {
        return Promise.resolve(false);
      }
      this.sentAt.push(now.getTime());
      if (this.queue.length >= this.batchSize) {
        if (this.flushPromise) {
          return Promise.resolve(true);
        }
        return this.flush();
      }
      this.scheduleFlush();
      return Promise.resolve(true);
    }

    normalize(candidate, applyDefaults) {
      if (!candidate || typeof candidate !== "object") {
        return null;
      }
      const candidateKind = cleanString(safeRead(candidate, "kind"));
      const kind = EVENT_KINDS.has(candidateKind) ? candidateKind : "error";
      const message = truncateUTF8(cleanString(safeRead(candidate, "message")), LIMITS.message);
      const sourceURL = truncateUTF8(
        scrubURL(safeRead(candidate, "source_url"), this.runtime),
        LIMITS.url,
      );
      if (kind === "resource_error" && !sourceURL) {
        return null;
      }
      if (kind !== "resource_error" && !message) {
        return null;
      }

      const pageValue = safeRead(candidate, "page_url") ||
        (applyDefaults ? readLocation(this.runtime) : "");
      const pageURL = truncateUTF8(scrubURL(pageValue, this.runtime), LIMITS.url);
      return compactObject({
        kind,
        message,
        stack: truncateUTF8(cleanString(safeRead(candidate, "stack")), LIMITS.stack),
        source_url: sourceURL,
        page_url: pageURL,
        line: nonNegativeInteger(safeRead(candidate, "line")),
        column: nonNegativeInteger(safeRead(candidate, "column")),
        occurred_at: validISODate(safeRead(candidate, "occurred_at")),
        release: truncateUTF8(
          cleanString(safeRead(candidate, "release") || (applyDefaults ? this.release : "")),
          LIMITS.release,
        ),
        environment: truncateUTF8(
          cleanString(
            safeRead(candidate, "environment") || (applyDefaults ? this.environment : ""),
          ),
          LIMITS.environment,
        ),
        tags: normalizeTags(applyDefaults
          ? mergeTags(this.tags, safeRead(candidate, "tags"))
          : safeRead(candidate, "tags")),
      });
    }

    hasBudget(now) {
      const cutoff = now - 60_000;
      while (this.sentAt.length && this.sentAt[0] <= cutoff) {
        this.sentAt.shift();
      }
      return this.sentAt.length < this.maxEventsPerMinute;
    }

    enqueue(captured) {
      if (this.queue.length + this.reservedCount >= this.maxQueueSize) {
        if (!this.queue.length) {
          this.stats.dropped++;
          return false;
        }
        this.queue.shift();
        this.stats.dropped++;
      }
      this.queue.push(cloneEvent(captured));
      return true;
    }

    flush() {
      this.clearFlushTimer();
      if (!this.queue.length) {
        return this.flushPromise || Promise.resolve(true);
      }
      const pending = this.queue.splice(0);
      const active = this.flushPromise;
      if (active) {
        this.reservedCount += pending.length;
      }
      const send = () => {
        return this.sendPending(pending, Boolean(active));
      };
      const delivery = active
        ? active.then(
          (accepted) => send().then((drained) => accepted && drained),
          () => send().then(() => false),
        )
        : send();
      let tracked;
      tracked = delivery.finally(() => {
        if (this.flushPromise !== tracked) {
          return;
        }
        this.flushPromise = null;
        if (this.queue.length >= this.batchSize) {
          this.settle(this.flush());
        } else {
          this.scheduleFlush();
        }
      });
      this.flushPromise = tracked;
      return tracked;
    }

    sendPending(pending, reserved) {
      const grouped = this.makeBatches(pending);
      if (grouped.rejected) {
        if (reserved) {
          this.reservedCount -= grouped.rejected;
        }
        this.stats.failed += grouped.rejected;
        this.stats.dropped += grouped.rejected;
      }
      return grouped.batches.reduce(
        (result, events) => result.then((accepted) => {
          if (reserved) {
            this.reservedCount -= events.length;
          }
          return this.sendBatch(events, 0).then((batchAccepted) => accepted && batchAccepted);
        }),
        Promise.resolve(grouped.rejected === 0),
      );
    }

    makeBatches(pending) {
      const batches = [];
      let current = [];
      let rejected = 0;
      for (const captured of pending) {
        const candidate = current.concat([captured]);
        const candidateBody = safeSerialize(this.projectKey, candidate);
        if (candidateBody === null) {
          rejected++;
          continue;
        }
        if (utf8Length(candidateBody) > this.maxBatchBytes) {
          if (!current.length) {
            rejected++;
            continue;
          }
          batches.push(current);
          const singleBody = safeSerialize(this.projectKey, [captured]);
          if (singleBody === null || utf8Length(singleBody) > this.maxBatchBytes) {
            rejected++;
            current = [];
          } else {
            current = [captured];
          }
        } else {
          current = candidate;
        }
        if (current.length >= this.batchSize) {
          batches.push(current);
          current = [];
        }
      }
      if (current.length) {
        batches.push(current);
      }
      return { batches, rejected };
    }

    sendBatch(events, attempt) {
      const payload = { project_key: this.projectKey, events };
      const body = safeSerialize(this.projectKey, events);
      if (body === null) {
        return this.retryBatch(events, attempt);
      }
      let result;
      try {
        result = Promise.resolve(this.transport(body, payload));
      } catch (_) {
        result = Promise.reject(new Error("transport failed"));
      }
      return result.then(
        (accepted) => accepted === false
          ? this.retryBatch(events, attempt)
          : this.acceptBatch(events),
        () => this.retryBatch(events, attempt),
      );
    }

    acceptBatch(events) {
      this.stats.sent += events.length;
      this.stats.batches++;
      return true;
    }

    retryBatch(events, attempt) {
      if (attempt < this.maxRetries) {
        this.stats.retries++;
        const delay = this.retryBaseDelay * (2 ** attempt);
        return this.wait(delay).then(() => this.sendBatch(events, attempt + 1));
      }
      this.stats.failed += events.length;
      this.stats.dropped += events.length;
      return false;
    }

    wait(delay) {
      const setTimer = safeRead(this.runtime, "setTimeout");
      if (typeof setTimer !== "function") {
        return Promise.resolve();
      }
      return new Promise((resolve) => {
        setTimer.call(this.runtime, resolve, delay);
      });
    }

    scheduleFlush() {
      if (this.flushTimer !== null || !this.queue.length || this.flushInterval === 0) {
        return;
      }
      const setTimer = safeRead(this.runtime, "setTimeout");
      if (typeof setTimer !== "function") {
        return;
      }
      this.flushTimer = setTimer.call(this.runtime, () => {
        this.flushTimer = null;
        this.settle(this.flush());
      }, this.flushInterval);
      if (this.flushTimer && typeof this.flushTimer.unref === "function") {
        this.flushTimer.unref();
      }
    }

    clearFlushTimer() {
      if (this.flushTimer === null) {
        return;
      }
      const clearTimer = safeRead(this.runtime, "clearTimeout");
      if (typeof clearTimer === "function") {
        clearTimer.call(this.runtime, this.flushTimer);
      }
      this.flushTimer = null;
    }

    getStats() {
      return Object.freeze({
        queued: this.queue.length + this.reservedCount,
        sent: this.stats.sent,
        dropped: this.stats.dropped,
        failed: this.stats.failed,
        batches: this.stats.batches,
        retries: this.stats.retries,
      });
    }
  }

  function defaultBatchEndpoint(endpoint) {
    const suffixIndex = endpoint.search(/[?#]/);
    const path = suffixIndex < 0 ? endpoint : endpoint.slice(0, suffixIndex);
    const parameters = suffixIndex < 0 ? "" : endpoint.slice(suffixIndex);
    const suffix = "/api/v1/events";
    if (path.endsWith(suffix + "/batch")) {
      return path + parameters;
    }
    if (path.endsWith(suffix)) {
      return path + "/batch" + parameters;
    }
    return path.replace(/\/$/, "") + "/batch" + parameters;
  }

  function defaultTransport(runtime, endpoint) {
    return function send(body) {
      const keepalive = utf8Length(body) <= KEEPALIVE_BODY_LIMIT;
      const navigator = safeRead(runtime, "navigator");
      const BlobConstructor = safeRead(runtime, "Blob");
      const sendBeacon = safeRead(navigator, "sendBeacon");
      if (keepalive && typeof sendBeacon === "function" &&
          typeof BlobConstructor === "function") {
        try {
          const blob = new BlobConstructor([body], { type: "text/plain;charset=UTF-8" });
          if (sendBeacon.call(navigator, endpoint, blob)) {
            return true;
          }
        } catch (_) {
          // Fall through to fetch when Beacon is unavailable or rejected.
        }
      }

      const fetchFunction = safeRead(runtime, "fetch");
      if (typeof fetchFunction !== "function") {
        return false;
      }
      return fetchFunction.call(runtime, endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body,
        credentials: "omit",
        keepalive,
      }).then((response) => Boolean(response && response.ok));
    };
  }

  function integerOption(value, fallback, minimum, maximum, name) {
    value = value === undefined ? fallback : Number(value);
    if (!Number.isInteger(value) || value < minimum || value > maximum) {
      throw new TypeError(
        `ErrorTracer ${name} must be an integer between ${minimum} and ${maximum}`,
      );
    }
    return value;
  }

  function errorDetails(error) {
    const message = cleanString(safeRead(error, "message")) || safeString(error) || "Unknown error";
    return {
      message,
      stack: cleanString(safeRead(error, "stack")),
    };
  }

  function rejectionDetails(reason) {
    const details = errorDetails(reason);
    if (details.message !== "[object Object]") {
      return details;
    }
    try {
      const serialized = JSON.stringify(reason);
      if (serialized) {
        details.message = serialized;
      }
    } catch (_) {
      // Keep the safe string representation for circular values.
    }
    return details;
  }

  function normalizeTags(input) {
    if (!input || typeof input !== "object") {
      return undefined;
    }
    const tags = {};
    let count = 0;
    for (const [rawKey, rawValue] of safeEntries(input)) {
      if (count >= LIMITS.tagCount) {
        break;
      }
      const key = truncateUTF8(cleanString(rawKey), LIMITS.tagKey);
      if (!key || key === "__proto__" || key === "prototype" || key === "constructor") {
        continue;
      }
      tags[key] = truncateUTF8(cleanString(rawValue), LIMITS.tagValue);
      count++;
    }
    return count ? tags : undefined;
  }

  function mergeTags(...sources) {
    const merged = Object.create(null);
    for (const source of sources) {
      for (const [key, value] of safeEntries(source)) {
        merged[key] = value;
      }
    }
    return merged;
  }

  function safeEntries(value) {
    try {
      return value && typeof value === "object" ? Object.entries(value) : [];
    } catch (_) {
      return [];
    }
  }

  function scrubURL(value, runtime) {
    value = cleanString(value);
    if (!value) {
      return "";
    }
    try {
      const RuntimeURL = safeRead(runtime, "URL") ||
        (typeof URL === "function" ? URL : null);
      if (!RuntimeURL) {
        throw new Error("URL is unavailable");
      }
      const base = readLocation(runtime) || undefined;
      const parsed = base ? new RuntimeURL(value, base) : new RuntimeURL(value);
      parsed.username = "";
      parsed.password = "";
      parsed.search = "";
      parsed.hash = "";
      return parsed.toString();
    } catch (_) {
      return value.split("#", 1)[0].split("?", 1)[0];
    }
  }

  function readLocation(runtime) {
    try {
      const location = safeRead(runtime, "location");
      return location ? cleanString(safeRead(location, "href")) : "";
    } catch (_) {
      return "";
    }
  }

  function removeListener(target, type, listener, options) {
    try {
      const remove = safeRead(target, "removeEventListener");
      if (typeof remove === "function") {
        remove.call(target, type, listener, options);
      }
    } catch (_) {
      // Listener cleanup is best-effort and must not escape into the host page.
    }
  }

  function safeSerialize(projectKey, events) {
    try {
      const serializedEvents = events.map((value) => {
        const captured = Object.create(null);
        for (const [key, item] of Object.entries(value)) {
          if (key === "tags" && item && typeof item === "object") {
            const tags = Object.create(null);
            for (const [tagKey, tagValue] of Object.entries(item)) {
              tags[tagKey] = tagValue;
            }
            captured[key] = tags;
          } else {
            captured[key] = item;
          }
        }
        return captured;
      });
      Object.defineProperty(serializedEvents, "toJSON", { value: undefined });
      const envelope = Object.create(null);
      envelope.project_key = projectKey;
      envelope.events = serializedEvents;
      const serialized = JSON.stringify(envelope);
      return typeof serialized === "string" ? serialized : null;
    } catch (_) {
      return null;
    }
  }

  function safeRead(value, property) {
    try {
      return value && value[property];
    } catch (_) {
      return "";
    }
  }

  function safeString(value) {
    try {
      return value === undefined || value === null ? "" : String(value);
    } catch (_) {
      return "";
    }
  }

  function cleanString(value) {
    return safeString(value).trim();
  }

  function utf8Length(value) {
    let bytes = 0;
    for (const character of safeString(value)) {
      const point = character.codePointAt(0);
      bytes += point <= 0x7f ? 1 : point <= 0x7ff ? 2 : point <= 0xffff ? 3 : 4;
    }
    return bytes;
  }

  function truncateUTF8(value, maximum) {
    value = safeString(value);
    let bytes = 0;
    const characters = [];
    for (const character of value) {
      const point = character.codePointAt(0);
      const size = point <= 0x7f ? 1 : point <= 0x7ff ? 2 : point <= 0xffff ? 3 : 4;
      if (bytes + size > maximum) {
        break;
      }
      characters.push(character);
      bytes += size;
    }
    return characters.join("");
  }

  function nonNegativeInteger(value) {
    value = Number(value);
    return Number.isInteger(value) && value > 0 ? value : undefined;
  }

  function validISODate(value) {
    if (!value) {
      return undefined;
    }
    try {
      const date = new Date(value);
      return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
    } catch (_) {
      return undefined;
    }
  }

  function validDate(value) {
    try {
      const date = value instanceof Date ? value : new Date(value);
      return Number.isNaN(date.getTime()) ? new Date() : date;
    } catch (_) {
      return new Date();
    }
  }

  function compactObject(value) {
    for (const key of Object.keys(value)) {
      if (value[key] === undefined || value[key] === "") {
        delete value[key];
      }
    }
    return value;
  }

  function cloneEvent(value) {
    return Object.assign({}, value, value.tags ? { tags: Object.assign({}, value.tags) } : {});
  }

  return Object.freeze({
    Client,
    init(options) {
      return new Client(options);
    },
  });
});
