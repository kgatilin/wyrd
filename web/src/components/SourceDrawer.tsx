import { useEffect, useMemo, useRef, useState } from 'react';
import { basicSetup, EditorView } from 'codemirror';
import { go as goLanguage } from '@codemirror/lang-go';
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
  hash?: string;
  error?: string;
}

export interface SaveSourceResult {
  path: string;
  content: string;
  hash: string;
}

export interface SourceDrawerProps {
  source: SourceDrawerState | null;
  onClose: () => void;
  onSave: (path: string, content: string, baseHash: string) => Promise<SaveSourceResult>;
}

export function SourceDrawer({ source, onClose, onSave }: SourceDrawerProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState('');
  const [baseHash, setBaseHash] = useState('');
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const lines = useMemo(() => {
    if (source?.content == null) return [];
    return highlightedLines(source.path, source.content);
  }, [source?.content, source?.path]);

  useEffect(() => {
    if (source?.status !== 'loaded') return;
    setDraft(source.content ?? '');
    setBaseHash(source.hash ?? '');
    setEditing(false);
    setSaving(false);
    setSaveError(null);
  }, [source?.path, source?.status, source?.content, source?.hash]);

  if (!source) return null;
  const canEdit = source.status === 'loaded' && source.content != null;
  const dirty = canEdit && draft !== (source.content ?? '');

  const saveDraft = async () => {
    if (!canEdit || !baseHash || saving) return;
    setSaving(true);
    setSaveError(null);
    try {
      const saved = await onSave(source.path, draft, baseHash);
      setDraft(saved.content);
      setBaseHash(saved.hash);
      setEditing(false);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <aside className="hf-source-drawer" aria-label="Source file viewer">
      <div className="hf-source-head">
        <div className="hf-source-title" title={source.path}>{source.path}</div>
        {canEdit && (
          <div className="hf-source-actions">
            {editing ? (
              <>
                <button
                  className="hf-source-action primary"
                  type="button"
                  onClick={saveDraft}
                  disabled={!dirty || !baseHash || saving}
                >
                  {saving ? 'Saving...' : 'Save'}
                </button>
                <button
                  className="hf-source-action"
                  type="button"
                  onClick={() => {
                    setDraft(source.content ?? '');
                    setSaveError(null);
                    setEditing(false);
                  }}
                  disabled={saving}
                >
                  Cancel
                </button>
              </>
            ) : (
              <button className="hf-source-action" type="button" onClick={() => setEditing(true)}>
                Edit
              </button>
            )}
          </div>
        )}
        <button className="hf-source-close" type="button" onClick={onClose} aria-label="Close source viewer">
          x
        </button>
      </div>
      <div className="hf-source-body">
        {source.status === 'loading' && <div className="hf-source-state">Loading...</div>}
        {source.status === 'error' && <div className="hf-source-state error">{source.error}</div>}
        {source.status === 'loaded' && editing && (
          <div className="hf-source-editor-wrap">
            {saveError && <div className="hf-source-save-error">{saveError}</div>}
            <CodeEditor path={source.path} value={draft} onChange={setDraft} />
          </div>
        )}
        {source.status === 'loaded' && !editing && (
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

function CodeEditor({ path, value, onChange }: { path: string; value: string; onChange: (value: string) => void }) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);

  useEffect(() => {
    if (!hostRef.current) return;
    const language = path.toLowerCase().endsWith('.go') ? goLanguage() : [];
    const view = new EditorView({
      parent: hostRef.current,
      doc: value,
      extensions: [
        basicSetup,
        language,
        EditorView.lineWrapping,
        EditorView.theme(
          {
            '&': {
              height: '100%',
              color: 'var(--fg-1)',
              backgroundColor: 'var(--bg-0)',
              fontFamily: "'JetBrains Mono', ui-monospace, monospace",
              fontSize: '12px',
            },
            '.cm-scroller': { overflow: 'auto' },
            '.cm-content': { caretColor: 'var(--fg-0)' },
            '.cm-cursor': { borderLeftColor: 'var(--fg-0)' },
            '.cm-gutters': {
              backgroundColor: 'var(--bg-1)',
              color: 'var(--fg-3)',
              borderRight: '1px solid var(--line-1)',
            },
            '.cm-activeLine': { backgroundColor: 'rgba(255,255,255,0.035)' },
            '.cm-activeLineGutter': { backgroundColor: 'rgba(255,255,255,0.05)' },
            '.cm-selectionBackground, ::selection': { backgroundColor: 'rgba(96, 165, 250, 0.28) !important' },
          },
          { dark: true }
        ),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) onChange(update.state.doc.toString());
        }),
      ],
    });
    viewRef.current = view;
    return () => {
      view.destroy();
      viewRef.current = null;
    };
  }, [path]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    const current = view.state.doc.toString();
    if (current === value) return;
    view.dispatch({ changes: { from: 0, to: current.length, insert: value } });
  }, [value]);

  return <div className="hf-source-cm" ref={hostRef} />;
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
