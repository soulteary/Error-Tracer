// SPDX-License-Identifier: Apache-2.0

"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const ErrorTracer = require("./error-tracer.js");

const PROJECT_KEY = "0123456789abcdef";
const FIXED_TIME = new Date("2026-08-29T03:00:00.000Z");

test("validates client options", () => {
  assert.throws(() => ErrorTracer.init(), /projectKey/);
  assert.throws(() => ErrorTracer.init("invalid"), /options/);
  assert.throws(() => ErrorTracer.init({ projectKey: "short" }), /projectKey/);
  assert.throws(() => ErrorTracer.init({ projectKey: "界".repeat(5) }), /projectKey/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, endpoint: " " }), /endpoint/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, sampleRate: -0.1 }), /sampleRate/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, sampleRate: 1.1 }), /sampleRate/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, maxEventsPerMinute: 0 }), /maxEventsPerMinute/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, maxEventsPerMinute: 1001 }), /maxEventsPerMinute/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, beforeSend: true }), /beforeSend/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, transport: true }), /transport/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, autoCapture: "yes" }), /autoCapture/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, batchSize: 0 }), /batchSize/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, batchSize: 101 }), /batchSize/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, flushInterval: -1 }), /flushInterval/);
  assert.throws(() => ErrorTracer.init({
    projectKey: PROJECT_KEY, batchSize: 10, maxQueueSize: 9,
  }), /maxQueueSize/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, maxRetries: 6 }), /maxRetries/);
  assert.throws(() => ErrorTracer.init({ projectKey: PROJECT_KEY, maxBatchBytes: 1023 }), /maxBatchBytes/);
  assert.throws(() => ErrorTracer.init({
    projectKey: PROJECT_KEY, maxBatchBytes: (900 * 1024) + 1,
  }), /maxBatchBytes/);

  assert.ok(ErrorTracer.init({
    projectKey: "界".repeat(6),
    transport() {
      return true;
    },
  }) instanceof ErrorTracer.Client);
});

test("captures and normalizes an exception", async () => {
  let body;
  let payload;
  const runtime = {
    location: {
      href: "https://user:password@app.example/checkout?token=secret#fragment",
    },
  };
  const client = ErrorTracer.init({
    runtime,
    endpoint: "https://errors.example/api/v1/events",
    projectKey: PROJECT_KEY,
    release: " web@1 ",
    environment: " production ",
    tags: { region: " ap-southeast-1 " },
    random: () => 0,
    clock: () => FIXED_TIME,
    batchSize: 1,
    transport(nextBody, nextPayload) {
      body = nextBody;
      payload = nextPayload;
      return true;
    },
  });
  const error = new Error(" boom ");
  error.stack = " Error: boom\n    at checkout (app.js:10:5) ";

  const accepted = await client.captureException(error, {
    sourceURL: "https://cdn.example/app.js?build=1#source",
    line: 10,
    column: 5,
    tags: { operation: " checkout " },
  });

  assert.equal(accepted, true);
  assert.deepEqual(JSON.parse(body), payload);
  assert.equal(payload.project_key, PROJECT_KEY);
  assert.deepEqual(payload.events, [{
    kind: "error",
    message: "boom",
    stack: "Error: boom\n    at checkout (app.js:10:5)",
    source_url: "https://cdn.example/app.js",
    page_url: "https://app.example/checkout",
    line: 10,
    column: 5,
    occurred_at: FIXED_TIME.toISOString(),
    release: "web@1",
    environment: "production",
    tags: {
      region: "ap-southeast-1",
      operation: "checkout",
    },
  }]);
});

