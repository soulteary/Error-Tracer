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

      this.release = truncateUTF8(cleanString(options.release), LIMITS.release);
      this.environment = truncateUTF8(cleanString(options.environment), LIMITS.environment);
      this.tags = normalizeTags(options.tags);
      this.beforeSend = options.beforeSend || null;
      this.random = typeof options.random === "function" ? options.random : Math.random;
      this.clock = typeof options.clock === "function" ? options.clock : () => new Date();
      this.sentAt = [];
      this.transport = options.transport || defaultTransport(this.runtime, this.endpoint);
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
        captured = this.normalize(candidate);
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
          captured = this.normalize(captured);
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
      if (!this.consumeBudget(now.getTime())) {
        return Promise.resolve(false);
      }
      captured.occurred_at = validISODate(captured.occurred_at) || now.toISOString();

      const payload = { project_key: this.projectKey, event: captured };
      const body = JSON.stringify(payload);
      try {
        return Promise.resolve(this.transport(body, payload))
          .then((result) => result !== false)
          .catch(() => false);
      } catch (_) {
        return Promise.resolve(false);
      }
    }

    normalize(candidate) {
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

      const pageURL = truncateUTF8(
        scrubURL(safeRead(candidate, "page_url") || readLocation(this.runtime), this.runtime),
        LIMITS.url,
      );
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
          cleanString(safeRead(candidate, "release") || this.release),
          LIMITS.release,
        ),
        environment: truncateUTF8(
          cleanString(safeRead(candidate, "environment") || this.environment),
          LIMITS.environment,
        ),
        tags: normalizeTags(mergeTags(this.tags, safeRead(candidate, "tags"))),
      });
    }

    consumeBudget(now) {
      const cutoff = now - 60_000;
      while (this.sentAt.length && this.sentAt[0] <= cutoff) {
        this.sentAt.shift();
      }
      if (this.sentAt.length >= this.maxEventsPerMinute) {
        return false;
      }
      this.sentAt.push(now);
      return true;
    }
  }

  function defaultTransport(runtime, endpoint) {
    return function send(body) {
      const navigator = safeRead(runtime, "navigator");
      const BlobConstructor = safeRead(runtime, "Blob");
      const sendBeacon = safeRead(navigator, "sendBeacon");
      if (typeof sendBeacon === "function" && typeof BlobConstructor === "function") {
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
        keepalive: true,
      }).then((response) => Boolean(response && response.ok));
    };
  }

  function errorDetails(error) {
    const message = cleanString(safeRead(error, "message")) || safeString(error) || "Unknown error";
    return {
      message,
      stack: cleanString(safeRead(error, "stack")),
    };
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
