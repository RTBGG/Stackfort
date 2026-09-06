// SPDX-License-Identifier: AGPL-3.0-or-later

// Offline checks for the repository's inline/reference/HTML links and ATX
// headings. This is not a general Markdown renderer or a remote URL checker.
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

export function prose(source) {
  let fence = null;
  return source.replace(/<!--[\s\S]*?-->/g, (comment) => comment.replace(/[^\n]/g, ' '))
    .split(/\r?\n/).map((line) => {
      const marker = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
      if (fence) {
        if (marker && marker[1][0] === fence[0] && marker[1].length >= fence.length && !marker[2].trim()) {
          fence = null;
        }
        return '';
      }
      if (marker) {
        fence = marker[1];
        return '';
      }
      return line;
    }).join('\n');
}

export function anchors(source) {
  const result = new Set();
  const text = prose(source);
  for (const match of text.matchAll(/^ {0,3}#{1,6}[ \t]+(.+?)\s*#*\s*$/gm)) {
    const slug = match[1].replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
      .replace(/<[^>]*>/g, '').toLowerCase()
      .replace(/[^\p{L}\p{M}\p{N}_\- ]/gu, '').replace(/ /g, '-');
    let unique = slug;
    for (let suffix = 1; result.has(unique); suffix += 1) unique = `${slug}-${suffix}`;
    result.add(unique);
  }
  for (const match of text.matchAll(/\b(?:id|name)=["']([^"']+)["']/g)) result.add(match[1]);
  return result;
}

export function links(source) {
  // Ignore inline code examples as well as fenced code and HTML comments.
  const text = prose(source).replace(/(`+)[\s\S]*?\1/g, (code) => code.replace(/[^\n]/g, ' '));
  const patterns = [
    /\]\(\s*(?:<([^>\n]+)>|([^\s)]+))(?:\s+["'][^\n]*?["'])?\s*\)/g,
    /^ {0,3}\[[^\]\n]+\]:\s*(?:<([^>\n]+)>|(\S+))/gm,
    /\b(?:href|src)=["']([^"']+)["']/g,
  ];
  return patterns.flatMap((pattern) => [...text.matchAll(pattern)].map((match) => ({
    target: match[1] ?? match[2],
    line: text.slice(0, match.index).split('\n').length,
  })));
}

export function checkDocuments(documents, files) {
  const errors = [];
  const available = new Set(files);
  const headingCache = new Map();
  let checked = 0;
  for (const [file, source] of documents) {
    for (const { target, line } of links(source)) {
      if (/^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(target)) continue;
      checked += 1;
      const report = (message) => errors.push(`${file}:${line}: ${message}: ${target}`);
      let pathname, fragment;
      try {
        const hash = target.indexOf('#');
        pathname = decodeURIComponent((hash < 0 ? target : target.slice(0, hash)).split('?')[0]);
        fragment = hash < 0 ? '' : decodeURIComponent(target.slice(hash + 1));
      } catch {
        report('malformed URL encoding');
        continue;
      }
      if (pathname.startsWith('/') || pathname.includes('\\')) {
        report('use a repository-relative forward-slash path');
        continue;
      }
      const resolved = pathname ? path.posix.normalize(path.posix.join(path.posix.dirname(file), pathname)) : file;
      if (resolved === '..' || resolved.startsWith('../')) {
        report('link escapes repository');
        continue;
      }
      const directory = resolved.replace(/\/$/, '');
      if (!available.has(resolved) && directory !== '.' && !files.some((entry) => entry.startsWith(`${directory}/`))) {
        report('missing target or incorrect path casing');
        continue;
      }
      if (fragment && documents.has(resolved)) {
        if (!headingCache.has(resolved)) headingCache.set(resolved, anchors(documents.get(resolved)));
        if (!headingCache.get(resolved).has(fragment)) report('missing Markdown heading/anchor');
      }
    }
  }
  return { errors, checked };
}

function main() {
  const root = fileURLToPath(new URL('../', import.meta.url));
  const files = [...new Set(execFileSync('git', ['ls-files', '--cached', '--others', '--exclude-standard', '-z'], {
    cwd: root, encoding: 'utf8', maxBuffer: 16 * 1024 * 1024,
  }).split('\0').filter(Boolean))];
  const documents = new Map(files.filter((file) => file.endsWith('.md')).map((file) => [
    file, readFileSync(path.join(root, file), 'utf8'),
  ]));
  const { errors, checked } = checkDocuments(documents, files);
  if (errors.length) {
    console.error(errors.join('\n'));
    process.exitCode = 1;
    return;
  }
  console.log(`Documentation checks passed: ${documents.size} Markdown files, ${checked} local links (paths and Markdown anchors). External URLs and commands are not executed.`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) main();