test("captures messages and bounds UTF-8 fields", async () => {
  const events = [];
  const client = testClient({
    release: "r".repeat(200),
    environment: "e".repeat(200),
    tags: Object.fromEntries(
      Array.from({ length: 40 }, (_, index) => [`tag-${index}`, "界".repeat(100)]),
    ),
    transport(_body, payload) {
      events.push(payload.events[0]);
      return true;
    },
  });

  assert.equal(await client.captureMessage("界".repeat(2000)), true);
  assert.equal(await client.captureMessage("   "), false);
  assert.equal(await client.capture({ kind: "resource_error" }), false);

  assert.equal(Buffer.byteLength(events[0].message), 4095);
  assert.equal(Buffer.byteLength(events[0].release), 128);
  assert.equal(Buffer.byteLength(events[0].environment), 128);
  assert.equal(Object.keys(events[0].tags).length, 32);
  assert.ok(Object.values(events[0].tags).every((value) => Buffer.byteLength(value) <= 256));
});

test("beforeSend can modify or drop an event", async () => {
  const events = [];
  const client = testClient({
    beforeSend(event) {
      if (event.message === "drop") {
        return null;
      }
      event.message = `filtered: ${event.message}`;
      event.tags = { filtered: "yes" };
      return event;
    },
    transport(_body, payload) {
      events.push(payload.events[0]);
      return true;
    },
  });

  assert.equal(await client.captureMessage("drop"), false);
  assert.equal(await client.captureMessage("keep"), true);
  assert.equal(events.length, 1);
  assert.equal(events[0].message, "filtered: keep");
  assert.deepEqual(events[0].tags, { filtered: "yes" });
});

test("beforeSend can remove default context", async () => {
  let captured;
  const client = testClient({
    release: "web@1",
    environment: "production",
    tags: { region: "us-east-1" },
    beforeSend(event) {
      delete event.page_url;
      delete event.release;
      delete event.environment;
      delete event.tags;
      return event;
    },
    transport(_body, payload) {
      captured = payload.events[0];
      return true;
    },
  });

  assert.equal(await client.captureMessage("redacted"), true);
  assert.equal("page_url" in captured, false);
  assert.equal("release" in captured, false);
  assert.equal("environment" in captured, false);
  assert.equal("tags" in captured, false);
});

test("sampling and the per-minute budget bound traffic", async () => {
  let sent = 0;
  const sampledOut = testClient({
    sampleRate: 0.5,
    random: () => 0.75,
    transport() {
      sent++;
      return true;
    },
  });
  assert.equal(await sampledOut.captureMessage("sampled"), false);
  assert.equal(sent, 0);

  let now = FIXED_TIME;
  const limited = testClient({
    maxEventsPerMinute: 2,
    random: () => 0,
    clock: () => now,
    transport() {
      sent++;
      return true;
    },
  });
  assert.equal(await limited.captureMessage("one"), true);
  assert.equal(await limited.captureMessage("two"), true);
  assert.equal(await limited.captureMessage("three"), false);
  now = new Date(now.getTime() + 60_001);
  assert.equal(await limited.captureMessage("four"), true);
  assert.equal(sent, 3);
});

test("default transport prefers sendBeacon with a simple text body", async () => {
  let beacon;
  let fetchCalled = false;
  class FakeBlob {
    constructor(parts, options) {
      this.parts = parts;
      this.type = options.type;
    }
  }
  const runtime = {
    Blob: FakeBlob,
    location: { href: "https://app.example/page" },
    navigator: {
      sendBeacon(endpoint, blob) {
        beacon = { endpoint, blob };
        return true;
      },
    },
    fetch() {
      fetchCalled = true;
      return Promise.resolve({ ok: true });
    },
  };
  const client = ErrorTracer.init({
    projectKey: PROJECT_KEY,
    runtime,
    endpoint: "https://errors.example/api/v1/events",
    random: () => 0,
    clock: () => FIXED_TIME,
    batchSize: 1,
  });

  assert.equal(await client.captureMessage("boom"), true);
  assert.equal(beacon.endpoint, "https://errors.example/api/v1/events/batch");
  assert.equal(beacon.blob.type, "text/plain;charset=UTF-8");
  assert.equal(JSON.parse(beacon.blob.parts[0]).events[0].message, "boom");
  assert.equal(fetchCalled, false);
});

