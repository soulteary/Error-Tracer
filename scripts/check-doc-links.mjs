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
  const paragraphCache = new Map();
  for (let index = 0; index < lines.length; index++) {
    const definition = referenceDefinitionAt(lines, index, true, paragraphCache);
    if (definition) {
      targets.push(definition.target);
      index += definition.consumed;
    }
  }
  return targets;
}

function referenceDefinitionAt(
  lines,
  index,
  checkParagraphInterruption = true,
  paragraphCache = null,
) {
  const parsedLine = parseBlockContainers(lines[index]);
  const match = parsedLine.content.match(
    /^[ \t]{0,3}\[(?!\^)((?:\\.|[^\]\\\n])+)\]:[ \t]*(.*)$/,
  );
  if (!match) {
    return null;
  }
  if (checkParagraphInterruption &&
      (orderedListInterruptsParagraph(
        lines, index, parsedLine.containers, paragraphCache,
      ) || paragraphOpenBefore(
        lines, index, parsedLine.containers, paragraphCache,
      ))) {
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

function parseBlockContainers(line, orderedListCanInterrupt = null) {
  let remaining = line.replace(/\r$/, "");
  const containers = [];
  while (true) {
    const quote = remaining.match(/^ {0,3}>[ \t]?/);
    if (quote) {
      remaining = remaining.slice(quote[0].length);
      containers.push({ type: "quote" });
      continue;
    }
    const list = listMarkerPrefix(remaining);
    if (list) {
      if ((list.empty ||
           (list.orderedStart !== null && list.orderedStart !== 1)) &&
          orderedListCanInterrupt && !orderedListCanInterrupt(containers)) {
        return { content: remaining, containers };
      }
      remaining = remaining.slice(list.length);
      containers.push({
        type: "list",
        indent: list.indent,
        markerIndent: list.markerIndent,
        orderedStart: list.orderedStart,
      });
      continue;
    }
    return { content: remaining, containers };
  }
}

function parseContinuedBlockContainers(
  line,
  activeContainers,
  activeParagraphOpen,
  orderedListCanInterrupt,
) {
  const continued = blockContainerPrefix(line, activeContainers);
  let containers = activeContainers.slice(0, continued.count);
  let content = continued.content;
  if (continued.count < activeContainers.length && activeParagraphOpen &&
      paragraphOpenAfter(content, true)) {
    containers = activeContainers;
  } else if (continued.count < activeContainers.length && !content.trim()) {
    for (let count = activeContainers.length; count > continued.count; count--) {
      if (blankWithinBlockContainers(line, activeContainers.slice(0, count))) {
        containers = activeContainers.slice(0, count);
        content = "";
        break;
      }
    }
  }

  const nested = parseBlockContainers(content, (nestedContainers) =>
    orderedListCanInterrupt([...containers, ...nestedContainers]));
  return {
    content: nested.content,
    containers: [...containers, ...nested.containers],
  };
}

function listMarkerPrefix(value) {
  const marker = value.match(/^( {0,3})(?:[-+*]|(\d{1,9})[.)])/);
  if (!marker) {
    return null;
  }

  let index = marker[0].length;
  const markerColumn = indentationWidth(value.slice(0, index));
  const empty = !value.slice(index).trim();
  if (index === value.length) {
    return {
      length: index,
      indent: markerColumn + 1,
      markerIndent: marker[1].length,
      orderedStart: marker[2] === undefined ? null : Number(marker[2]),
      empty: true,
    };
  }
  if (value[index] !== " " && value[index] !== "\t") {
    return null;
  }
  const firstPaddingEnd = index + 1;
  const firstPaddingColumn = value[index] === "\t"
    ? markerColumn + 4 - (markerColumn % 4)
    : markerColumn + 1;
  let column = markerColumn;
  while (value[index] === " " || value[index] === "\t") {
    column = value[index] === "\t" ? column + 4 - (column % 4) : column + 1;
    if (column - markerColumn > 4) {
      return {
        length: firstPaddingEnd,
        indent: firstPaddingColumn,
        markerIndent: marker[1].length,
        orderedStart: marker[2] === undefined ? null : Number(marker[2]),
        empty,
      };
    }
    index++;
  }
  return {
    length: index,
    indent: column,
    markerIndent: marker[1].length,
    orderedStart: marker[2] === undefined ? null : Number(marker[2]),
    empty,
  };
}

