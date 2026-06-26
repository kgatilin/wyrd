import { Fragment, type ReactNode } from 'react';

/**
 * Lightweight markdown prose block. Supports headings (##, ###), paragraphs,
 * unordered lists, and inline `code` / **bold**. Intentionally minimal and
 * HTML-safe (no raw HTML) — the agent emits markdown, this renders it.
 */
export function ProseBlock({ markdown }: { markdown: string }) {
  return <div className="prose-block">{renderMarkdown(markdown)}</div>;
}

function renderMarkdown(src: string): ReactNode {
  const lines = src.replace(/\r\n/g, '\n').split('\n');
  const out: ReactNode[] = [];
  let para: string[] = [];
  let list: string[] = [];
  let key = 0;

  const flushPara = () => {
    if (para.length) {
      out.push(<p key={key++}>{renderInline(para.join(' '))}</p>);
      para = [];
    }
  };
  const flushList = () => {
    if (list.length) {
      out.push(
        <ul key={key++}>
          {list.map((item, i) => (
            <li key={i}>{renderInline(item)}</li>
          ))}
        </ul>,
      );
      list = [];
    }
  };

  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed === '') {
      flushPara();
      flushList();
    } else if (trimmed.startsWith('### ')) {
      flushPara();
      flushList();
      out.push(<h3 key={key++}>{renderInline(trimmed.slice(4))}</h3>);
    } else if (trimmed.startsWith('## ')) {
      flushPara();
      flushList();
      out.push(<h2 key={key++}>{renderInline(trimmed.slice(3))}</h2>);
    } else if (trimmed.startsWith('- ')) {
      flushPara();
      list.push(trimmed.slice(2));
    } else {
      flushList();
      para.push(trimmed);
    }
  }
  flushPara();
  flushList();
  return out;
}

/** Renders inline `code` and **bold** within a text run. */
function renderInline(text: string): ReactNode {
  // Split on `code` spans and **bold** spans, keeping delimiters.
  const tokens = text.split(/(`[^`]+`|\*\*[^*]+\*\*)/g).filter(Boolean);
  return tokens.map((tok, i) => {
    if (tok.startsWith('`') && tok.endsWith('`')) {
      return <code key={i}>{tok.slice(1, -1)}</code>;
    }
    if (tok.startsWith('**') && tok.endsWith('**')) {
      return <strong key={i}>{tok.slice(2, -2)}</strong>;
    }
    return <Fragment key={i}>{tok}</Fragment>;
  });
}