test("default transport falls back to credential-free fetch", async () => {
  let request;
  const runtime = {
    location: { href: "https://app.example/page" },
    navigator: {
      sendBeacon() {
        return false;
      },
    },
    Blob,
    fetch(endpoint, options) {
      request = { endpoint, options };
      return Promise.resolve({ ok: true });
    },
  };
  const client = ErrorTracer.init({
    projectKey: PROJECT_KEY,
    runtime,
    random: () => 0,
    clock: () => FIXED_TIME,
    batchSize: 1,
  });

  assert.equal(await client.captureMessage("boom"), true);
  assert.equal(request.endpoint, "/api/v1/events/batch");
  assert.equal(request.options.method, "POST");
  assert.deepEqual(request.options.headers, { "Content-Type": "application/json" });
  assert.equal(request.options.credentials, "omit");
  assert.equal(request.options.keepalive, true);
  assert.equal(JSON.parse(request.options.body).events[0].message, "boom");
});

test("default transport disables keepalive for large payloads", async () => {
  let beaconCalled = false;
  let request;
  const runtime = {
    location: { href: "https://app.example/page" },
    navigator: {
      sendBeacon() {
        beaconCalled = true;
        return true;
      },
    },
    Blob,
    fetch(endpoint, options) {
      request = { endpoint, options };
      return Promise.resolve({ ok: true });
    },
  };
  const client = ErrorTracer.init({
    projectKey: PROJECT_KEY,
    runtime,
    random: () => 0,
    clock: () => FIXED_TIME,
    batchSize: 1,
    maxBatchBytes: 128 * 1024,
  });

  assert.equal(await client.capture({
    kind: "error",
    message: "boom",
    stack: "x".repeat(63 * 1024),
  }), true);
  assert.equal(beaconCalled, false);
  assert.equal(request.options.keepalive, false);
});

test("capture contains extension and transport failures", async () => {
  const badRandom = testClient({
    random() {
      throw new Error("random failed");
    },
  });
  assert.equal(await badRandom.captureMessage("boom"), false);

  const badHook = testClient({
    beforeSend() {
      throw new Error("hook failed");
    },
  });
  assert.equal(await badHook.captureMessage("boom"), false);

  const badTransport = testClient({
    transport() {
      return Promise.reject(new Error("offline"));
    },
  });
  assert.equal(await badTransport.captureMessage("boom"), false);

  const hostile = new Proxy({}, {
    get() {
      throw new Error("hostile getter");
    },
  });
  assert.equal(await testClient().capture(hostile), false);
});

test("capture contains JSON serialization failures", async () => {
  const original = JSON.stringify;
  let result;
  try {
    JSON.stringify = function failSerialization() {
      throw new Error("serialization failed");
    };
    result = await testClient().captureMessage("boom");
  } finally {
    JSON.stringify = original;
  }
  assert.equal(result, false);
});

test("capture rejects a non-string JSON serialization result", async () => {
  const original = JSON.stringify;
  let result;
  let transports = 0;
  try {
    JSON.stringify = () => undefined;
    result = await testClient({
      transport() {
        transports++;
        return true;
      },
    }).captureMessage("boom");
  } finally {
    JSON.stringify = original;
  }
  assert.equal(result, false);
  assert.equal(transports, 0);
});

test("capture rejects serialized bodies without the expected envelope", async () => {
  const original = JSON.stringify;
  const results = [];
  let transports = 0;
  try {
    for (const serialized of ["null", "{}", '{"project_key":"wrong","events":[]}']) {
      JSON.stringify = () => serialized;
      results.push(await testClient({
        transport() {
          transports++;
          return true;
        },
      }).captureMessage("boom"));
    }
  } finally {
    JSON.stringify = original;
  }
  assert.deepEqual(results, [false, false, false]);
  assert.equal(transports, 0);
});

