// SPDX-License-Identifier: Apache-2.0

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, isAbsolute, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const scriptPath = fileURLToPath(import.meta.url);

export function checkMarkdownLinks(root, markdownFile, contents) {
  const failures = [];
  for (const rawTarget of markdownTargets(contents)) {
    if (isExternal(rawTarget)) {
      continue;
    }
    let target;
    try {
      target = decodeURIComponent(rawTarget.split(/[?#]/, 1)[0]);
    } catch {
      failures.push(`${markdownFile}: invalid URL encoding in ${rawTarget}`);
      continue;
    }
    if (!target) {
      continue;
    }
    const resolvedTarget = target.startsWith("/")
      ? resolve(root, target.slice(1))
      : resolve(root, dirname(markdownFile), target);
    const repositoryRelative = relative(root, resolvedTarget);
    if (repositoryRelative.startsWith("..") || isAbsolute(repositoryRelative)) {
      failures.push(`${markdownFile}: link escapes the repository: ${rawTarget}`);
      continue;
    }
    if (!existsSync(resolvedTarget)) {
      failures.push(`${markdownFile}: missing link target: ${rawTarget}`);
    }
  }
  return failures;
}

export function markdownTargets(contents) {
  const markdown = withoutFencedCode(contents);
  const targets = [];
  for (const match of markdown.matchAll(
    /!?\[[^\]\n]*\]\((<[^>\n]+>|[^\s)]+)(?:\s+[^)]*)?\)/g,
  )) {
    targets.push(match[1].replace(/^<|>$/g, ""));
  }
  const lines = markdown.split("\n");
  for (let index = 0; index < lines.length; index++) {
    const definition = referenceDefinitionAt(lines, index);
    if (definition) {
      targets.push(definition.target);
      index += definition.continued ? 1 : 0;
    }
  }
  return targets;
}

function referenceDefinitionAt(lines, index) {
  const line = stripBlockContainers(lines[index]);
  const match = line.match(
    /^[ \t]{0,3}\[(?!\^)((?:\\.|[^\]\\\n])+)\]:[ \t]*(.*)$/,
  );
  if (!match) {
    return null;
  }

  let remainder = match[2];
  let continued = false;
  if (!remainder.trim()) {
    if (index + 1 >= lines.length) {
      return null;
    }
    const continuation = stripBlockContainers(lines[index + 1]);
    if (/^[ \t]{0,3}\[(?!\^)(?:\\.|[^\]\\\n])+\]:/.test(continuation)) {
      return null;
    }
    const continuationMatch = continuation.match(/^[ \t]{0,3}(\S.*)$/);
    if (!continuationMatch) {
      return null;
    }
    remainder = continuationMatch[1];
    continued = true;
  }

  const target = referenceDestination(remainder);
  return target === null ? null : { target, continued };
}

function referenceDestination(remainder) {
  const value = remainder.trim();
  let target;
  let trailing;
  if (value.startsWith("<")) {
    const match = value.match(/^<((?:\\.|[^<>\\\n])*)>(.*)$/);
    if (!match) {
      return null;
    }
    target = match[1];
    trailing = match[2].trim();
  } else {
    const match = value.match(/^(\S+)(.*)$/);
    if (!match) {
      return null;
    }
    target = match[1];
    trailing = match[2].trim();
  }

  if (trailing && !referenceTitle(trailing)) {
    return null;
  }
  return target;
}

function referenceTitle(value) {
  return /^(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\((?:\\.|[^)\\])*\))$/.test(value);
}

function stripBlockContainers(line) {
  let remaining = line.replace(/\r$/, "");
  while (true) {
    const quote = remaining.match(/^[ \t]{0,3}>[ \t]?/);
    if (quote) {
      remaining = remaining.slice(quote[0].length);
      continue;
    }
    const list = remaining.match(/^[ \t]{0,3}(?:[-+*]|\d{1,9}[.)])[ \t]+/);
    if (list) {
      remaining = remaining.slice(list[0].length);
      continue;
    }
    return remaining;
  }
}

function isExternal(target) {
  return target.startsWith("//") || /^[a-z][a-z0-9+.-]*:/i.test(target);
}

function withoutFencedCode(contents) {
  let fence = "";
  return contents.split("\n").map((line) => {
    const marker = line.match(/^\s*(```+|~~~+)/)?.[1] || "";
    if (marker && !fence) {
      fence = marker[0];
      return "";
    }
    if (marker && fence === marker[0]) {
      fence = "";
      return "";
    }
    return fence ? "" : line;
  }).join("\n");
}

function run() {
  const markdownFiles = execFileSync("git", ["ls-files", "-z", "*.md"], {
    cwd: repositoryRoot,
    encoding: "utf8",
  }).split("\0").filter(Boolean);
  const failures = markdownFiles.flatMap((markdownFile) => checkMarkdownLinks(
    repositoryRoot,
    markdownFile,
    readFileSync(resolve(repositoryRoot, markdownFile), "utf8"),
  ));

  if (failures.length) {
    process.stderr.write(`${failures.join("\n")}\n`);
    process.exitCode = 1;
  } else {
    process.stdout.write(`Checked ${markdownFiles.length} Markdown files.\n`);
  }
}

if (process.argv[1] && resolve(process.argv[1]) === scriptPath) {
  run();
}
