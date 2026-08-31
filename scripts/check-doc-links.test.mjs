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
    "",
    "[guide-ref]: <docs/guide.md> \"Guide\"",
    "![asset][asset-ref]",
    "",
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

test("ignores HTML blocks and resumes after their terminators", () => {
  assert.deepEqual(markdownTargets([
    "<script></script>",
    "[after-inline]: docs/after-inline.md",
    "<style>",
    "[inside-style]: docs/not-style.md",
    "</style>",
    "[after-style]: docs/after-style.md",
    "<!--",
    "[inside-comment]: docs/not-comment.md",
    "-->",
    "[after-comment]: docs/after-comment.md",
  ].join("\n")), [
    "docs/after-inline.md",
    "docs/after-style.md",
    "docs/after-comment.md",
  ]);
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

test("preserves list context after masking block content", () => {
  assert.deepEqual(markdownTargets([
    "10. ```",
    "    code",
    "    ```",
    "    [missing]: docs/missing.md",
  ].join("\n")), ["docs/missing.md"]);
  assert.deepEqual(markdownTargets([
    "> 10. <script>",
    ">     const example = true;",
    ">     </script>",
    ">     [nested]: docs/nested.md",
  ].join("\n")), ["docs/nested.md"]);
  assert.deepEqual(markdownTargets([
    "10.     indented code",
    "    [after-code]: docs/after-code.md",
  ].join("\n")), ["docs/after-code.md"]);
});

test("preserves paragraph interruption after masking list blocks", () => {
  assert.deepEqual(markdownTargets([
    "Paragraph",
    "1. ```",
    "   code",
    "   ```",
    "[after]: docs/missing.md",
  ].join("\n")), ["docs/missing.md"]);
});

test("does not mask fences from ordered markers inside paragraphs", () => {
  assert.deepEqual(markdownTargets([
    "Paragraph",
    "2. ~~~markdown",
    "   [top-level](docs/top-level.md)",
    "   ~~~",
    "",
    "2. ~~~markdown",
    "   [masked](docs/not-visible.md)",
    "   ~~~",
    "> Quoted paragraph",
    "> 3. ~~~markdown",
    ">    [quoted](docs/quoted.md)",
    ">    ~~~",
  ].join("\n")), [
    "docs/top-level.md",
    "docs/quoted.md",
  ]);
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

test("respects ordered-list paragraph interruption rules", () => {
  assert.deepEqual(markdownTargets([
    "Paragraph text",
    "2. [continued]: docs/not-a-definition.md",
    "Paragraph interrupted by one",
    "1. [interrupting]: docs/interrupting.md",
    "",
    "2. [after-blank]: docs/after-blank.md",
    "> Nested paragraph",
    "> 3. [nested-continued]: docs/not-nested.md",
    ">",
    "> 3. [nested-after-blank]: docs/nested.md",
  ].join("\n")), [
    "docs/interrupting.md",
    "docs/after-blank.md",
    "docs/nested.md",
  ]);
});

test("preserves outer list context for nested interruptions", () => {
  assert.deepEqual(markdownTargets([
    "- Outer paragraph",
    "  2. [continued]: docs/not-a-definition.md",
  ].join("\n")), []);
  assert.deepEqual(markdownTargets([
    "- Outer paragraph",
    "  1. [nested]: docs/nested.md",
  ].join("\n")), ["docs/nested.md"]);
});

test("does not let reference definitions interrupt paragraphs", () => {
  assert.deepEqual(markdownTargets([
    "Top-level paragraph",
    "[top-level]: docs/not-top-level.md",
    "",
    "[after-blank]: docs/after-blank.md",
    "> Quoted paragraph",
    "> [quoted]: docs/not-quoted.md",
    ">",
    "> [quoted-after-blank]: docs/quoted.md",
  ].join("\n")), [
    "docs/after-blank.md",
    "docs/quoted.md",
  ]);
});

test("keeps invalid backtick fence candidates in paragraphs", () => {
  assert.deepEqual(markdownTargets([
    "Paragraph",
    "``` foo ` bar",
    "[continued]: docs/not-a-definition.md",
    "",
    "[after-blank]: docs/after-blank.md",
  ].join("\n")), ["docs/after-blank.md"]);
});

test("measures fence indentation in visual columns", () => {
  assert.deepEqual(markdownTargets([
    "Paragraph",
    "\t```",
    "[visible](docs/visible.md)",
  ].join("\n")), ["docs/visible.md"]);
  assert.deepEqual(markdownTargets([
    "```",
    "\t```",
    "[hidden](docs/not-visible.md)",
    "```",
  ].join("\n")), []);
});

test("preserves lazy block-quote paragraph continuations", () => {
  assert.deepEqual(markdownTargets([
    "> Quoted paragraph",
    "lazy continuation",
    "> [quoted]: docs/not-a-definition.md",
    ">",
    "> [after-blank]: docs/quoted.md",
    "> > Nested paragraph",
    "> lazy nested continuation",
    "> > [nested]: docs/not-a-nested-definition.md",
  ].join("\n")), ["docs/quoted.md"]);
});

test("recognizes short hyphen setext underlines", () => {
  assert.deepEqual(markdownTargets([
    "Heading",
    "-",
    "2. [after-heading]: docs/after-heading.md",
  ].join("\n")), ["docs/after-heading.md"]);
});

test("treats a tab-indented block quote as code", () => {
  assert.deepEqual(markdownTargets(
    "\t> [example]: docs/not-a-definition.md",
  ), []);
});

test("preserves tab columns after block-quote markers", () => {
  assert.deepEqual(markdownTargets([
    ">\t  [code]: docs/not-visible.md",
    "> \t[visible]: docs/visible.md",
  ].join("\n")), ["docs/visible.md"]);
});

test("ignores indented code after list markers", () => {
  assert.deepEqual(markdownTargets([
    "-     [unordered]: docs/unordered-missing.md",
    "1.     [ordered]: docs/ordered-missing.md",
  ].join("\n")), []);
});

test("ignores links in four-column indented code", () => {
  for (const contents of [
    [
      "    ```",
      "    [inline](docs/inline-missing.md)",
      "    [reference]: docs/reference-missing.md",
      "    ```",
    ].join("\n"),
    ">     [quoted](docs/quoted-missing.md)",
    "-     [listed](docs/listed-missing.md)",
    [
      "- Previous item",
      "  continuation",
      "-     [sibling-code](docs/sibling-code-missing.md)",
    ].join("\n"),
    "\t[tabbed](docs/tabbed-missing.md)",
  ]) {
    assert.deepEqual(markdownTargets(contents), []);
  }
});

test("keeps indented lazy paragraph continuations visible", () => {
  assert.deepEqual(markdownTargets([
    "Paragraph",
    "    [inline](docs/inline.md)",
  ].join("\n")), ["docs/inline.md"]);
});

test("keeps noninterrupting list-like code in paragraphs visible", () => {
  assert.deepEqual(markdownTargets([
    "Paragraph",
    "-     [unordered](docs/unordered.md)",
    "Another paragraph",
    "1.     [ordered](docs/ordered.md)",
    "",
    "-     [code](docs/not-visible.md)",
  ].join("\n")), ["docs/unordered.md", "docs/ordered.md"]);
});

test("keeps indented list continuations visible", () => {
  assert.deepEqual(markdownTargets([
    "- List item",
    "  [inline](docs/inline.md)",
    "  continued",
    "  [second](docs/second.md)",
  ].join("\n")), ["docs/inline.md", "docs/second.md"]);
});

test("preserves tab columns after list indentation", () => {
  assert.deepEqual(markdownTargets([
    "- List item",
    "",
    "  \t[continuation](docs/continuation.md)",
  ].join("\n")), ["docs/continuation.md"]);
  assert.deepEqual(markdownTargets([
    "- List item",
    "",
    "    \t[code](docs/not-visible.md)",
  ].join("\n")), []);
});

test("recognizes empty list items without breaking Setext headings", () => {
  assert.deepEqual(markdownTargets([
    "-",
    "    [unordered](docs/unordered.md)",
    "1.",
    "    [ordered](docs/ordered.md)",
    "> -",
    ">     [quoted](docs/quoted.md)",
  ].join("\n")), [
    "docs/unordered.md",
    "docs/ordered.md",
    "docs/quoted.md",
  ]);
  assert.deepEqual(markdownTargets([
    "Heading",
    "-",
    "    [code](docs/not-visible.md)",
  ].join("\n")), []);
  assert.deepEqual(markdownTargets([
    "Paragraph",
    "+",
    "    [continuation](docs/continuation.md)",
  ].join("\n")), ["docs/continuation.md"]);
});

test("resets paragraph state between sibling list items", () => {
  assert.deepEqual(markdownTargets([
    "- First item",
    "  continuation",
    "- [second]: docs/second.md",
    "> - Quoted item",
    ">   continuation",
    "> - [quoted]: docs/quoted.md",
    "- Outer item",
    "  - Inner item",
    "    continuation",
    "  - [nested]: docs/nested.md",
  ].join("\n")), [
    "docs/second.md",
    "docs/quoted.md",
    "docs/nested.md",
  ]);
});

test("parses mixed space-and-tab list padding", () => {
  assert.deepEqual(markdownTargets([
    "- \t```markdown",
    "    [missing](docs/missing.md)",
    "    ```",
    "> -\t```markdown",
    ">   [quoted](docs/quoted-missing.md)",
    ">   ```",
    "- -\t```markdown",
    "    [nested](docs/nested-missing.md)",
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
    "[indented]: docs/indented.md \"An indented",
    "    title continuation\"",
  ].join("\n")), [
    "docs/quoted.md",
    "docs/parenthesized.md",
    "docs/next-line.md",
    "docs/ordered-title.md",
    "docs/indented.md",
  ]);
});

test("scans multiline definitions with active list containers", () => {
  assert.deepEqual(markdownTargets([
    "-",
    "    [first]: docs/first.md \"A long",
    "    title\"",
    "    [second]: docs/second.md",
  ].join("\n")), ["docs/first.md", "docs/second.md"]);
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