test("batch serialization ignores inherited toJSON hooks", async () => {
  const original = Object.prototype.toJSON;
  const bodies = [];
  try {
    for (const replacement of [
      () => null,
      () => ({}),
      () => { throw new Error("inherited hook must not run"); },
    ]) {
      Object.prototype.toJSON = replacement;
      const client = testClient({
        batchSize: 1,
        transport(body) {
          bodies.push(body);
          return true;
        },
      });
      assert.equal(await client.captureMessage("boom"), true);
    }
  } finally {
    if (original === undefined) {
      delete Object.prototype.toJSON;
    } else {
      Object.prototype.toJSON = original;
    }
  }
  assert.equal(bodies.length, 3);
  for (const body of bodies) {
    const payload = JSON.parse(body);
    assert.equal(payload.project_key, PROJECT_KEY);
    assert.equal(payload.events.length, 1);
    assert.equal(payload.events[0].message, "boom");
  }
});

test("automatically captures runtime, resource, and rejection failures", async () => {
  const events = [];
  const runtime = fakeRuntime();
  const client = ErrorTracer.init({
    projectKey: PROJECT_KEY,
    runtime,
    random: () => 0,
    clock: () => FIXED_TIME,
    batchSize: 1,
    transport(_body, payload) {
      events.push(payload.events[0]);
      return true;
    },
  });

  assert.equal(client.installed, true);
  runtime.dispatch("error", {
    target: runtime,
    error: Object.assign(new Error("runtime boom"), { stack: "Error: runtime boom\n at app.js:7:9" }),
    message: "runtime boom",
    filename: "https://app.example/app.js?build=1",
    lineno: 7,
    colno: 9,
  });
  runtime.dispatch("error", {
    target: {
      tagName: "SCRIPT",
      currentSrc: "https://cdn.example/missing.js?token=secret#fragment",
    },
  });
  runtime.dispatch("unhandledrejection", {
    reason: Object.assign(new Error("promise boom"), { stack: "Error: promise boom\n at promise.js:2:1" }),
  });
  await eventLoopTurn();

  assert.equal(events.length, 3);
  assert.deepEqual(events.map((event) => event.kind), [
    "error",
    "resource_error",
    "unhandled_rejection",
  ]);
  assert.equal(events[0].source_url, "https://app.example/app.js");
  assert.equal(events[0].line, 7);
  assert.equal(events[0].column, 9);
  assert.equal(events[1].message, "Failed to load script");
  assert.equal(events[1].source_url, "https://cdn.example/missing.js");
  assert.equal(events[2].message, "promise boom");
});

test("automatic capture can be installed and destroyed idempotently", () => {
  const runtime = fakeRuntime();
  const client = ErrorTracer.init({
    projectKey: PROJECT_KEY,
    runtime,
    autoCapture: false,
    transport() {
      return true;
    },
  });

  assert.equal(client.installed, false);
  assert.equal(runtime.listenerCount("error"), 0);
  assert.equal(client.install(), true);
  assert.equal(client.install(), false);
  assert.equal(runtime.listenerCount("error"), 1);
  assert.equal(runtime.listenerCount("unhandledrejection"), 1);
  assert.equal(runtime.listenerCount("pagehide"), 1);
  assert.equal(runtime.document.listenerCount("visibilitychange"), 1);

  client.destroy();
  client.destroy();
  assert.equal(client.installed, false);
  assert.equal(runtime.listenerCount("error"), 0);
  assert.equal(runtime.listenerCount("unhandledrejection"), 0);
  assert.equal(runtime.listenerCount("pagehide"), 0);
  assert.equal(runtime.document.listenerCount("visibilitychange"), 0);
});

test("automatic capture rolls back partial listener installation", () => {
  const listeners = new Map();
  const runtime = {
    addEventListener(type, listener) {
      if (type === "unhandledrejection") {
        throw new Error("registration failed");
      }
      listeners.set(type, listener);
    },
    removeEventListener(type, listener) {
      if (listeners.get(type) === listener) {
        listeners.delete(type);
      }
    },
  };
  const client = ErrorTracer.init({
    projectKey: PROJECT_KEY,
    runtime,
    autoCapture: false,
  });

  assert.equal(client.install(), false);
  assert.equal(client.installed, false);
  assert.equal(listeners.size, 0);
});