function orderedListInterruptsParagraph(lines, index, containers, paragraphCache) {
  const listIndex = containers.findIndex((container) =>
    container.type === "list" &&
    container.orderedStart !== null &&
    container.orderedStart !== 1);
  if (listIndex < 0) {
    return false;
  }
  return paragraphOpenBefore(
    lines, index, containers.slice(0, listIndex), paragraphCache,
  );
}

function paragraphOpenBefore(lines, index, containers, paragraphCache) {
  const cache = paragraphCache || new Map();
  const key = containers.map((container) => container.type === "quote"
    ? "quote"
    : `list:${container.indent}:${container.orderedStart ?? "bullet"}`).join("/");
  let state = cache.get(key);
  if (!state || state.index > index) {
    state = { index: 0, paragraphOpen: false };
    cache.set(key, state);
  }
  while (state.index < index) {
    const previous = state.index;
    state.index++;
    if (startsNewListItem(lines[previous], containers)) {
      state.paragraphOpen = false;
    }
    const content = continueBlockContainers(lines[previous], containers);
    if (content === null) {
      state.paragraphOpen = state.paragraphOpen &&
        lazyParagraphContinuation(lines[previous], containers);
      continue;
    }
    const nextParagraphOpen = paragraphOpenAfter(content, state.paragraphOpen);
    if (!state.paragraphOpen || !nextParagraphOpen) {
      const definition = referenceDefinitionAt(lines, previous, false);
      if (definition) {
        state.paragraphOpen = false;
        state.index += definition.consumed;
        continue;
      }
    }
    state.paragraphOpen = nextParagraphOpen;
  }
  return !startsNewListItem(lines[index], containers) && state.paragraphOpen;
}

function paragraphOpenAfter(line, paragraphOpen) {
  const value = line.replace(/\r$/, "");
  if (!value.trim()) {
    return false;
  }
  const list = listMarkerPrefix(value);
  if (list) {
    if (list.empty && value.trim() !== "-") {
      return paragraphOpen;
    }
    return paragraphOpen && list.orderedStart !== null && list.orderedStart !== 1;
  }
  if (/^(?: {4}|\t)/.test(value)) {
    return paragraphOpen;
  }
  if (/^ {0,3}(?:#{1,6}(?:[ \t]+|$)|>|`{3,}|~{3,})/.test(value) ||
      /^ {0,3}(?:(?:\*[ \t]*){3,}|(?:_[ \t]*){3,}|(?:-[ \t]*)+|=+[ \t]*)$/.test(value)) {
    return false;
  }
  if (/^ {0,3}\[(?!\^)(?:\\.|[^\]\\\n])+\]:/.test(value)) {
    return paragraphOpen;
  }
  return true;
}

function continueBlockContainers(line, containers) {
  const continued = blockContainerPrefix(line, containers);
  return continued.count === containers.length ? continued.content : null;
}

function blockContainerPrefix(line, containers) {
  let remaining = line.replace(/\r$/, "");
  let count = 0;
  for (const container of containers) {
    if (container.type === "quote") {
      const quote = remaining.match(/^ {0,3}>[ \t]?/);
      if (!quote) {
        break;
      }
      remaining = remaining.slice(quote[0].length);
    } else {
      const stripped = stripIndent(remaining, container.indent);
      if (stripped === null) {
        break;
      }
      remaining = stripped;
    }
    count++;
  }
  return { content: remaining, count };
}

function startsNewListItem(line, containers) {
  if (line === undefined) {
    return false;
  }
  let remaining = line.replace(/\r$/, "");
  for (const container of containers) {
    if (container.type === "quote") {
      const quote = remaining.match(/^ {0,3}>[ \t]?/);
      if (!quote) {
        return false;
      }
      remaining = remaining.slice(quote[0].length);
      continue;
    }
    const marker = listMarkerPrefix(remaining);
    if (marker?.markerIndent === container.markerIndent) {
      return true;
    }
    const stripped = stripIndent(remaining, container.indent);
    if (stripped === null) {
      return false;
    }
    remaining = stripped;
  }
  return false;
}

