import { useMemo } from 'react';
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

export interface SourceDrawerState {
  path: string;
  status: 'loading' | 'loaded' | 'error';
  content?: string;
  error?: string;
}

export interface SourceDrawerProps {
  source: SourceDrawerState | null;
  onClose: () => void;
}

export function SourceDrawer({ source, onClose }: SourceDrawerProps) {
  const lines = useMemo(() => {
    if (source?.content == null) return [];
    return highlightedLines(source.path, source.content);
  }, [source?.content, source?.path]);

  if (!source) return null;

  return (
    <aside className="hf-source-drawer" aria-label="Source file viewer">
      <div className="hf-source-head">
        <div className="hf-source-title" title={source.path}>{source.path}</div>
        <button className="hf-source-close" type="button" onClick={onClose} aria-label="Close source viewer">
          x
        </button>
      </div>
      <div className="hf-source-body">
        {source.status === 'loading' && <div className="hf-source-state">Loading...</div>}
        {source.status === 'error' && <div className="hf-source-state error">{source.error}</div>}
        {source.status === 'loaded' && (
          <table className="hf-source-table">
            <tbody>
              {(lines.length > 0 ? lines : ['']).map((line, idx) => (
                <tr key={idx}>
                  <td className="hf-source-no">{idx + 1}</td>
                  <td className="hf-source-code">
                    <code dangerouslySetInnerHTML={{ __html: line || ' ' }} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </aside>
  );
}

function highlightedLines(path: string, content: string): string[] {
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

function languageForPath(path: string): string | undefined {
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