test("serializes plain-object rejection reasons", async () => {
  const events = [];
  const runtime = fakeRuntime();
  ErrorTracer.init({
    projectKey: PROJECT_KEY,
    runtime,
    random: () => 0,
    clock: () => FIXED_TIME,
    batchSize: 1,
    transport(_body, payload) {
      events.push(payload.events[0]);
      return true;
    },
  });

  runtime.dispatch("unhandledrejection", {
    reason: { code: "E_CHECKOUT", retryable: false },
  });
  await eventLoopTurn();

  assert.equal(events.length, 1);
  assert.equal(events[0].kind, "unhandled_rejection");
  assert.equal(events[0].message, '{"code":"E_CHECKOUT","retryable":false}');
});

test("queues events and sends an atomic batch", async () => {
  const payloads = [];
  const client = testClient({
    batchSize: 2,
    flushInterval: 0,
    transport(_body, payload) {
      payloads.push(payload);
      return true;
    },
  });

  assert.equal(await client.captureMessage("one"), true);
  assert.deepEqual(client.getStats(), {
    queued: 1, sent: 0, dropped: 0, failed: 0, batches: 0, retries: 0,
  });
  assert.equal(await client.captureMessage("two"), true);
  assert.equal(payloads.length, 1);
  assert.deepEqual(payloads[0].events.map((item) => item.message), ["one", "two"]);
  assert.deepEqual(client.getStats(), {
    queued: 0, sent: 2, dropped: 0, failed: 0, batches: 1, retries: 0,
  });
});

test("splits a queue before the configured request-size limit", async () => {
  const payloads = [];
  const client = testClient({
    batchSize: 10,
    flushInterval: 0,
    maxBatchBytes: 1024,
    transport(_body, payload) {
      payloads.push(payload);
      return true;
    },
  });

  assert.equal(await client.captureMessage("a".repeat(700)), true);
  assert.equal(await client.captureMessage("b".repeat(700)), true);
  assert.equal(await client.flush(), true);
  assert.equal(payloads.length, 2);
  assert.deepEqual(payloads.map((payload) => payload.events.length), [1, 1]);
});

test("drops an event that exceeds maxBatchBytes by itself", async () => {
  let transportCalled = false;
  const client = testClient({
    maxBatchBytes: 1024,
    transport() {
      transportCalled = true;
      return true;
    },
  });

  assert.equal(await client.captureMessage("x".repeat(2000)), false);
  assert.equal(transportCalled, false);
  assert.deepEqual(client.getStats(), {
    queued: 0, sent: 0, dropped: 1, failed: 1, batches: 0, retries: 0,
  });
});

test("flushes a partial queue after the configured interval", async () => {
  let scheduled;
  const payloads = [];
  const runtime = {
    location: { href: "https://app.example/page" },
    setTimeout(callback, delay) {
      scheduled = { callback, delay };
      return 1;
    },
    clearTimeout() {},
  };
  const client = testClient({
    runtime,
    batchSize: 10,
    flushInterval: 75,
    transport(_body, payload) {
      payloads.push(payload);
      return true;
    },
  });

  assert.equal(await client.captureMessage("scheduled"), true);
  assert.equal(scheduled.delay, 75);
  scheduled.callback();
  await eventLoopTurn();
  assert.equal(payloads.length, 1);
  assert.equal(payloads[0].events[0].message, "scheduled");
});

test("bounds the queue while another batch is in flight", async () => {
  let releaseFirst;
  const payloads = [];
  const firstDelivery = new Promise((resolve) => {
    releaseFirst = resolve;
  });
  const client = testClient({
    batchSize: 1,
    maxQueueSize: 2,
    flushInterval: 0,
    transport(_body, payload) {
      payloads.push(payload);
      return payloads.length === 1 ? firstDelivery : true;
    },
  });

  const firstCapture = client.captureMessage("one");
  assert.equal(await client.captureMessage("two"), true);
  assert.equal(await client.captureMessage("three"), true);
  assert.equal(await client.captureMessage("four"), true);
  assert.equal(client.getStats().dropped, 1);

  releaseFirst(true);
  assert.equal(await firstCapture, true);
  await eventLoopTurn();
  assert.equal(await client.flush(), true);
  assert.deepEqual(
    payloads.flatMap((payload) => payload.events.map((event) => event.message)),
    ["one", "three", "four"],
  );
  assert.deepEqual(client.getStats(), {
    queued: 0, sent: 3, dropped: 1, failed: 0, batches: 3, retries: 0,
  });
});

