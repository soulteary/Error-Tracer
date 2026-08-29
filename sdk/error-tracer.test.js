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
  assert.deepEqual(payload.event, {
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
  });
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
      events.push(payload.event);
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
      events.push(payload.event);
      return true;
    },
  });

  assert.equal(await client.captureMessage("drop"), false);
  assert.equal(await client.captureMessage("keep"), true);
  assert.equal(events.length, 1);
  assert.equal(events[0].message, "filtered: keep");
  assert.deepEqual(events[0].tags, { filtered: "yes" });
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
  });

  assert.equal(await client.captureMessage("boom"), true);
  assert.equal(beacon.endpoint, "https://errors.example/api/v1/events");
  assert.equal(beacon.blob.type, "text/plain;charset=UTF-8");
  assert.equal(JSON.parse(beacon.blob.parts[0]).event.message, "boom");
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
  });

  assert.equal(await client.captureMessage("boom"), true);
  assert.equal(request.endpoint, "/api/v1/events");
  assert.equal(request.options.method, "POST");
  assert.deepEqual(request.options.headers, { "Content-Type": "application/json" });
  assert.equal(request.options.credentials, "omit");
  assert.equal(request.options.keepalive, true);
  assert.equal(JSON.parse(request.options.body).event.message, "boom");
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

function testClient(overrides) {
  return ErrorTracer.init({
    projectKey: PROJECT_KEY,
    runtime: { location: { href: "https://app.example/page" } },
    random: () => 0,
    clock: () => FIXED_TIME,
    transport() {
      return true;
    },
    ...overrides,
  });
}