function lazyParagraphContinuation(line, containers) {
  const continued = blockContainerPrefix(line, containers);
  return continued.count < containers.length &&
    paragraphOpenAfter(continued.content, true);
}

function blankWithinBlockContainers(line, containers) {
  let remaining = line.replace(/\r$/, "");
  for (const container of containers) {
    if (container.type === "quote") {
      const quote = remaining.match(/^ {0,3}>[ \t]?/);
      if (!quote) {
        return false;
      }
      remaining = remaining.slice(quote[0].length);
      continue;
    }
    if (!remaining.trim()) {
      // A blank line may omit list indentation, but any inner quote
      // containers must still be checked below.
      continue;
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
  let htmlBlock = null;
  let activeContainers = [];
  const output = [];
  const paragraphCache = new Map();
  for (const line of contents.split("\n")) {
    if (fence) {
      const content = continueBlockContainers(line, fence.containers);
      if (content !== null) {
        const closing = content.match(/^[ \t]{0,3}(`+|~+)[ \t]*$/)?.[1] || "";
        if (closing[0] === fence.character && closing.length >= fence.length) {
          fence = null;
        }
        output.push("");
        continue;
      }
      if (blankWithinBlockContainers(line, fence.containers)) {
        output.push("");
        continue;
      }
      fence = null;
    }

    if (htmlBlock) {
      const content = continueBlockContainers(line, htmlBlock.containers);
      if (content !== null) {
        if (htmlBlock.terminator.test(content)) {
          htmlBlock = null;
        }
        output.push("");
        continue;
      }
      if (blankWithinBlockContainers(line, htmlBlock.containers)) {
        output.push("");
        continue;
      }
      htmlBlock = null;
    }

    const orderedListCanInterrupt = (containers) =>
      !paragraphOpenBefore(
        output, output.length, containers, paragraphCache,
      );
    const parsed = activeContainers.length
      ? parseContinuedBlockContainers(
        line,
        activeContainers,
        paragraphOpenBefore(
          output, output.length, activeContainers, paragraphCache,
        ),
        orderedListCanInterrupt,
      )
      : parseBlockContainers(line, orderedListCanInterrupt);
    activeContainers = parsed.containers;
    if (stripIndent(parsed.content, 4) !== null &&
        (startsNewListItem(line, parsed.containers) ||
         !paragraphOpenBefore(
           output, output.length, parsed.containers, paragraphCache,
         ))) {
      output.push("");
      continue;
    }
    const openingHTML = htmlBlockAt(parsed.content);
    if (openingHTML) {
      if (!openingHTML.terminator.test(parsed.content)) {
        htmlBlock = {
          terminator: openingHTML.terminator,
          containers: parsed.containers,
        };
      }
      output.push("");
      continue;
    }
    const opening = parsed.content.match(/^[ \t]{0,3}(`{3,}|~{3,})(.*)$/);
    if (opening && !(opening[1][0] === "`" && opening[2].includes("`"))) {
      fence = {
        character: opening[1][0],
        length: opening[1].length,
        containers: parsed.containers,
      };
      output.push("");
      continue;
    }
    output.push(line);
  }
  return output.join("\n");
}

function htmlBlockAt(content) {
  const typeOne = content.match(/^ {0,3}<(script|pre|style|textarea)(?:[ \t]|>|$)/i);
  if (typeOne) {
    return { terminator: new RegExp(`</${typeOne[1]}[ \\t]*>`, "i") };
  }
  if (/^ {0,3}<!--/.test(content)) {
    return { terminator: /-->/ };
  }
  if (/^ {0,3}<\?/.test(content)) {
    return { terminator: /\?>/ };
  }
  if (/^ {0,3}<!\[CDATA\[/.test(content)) {
    return { terminator: /\]\]>/ };
  }
  if (/^ {0,3}<![A-Z]/.test(content)) {
    return { terminator: />/ };
  }
  return null;
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