test("rejects an oversized event before it can evict queued events", async () => {
  let releaseFirst;
  const payloads = [];
  const firstDelivery = new Promise((resolve) => {
    releaseFirst = resolve;
  });
  const client = testClient({
    batchSize: 1,
    maxQueueSize: 2,
    maxBatchBytes: 1024,
    flushInterval: 0,
    transport(_body, payload) {
      payloads.push(payload);
      return payloads.length === 1 ? firstDelivery : true;
    },
  });

  const firstCapture = client.captureMessage("one");
  assert.equal(await client.captureMessage("two"), true);
  assert.equal(await client.captureMessage("three"), true);
  assert.equal(await client.captureMessage("x".repeat(2000)), false);
  assert.deepEqual(client.getStats(), {
    queued: 2, sent: 0, dropped: 1, failed: 1, batches: 0, retries: 0,
  });

  releaseFirst(true);
  assert.equal(await firstCapture, true);
  await eventLoopTurn();
  assert.equal(await client.flush(), true);
  assert.deepEqual(
    payloads.flatMap((payload) => payload.events.map((event) => event.message)),
    ["one", "two", "three"],
  );
});

test("flush waits for events queued during an active delivery", async () => {
  let releaseFirst;
  let releaseSecond;
  const deliveries = [
    new Promise((resolve) => { releaseFirst = resolve; }),
    new Promise((resolve) => { releaseSecond = resolve; }),
  ];
  const messages = [];
  const client = testClient({
    transport(_body, payload) {
      messages.push(payload.events[0].message);
      return deliveries[messages.length - 1];
    },
  });

  const firstCapture = client.captureMessage("one");
  assert.equal(await client.captureMessage("two"), true);
  let flushResolved = false;
  const flush = client.flush().then((accepted) => {
    flushResolved = true;
    return accepted;
  });

  releaseFirst(true);
  assert.equal(await firstCapture, true);
  await eventLoopTurn();
  assert.deepEqual(messages, ["one", "two"]);
  assert.equal(flushResolved, false);

  releaseSecond(true);
  assert.equal(await flush, true);
  assert.equal(flushResolved, true);
});

test("flush reserves the events queued when it is called", async () => {
  let releaseFirst;
  const payloads = [];
  const firstDelivery = new Promise((resolve) => {
    releaseFirst = resolve;
  });
  const client = testClient({
    batchSize: 1,
    maxQueueSize: 1,
    maxEventsPerMinute: 3,
    flushInterval: 0,
    transport(_body, payload) {
      payloads.push(payload);
      return payloads.length === 1 ? firstDelivery : true;
    },
  });

  const firstCapture = client.captureMessage("one");
  assert.equal(await client.captureMessage("two"), true);
  const flush = client.flush();
  assert.equal(await client.captureMessage("three"), false);
  assert.deepEqual(client.getStats(), {
    queued: 1, sent: 0, dropped: 1, failed: 0, batches: 0, retries: 0,
  });

  releaseFirst(true);
  assert.equal(await firstCapture, true);
  assert.equal(await flush, true);
  await eventLoopTurn();
  assert.equal(await client.flush(), true);
  assert.deepEqual(
    payloads.flatMap((payload) => payload.events.map((event) => event.message)),
    ["one", "two"],
  );
  assert.equal(client.getStats().dropped, 1);
  assert.equal(await client.captureMessage("four"), true);
  assert.deepEqual(
    payloads.flatMap((payload) => payload.events.map((event) => event.message)),
    ["one", "two", "four"],
  );
});

