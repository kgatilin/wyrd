/**
 * Shared syntax highlighting for the code surfaces (source drawer, file
 * diff). highlight.js is registered here once with the languages this UI
 * actually meets, so a second viewer does not pull in a second highlighter.
 */
import hljs from 'highlight.js/lib/core';
import bash from 'highlight.js/lib/languages/bash';
import go from 'highlight.js/lib/languages/go';
import javascript from 'highlight.js/lib/languages/javascript';
import json from 'highlight.js/lib/languages/json';
import markdown from 'highlight.js/lib/languages/markdown';
import typescript from 'highlight.js/lib/languages/typescript';
import xml from 'highlight.js/lib/languages/xml';
import yaml from 'highlight.js/lib/languages/yaml';

hljs.registerLanguage('bash', bash);
hljs.registerLanguage('go', go);
hljs.registerLanguage('javascript', javascript);
hljs.registerLanguage('json', json);
hljs.registerLanguage('markdown', markdown);
hljs.registerLanguage('typescript', typescript);
hljs.registerLanguage('xml', xml);
hljs.registerLanguage('yaml', yaml);

export function languageForPath(path: string): string | undefined {
  const lower = path.toLowerCase();
  if (lower.endsWith('.go')) return 'go';
  if (lower.endsWith('.ts') || lower.endsWith('.tsx')) return 'typescript';
  if (lower.endsWith('.js') || lower.endsWith('.jsx') || lower.endsWith('.mjs') || lower.endsWith('.cjs')) {
    return 'javascript';
  }
  if (lower.endsWith('.json')) return 'json';
  if (lower.endsWith('.yaml') || lower.endsWith('.yml')) return 'yaml';
  if (lower.endsWith('.sh') || lower.endsWith('.bash') || lower.endsWith('.zsh')) return 'bash';
  if (lower.endsWith('.html') || lower.endsWith('.xml') || lower.endsWith('.svg')) return 'xml';
  if (lower.endsWith('.md') || lower.endsWith('.markdown')) return 'markdown';
  return undefined;
}

/**
 * Highlights one line as HTML. Unlike the whole-file path this never falls
 * back to language auto-detection: a diff renders thousands of independent
 * lines, and per-line detection is both slow and visibly inconsistent.
 */
export function highlightLine(line: string, language: string | undefined): string {
  if (line === '') return '';
  if (!language) return escapeHTML(line);
  return hljs.highlight(line, { language, ignoreIllegals: true }).value;
}

/** Highlights a whole file, one HTML string per line. */
export function highlightedLines(path: string, content: string): string[] {
  const language = languageForPath(path);
  return content
    .replace(/\n$/, '')
    .split('\n')
    .map((line) => {
      if (line === '') return '';
      return language
        ? hljs.highlight(line, { language, ignoreIllegals: true }).value
        : hljs.highlightAuto(line).value;
    });
}

export function escapeHTML(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}
