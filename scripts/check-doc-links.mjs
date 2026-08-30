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
      index += definition.consumed;
    }
  }
  return targets;
}

function referenceDefinitionAt(lines, index) {
  const parsedLine = parseBlockContainers(lines[index]);
  const match = parsedLine.content.match(
    /^[ \t]{0,3}\[(?!\^)((?:\\.|[^\]\\\n])+)\]:[ \t]*(.*)$/,
  );
  if (!match) {
    return null;
  }

  let remainder = match[2];
  let consumed = 0;
  if (!remainder.trim()) {
    if (index + 1 >= lines.length) {
      return null;
    }
    const continuation = continueBlockContainers(
      lines[index + 1], parsedLine.containers,
    );
    if (continuation === null) {
      return null;
    }
    if (/^[ \t]{0,3}\[(?!\^)(?:\\.|[^\]\\\n])+\]:/.test(continuation)) {
      return null;
    }
    const continuationMatch = continuation.match(/^[ \t]{0,3}(\S.*)$/);
    if (!continuationMatch) {
      return null;
    }
    remainder = continuationMatch[1];
    consumed = 1;
  }

  const destination = referenceDestination(remainder);
  if (destination === null) {
    return null;
  }
  if (destination.trailing) {
    const title = completeReferenceTitle(
      destination.trailing, lines, index + consumed + 1, parsedLine.containers,
    );
    if (!title.valid) {
      return null;
    }
    consumed += title.consumed;
  } else {
    const nextLine = referenceContinuation(
      lines, index + consumed + 1, parsedLine.containers,
    );
    if (nextLine !== null && /^["'(]/.test(nextLine)) {
      const title = completeReferenceTitle(
        nextLine, lines, index + consumed + 2, parsedLine.containers,
      );
      if (title.valid) {
        consumed += title.consumed + 1;
      }
    }
  }
  return { target: destination.target, consumed };
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

  return { target, trailing };
}

function referenceTitle(value) {
  return /^(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\((?:\\.|[^)\\])*\))$/.test(value);
}

function completeReferenceTitle(initial, lines, nextIndex, containers) {
  let value = initial.trim();
  let consumed = 0;
  while (true) {
    if (referenceTitle(value)) {
      return { valid: true, consumed };
    }
    if (!unterminatedReferenceTitle(value)) {
      return { valid: false, consumed: 0 };
    }
    const continuation = referenceContinuation(lines, nextIndex + consumed, containers);
    if (continuation === null || !continuation.trim()) {
      return { valid: false, consumed: 0 };
    }
    value += `\n${continuation.trimEnd()}`;
    consumed++;
  }
}

function unterminatedReferenceTitle(value) {
  const closing = value[0] === "(" ? ")" : value[0];
  if (closing !== ")" && closing !== "\"" && closing !== "'") {
    return false;
  }
  for (let index = 1; index < value.length; index++) {
    if (value[index] === "\\") {
      index++;
    } else if (value[index] === closing) {
      return false;
    }
  }
  return true;
}

function referenceContinuation(lines, index, containers) {
  if (index >= lines.length) {
    return null;
  }
  const continuation = continueBlockContainers(lines[index], containers);
  return continuation?.match(/^[ \t]{0,3}(\S.*)$/)?.[1] || null;
}

function parseBlockContainers(line) {
  let remaining = line.replace(/\r$/, "");
  const containers = [];
  while (true) {
    const quote = remaining.match(/^[ \t]{0,3}>[ \t]?/);
    if (quote) {
      remaining = remaining.slice(quote[0].length);
      containers.push({ type: "quote" });
      continue;
    }
    const list = remaining.match(
      /^[ \t]{0,3}(?:[-+*]|\d{1,9}[.)])(?: {1,4}(?![ \t])|\t)/,
    );
    if (list) {
      remaining = remaining.slice(list[0].length);
      containers.push({ type: "list", indent: indentationWidth(list[0]) });
      continue;
    }
    return { content: remaining, containers };
  }
}

function continueBlockContainers(line, containers) {
  let remaining = line.replace(/\r$/, "");
  for (const container of containers) {
    if (container.type === "quote") {
      const quote = remaining.match(/^[ \t]{0,3}>[ \t]?/);
      if (!quote) {
        return null;
      }
      remaining = remaining.slice(quote[0].length);
      continue;
    }
    remaining = stripIndent(remaining, container.indent);
    if (remaining === null) {
      return null;
    }
  }
  return remaining;
}

function blankWithinBlockContainers(line, containers) {
  let remaining = line.replace(/\r$/, "");
  for (const container of containers) {
    if (container.type === "quote") {
      const quote = remaining.match(/^[ \t]{0,3}>[ \t]?/);
      if (!quote) {
        return false;
      }
      remaining = remaining.slice(quote[0].length);
      continue;
    }
    if (!remaining.trim()) {
      return true;
    }
    remaining = stripIndent(remaining, container.indent);
    if (remaining === null) {
      return false;
    }
  }
  return !remaining.trim();
}

function indentationWidth(value) {
  let column = 0;
  for (const character of value) {
    column = character === "\t" ? column + 4 - (column % 4) : column + 1;
  }
  return column;
}

function stripIndent(line, width) {
  let column = 0;
  let index = 0;
  while (index < line.length && column < width) {
    if (line[index] === " ") {
      column++;
    } else if (line[index] === "\t") {
      column += 4 - (column % 4);
    } else {
      return null;
    }
    index++;
  }
  return column < width ? null : `${" ".repeat(column - width)}${line.slice(index)}`;
}

function isExternal(target) {
  return target.startsWith("//") || /^[a-z][a-z0-9+.-]*:/i.test(target);
}

function withoutFencedCode(contents) {
  let fence = null;
  return contents.split("\n").map((line) => {
    if (fence) {
      const content = continueBlockContainers(line, fence.containers);
      if (content !== null) {
        const closing = content.match(/^[ \t]{0,3}(`+|~+)[ \t]*$/)?.[1] || "";
        if (closing[0] === fence.character && closing.length >= fence.length) {
          fence = null;
        }
        return "";
      }
      if (blankWithinBlockContainers(line, fence.containers)) {
        return "";
      }
      fence = null;
    }

    const parsed = parseBlockContainers(line);
    const opening = parsed.content.match(/^[ \t]{0,3}(`{3,}|~{3,})(.*)$/);
    if (opening && !(opening[1][0] === "`" && opening[2].includes("`"))) {
      fence = {
        character: opening[1][0],
        length: opening[1].length,
        containers: parsed.containers,
      };
      return "";
    }
    return line;
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