test("bounds snapshots reserved behind a stalled delivery", async () => {
  let releaseFirst;
  const payloads = [];
  const firstDelivery = new Promise((resolve) => {
    releaseFirst = resolve;
  });
  const client = testClient({
    batchSize: 3,
    maxQueueSize: 3,
    flushInterval: 0,
    transport(_body, payload) {
      payloads.push(payload);
      return payloads.length === 1 ? firstDelivery : true;
    },
  });

  assert.equal(await client.captureMessage("one"), true);
  const firstFlush = client.flush();
  for (const message of ["two", "three", "four"]) {
    assert.equal(await client.captureMessage(message), true);
    client.flush();
  }
  assert.equal(await client.captureMessage("five"), false);
  assert.deepEqual(client.getStats(), {
    queued: 3, sent: 0, dropped: 1, failed: 0, batches: 0, retries: 0,
  });

  releaseFirst(true);
  assert.equal(await firstFlush, true);
  assert.equal(await client.flush(), true);
  assert.deepEqual(
    payloads.flatMap((payload) => payload.events.map((event) => event.message)),
    ["one", "two", "three", "four"],
  );
  assert.deepEqual(client.getStats(), {
    queued: 0, sent: 4, dropped: 1, failed: 0, batches: 4, retries: 0,
  });
});

test("keeps unsent sub-batches reserved behind a stalled sub-batch", async () => {
  let releaseFirst;
  let releaseSecond;
  const deliveries = [
    new Promise((resolve) => { releaseFirst = resolve; }),
    new Promise((resolve) => { releaseSecond = resolve; }),
  ];
  const payloads = [];
  const client = testClient({
    batchSize: 2,
    maxQueueSize: 3,
    flushInterval: 0,
    transport(_body, payload) {
      payloads.push(payload);
      return deliveries[payloads.length - 1] || true;
    },
  });

  assert.equal(await client.captureMessage("one"), true);
  const firstFlush = client.captureMessage("two");
  for (const message of ["three", "four", "five"]) {
    assert.equal(await client.captureMessage(message), true);
  }
  const pendingFlush = client.flush();

  releaseFirst(true);
  assert.equal(await firstFlush, true);
  await eventLoopTurn();
  assert.deepEqual(
    payloads.map((payload) => payload.events.map((event) => event.message)),
    [["one", "two"], ["three", "four"]],
  );
  assert.equal(client.getStats().queued, 1);

  for (const message of ["six", "seven", "eight"]) {
    assert.equal(await client.captureMessage(message), true);
  }
  assert.deepEqual(client.getStats(), {
    queued: 3, sent: 2, dropped: 1, failed: 0, batches: 1, retries: 0,
  });

  releaseSecond(true);
  assert.equal(await pendingFlush, true);
  assert.equal(await client.flush(), true);
  assert.deepEqual(
    payloads.flatMap((payload) => payload.events.map((event) => event.message)),
    ["one", "two", "three", "four", "five", "seven", "eight"],
  );
  assert.deepEqual(client.getStats(), {
    queued: 0, sent: 7, dropped: 1, failed: 0, batches: 4, retries: 0,
  });
});

test("keeps initial unsent sub-batches reserved behind a stalled sub-batch", async () => {
  let releaseFirst;
  const firstDelivery = new Promise((resolve) => { releaseFirst = resolve; });
  const payloads = [];
  const client = testClient({
    batchSize: 3,
    maxQueueSize: 3,
    maxBatchBytes: 1024,
    flushInterval: 0,
    transport(_body, payload) {
      payloads.push(payload);
      return payloads.length === 1 ? firstDelivery : true;
    },
  });
  const large = (label) => `${label}:${"x".repeat(700)}`;
  const messages = ["one", "two", "three", "four", "five", "six"].map(large);

  assert.equal(await client.captureMessage(messages[0]), true);
  assert.equal(await client.captureMessage(messages[1]), true);
  const initialFlush = client.captureMessage(messages[2]);
  await eventLoopTurn();
  assert.deepEqual(payloads.map((payload) => payload.events.length), [1]);
  assert.equal(client.getStats().queued, 2);

  for (const message of messages.slice(3)) {
    assert.equal(await client.captureMessage(message), true);
  }
  assert.deepEqual(client.getStats(), {
    queued: 3, sent: 0, dropped: 2, failed: 0, batches: 0, retries: 0,
  });

  releaseFirst(true);
  assert.equal(await initialFlush, true);
  assert.equal(await client.flush(), true);
  assert.deepEqual(
    payloads.flatMap((payload) => payload.events.map((event) => event.message)),
    [messages[0], messages[1], messages[2], messages[5]],
  );
  assert.deepEqual(client.getStats(), {
    queued: 0, sent: 4, dropped: 2, failed: 0, batches: 4, retries: 0,
  });
});

