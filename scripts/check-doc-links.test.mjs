// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { checkMarkdownLinks, markdownTargets } from "./check-doc-links.mjs";

test("extracts inline and reference-definition targets", () => {
  const contents = [
    "[inline](docs/inline.md)",
    "[guide][guide-ref]",
    "[guide-ref]: <docs/guide.md> \"Guide\"",
    "![asset][asset-ref]",
    "[asset-ref]: images/example.png 'Example'",
  ].join("\n");

  assert.deepEqual(markdownTargets(contents), [
    "docs/inline.md",
    "docs/guide.md",
    "images/example.png",
  ]);
});

test("ignores reference definitions in fenced code", () => {
  assert.deepEqual(markdownTargets([
    "```markdown",
    "[missing]: docs/missing.md",
    "```",
  ].join("\n")), []);
});

test("ignores fenced code nested in block containers", () => {
  assert.deepEqual(markdownTargets([
    "> ```markdown",
    "> [quoted]: docs/quoted-missing.md",
    "> ```",
    "- ~~~markdown",
    "  [listed]: docs/listed-missing.md",
    "  ~~~",
    "> - ```markdown",
    ">   [nested]: docs/nested-missing.md",
    ">   ```",
  ].join("\n")), []);
});

test("keeps longer container fences open past shorter marker runs", () => {
  assert.deepEqual(markdownTargets([
    "> ````markdown",
    "> ```",
    "> [missing]: docs/still-fenced.md",
    "> ````",
  ].join("\n")), []);
});

test("ends unclosed fences at their container boundary", () => {
  assert.deepEqual(markdownTargets([
    "> ```markdown",
    "> quoted code",
    "",
    "[after-quote]: docs/after-quote.md",
    "- ~~~markdown",
    "  listed code",
    "",
    "[after-list]: docs/after-list.md",
  ].join("\n")), [
    "docs/after-quote.md",
    "docs/after-list.md",
  ]);
});

test("keeps a list fence open across an unindented blank line", () => {
  assert.deepEqual(markdownTargets([
    "- ```markdown",
    "  listed code",
    "",
    "  [missing]: docs/still-in-list-fence.md",
    "  ```",
  ].join("\n")), []);
});

test("keeps a mixed-container fence open across a quote-only blank", () => {
  assert.deepEqual(markdownTargets([
    "> - ```markdown",
    ">   listed code in a quote",
    ">",
    ">   [missing]: docs/still-in-mixed-fence.md",
    ">   ```",
  ].join("\n")), []);
});

test("ends an inner quote fence after an unmarked list blank", () => {
  assert.deepEqual(markdownTargets([
    "- > ```markdown",
    "  > fenced code",
    "",
    "  > [new-quote]: docs/new-quote.md",
  ].join("\n")), ["docs/new-quote.md"]);
});

test("ends an unclosed quote fence before a new quote block", () => {
  assert.deepEqual(markdownTargets([
    "> ```markdown",
    "> fenced code",
    "",
    "> [new-quote]: docs/new-quote.md",
  ].join("\n")), ["docs/new-quote.md"]);
});

test("ignores GitHub footnote definitions", () => {
  assert.deepEqual(markdownTargets([
    "A statement with a footnote.[^1]",
    "",
    "[^1]: This is explanatory prose, not a link destination.",
  ].join("\n")), []);
});

test("ignores prose that resembles a reference definition", () => {
  assert.deepEqual(markdownTargets([
    "[status]: Not yet available",
    "[owner]: Documentation team",
  ].join("\n")), []);
});

test("extracts definitions nested in block containers", () => {
  assert.deepEqual(markdownTargets([
    "> [quoted]: docs/quoted.md",
    "- [listed]: <docs/listed.md> \"Listed guide\"",
    "1. > [nested]: docs/nested.md 'Nested guide'",
  ].join("\n")), [
    "docs/quoted.md",
    "docs/listed.md",
    "docs/nested.md",
  ]);
});

test("ignores indented code after list markers", () => {
  assert.deepEqual(markdownTargets([
    "-     [unordered]: docs/unordered-missing.md",
    "1.     [ordered]: docs/ordered-missing.md",
  ].join("\n")), []);
});

test("parses mixed space-and-tab list padding", () => {
  assert.deepEqual(markdownTargets([
    "- \t```markdown",
    "    [missing](docs/missing.md)",
    "    ```",
  ].join("\n")), []);
});

test("extracts reference destinations continued on the next line", () => {
  assert.deepEqual(markdownTargets([
    "[guide]:",
    "  docs/guide.md",
    "[wrapped]:",
    "   <docs/wrapped.md> \"Wrapped guide\"",
    "10. [ordered]:",
    "    docs/ordered.md",
    "> 10. [nested]:",
    ">     docs/nested-ordered.md",
  ].join("\n")), [
    "docs/guide.md",
    "docs/wrapped.md",
    "docs/ordered.md",
    "docs/nested-ordered.md",
  ]);
});

test("extracts references with multiline titles", () => {
  assert.deepEqual(markdownTargets([
    "[quoted]: docs/quoted.md \"A long",
    "title\"",
    "[parenthesized]: docs/parenthesized.md (Another",
    "title)",
    "[next-line]: docs/next-line.md",
    "  'A title",
    "  on following lines'",
    "10. [ordered]: docs/ordered-title.md \"A list",
    "    title\"",
  ].join("\n")), [
    "docs/quoted.md",
    "docs/parenthesized.md",
    "docs/next-line.md",
    "docs/ordered-title.md",
  ]);
});

test("reports a missing local reference target", () => {
  const root = mkdtempSync(join(tmpdir(), "error-tracer-doc-links-"));
  mkdirSync(join(root, "docs"));
  writeFileSync(join(root, "docs", "exists.md"), "# Exists\n");

  assert.deepEqual(checkMarkdownLinks(root, "README.md", [
    "[exists]: docs/exists.md",
    "[missing]: docs/missing.md",
  ].join("\n")), [
    "README.md: missing link target: docs/missing.md",
  ]);
});

test("reports a missing continued reference target", () => {
  const root = mkdtempSync(join(tmpdir(), "error-tracer-doc-links-"));

  assert.deepEqual(checkMarkdownLinks(root, "README.md", [
    "[missing]:",
    "  docs/missing.md",
  ].join("\n")), [
    "README.md: missing link target: docs/missing.md",
  ]);
});