test("retries failed batches with a finite budget", async () => {
  let attempts = 0;
  const runtime = {
    location: { href: "https://app.example/page" },
    setTimeout(callback) {
      callback();
      return 1;
    },
    clearTimeout() {},
  };
  const client = testClient({
    runtime,
    batchSize: 1,
    maxRetries: 2,
    retryBaseDelay: 1,
    transport() {
      attempts++;
      return false;
    },
  });

  assert.equal(await client.captureMessage("offline"), false);
  assert.equal(attempts, 3);
  assert.deepEqual(client.getStats(), {
    queued: 0, sent: 0, dropped: 1, failed: 1, batches: 0, retries: 2,
  });
});

test("pagehide flushes a partial queue", async () => {
  const payloads = [];
  const runtime = fakeRuntime();
  const client = testClient({
    runtime,
    batchSize: 10,
    flushInterval: 0,
    transport(_body, payload) {
      payloads.push(payload);
      return true;
    },
  });

  assert.equal(await client.captureMessage("leaving"), true);
  assert.equal(payloads.length, 0);
  runtime.dispatch("pagehide", {});
  await eventLoopTurn();
  assert.equal(payloads.length, 1);
  assert.equal(payloads[0].events[0].message, "leaving");
});

test("visibilitychange flushes when the document becomes hidden", async () => {
  const payloads = [];
  const runtime = fakeRuntime();
  const client = testClient({
    runtime,
    batchSize: 10,
    flushInterval: 0,
    transport(_body, payload) {
      payloads.push(payload);
      return true;
    },
  });

  assert.equal(await client.captureMessage("backgrounded"), true);
  runtime.document.visibilityState = "visible";
  runtime.document.dispatch("visibilitychange", {});
  await eventLoopTurn();
  assert.equal(payloads.length, 0);

  runtime.document.visibilityState = "hidden";
  runtime.document.dispatch("visibilitychange", {});
  await eventLoopTurn();
  assert.equal(payloads.length, 1);
  assert.equal(payloads[0].events[0].message, "backgrounded");
});

function testClient(overrides) {
  return ErrorTracer.init({
    projectKey: PROJECT_KEY,
    runtime: { location: { href: "https://app.example/page" } },
    random: () => 0,
    clock: () => FIXED_TIME,
    batchSize: 1,
    transport() {
      return true;
    },
    ...overrides,
  });
}

function fakeRuntime() {
  const runtimeTarget = fakeEventTarget();
  const documentTarget = fakeEventTarget();
  return Object.assign(runtimeTarget, {
    location: { href: "https://app.example/page?secret=1#fragment" },
    document: Object.assign(documentTarget, { visibilityState: "visible" }),
  });
}

function fakeEventTarget() {
  const listeners = new Map();
  return {
    addEventListener(type, listener) {
      const registered = listeners.get(type) || new Set();
      registered.add(listener);
      listeners.set(type, registered);
    },
    removeEventListener(type, listener) {
      const registered = listeners.get(type);
      if (registered) {
        registered.delete(listener);
      }
    },
    dispatch(type, event) {
      for (const listener of listeners.get(type) || []) {
        listener(event);
      }
    },
    listenerCount(type) {
      return (listeners.get(type) || new Set()).size;
    },
  };
}

function eventLoopTurn() {
  return new Promise((resolve) => setImmediate(resolve));
}
