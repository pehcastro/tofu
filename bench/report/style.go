package report

const shadcnTokens = `
:root{
  color-scheme: light;
  --background: oklch(1 0 0);
  --foreground: oklch(0.1450 0 0);
  --card: oklch(1 0 0);
  --card-foreground: oklch(0.1450 0 0);
  --primary: oklch(0.2050 0 0);
  --primary-foreground: oklch(0.9850 0 0);
  --secondary: oklch(0.9700 0 0);
  --secondary-foreground: oklch(0.2050 0 0);
  --muted: oklch(0.9700 0 0);
  --muted-foreground: oklch(0.5560 0 0);
  --accent: oklch(0.9700 0 0);
  --accent-foreground: oklch(0.2050 0 0);
  --destructive: oklch(0.5770 0.2450 27.3250);
  --destructive-foreground: oklch(1 0 0);
  --border: oklch(0.9220 0 0);
  --input: oklch(0.9220 0 0);
  --ring: oklch(0.7080 0 0);
  --chart-1: oklch(0.8100 0.1000 252);
  --chart-2: oklch(0.6200 0.1900 260);
  --chart-3: oklch(0.5500 0.2200 263);
  --font-sans: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
  --font-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  --radius: 0.625rem;
  --radius-sm: calc(var(--radius) - 4px);
  --radius-md: calc(var(--radius) - 2px);
  --radius-lg: var(--radius);
  --radius-xl: calc(var(--radius) + 4px);
}
.dark{
  color-scheme: dark;
  --background: oklch(0.1450 0 0);
  --foreground: oklch(0.9850 0 0);
  --card: oklch(0.2050 0 0);
  --card-foreground: oklch(0.9850 0 0);
  --primary: oklch(0.9220 0 0);
  --primary-foreground: oklch(0.2050 0 0);
  --secondary: oklch(0.2690 0 0);
  --secondary-foreground: oklch(0.9850 0 0);
  --muted: oklch(0.2690 0 0);
  --muted-foreground: oklch(0.7080 0 0);
  --accent: oklch(0.3710 0 0);
  --accent-foreground: oklch(0.9850 0 0);
  --destructive: oklch(0.7040 0.1910 22.2160);
  --destructive-foreground: oklch(0.9850 0 0);
  --border: oklch(0.2750 0 0);
  --input: oklch(0.3250 0 0);
  --ring: oklch(0.5560 0 0);
}
`

const shadcnComponents = `
@layer components{
  .card{background-color:var(--card);color:var(--card-foreground);border:1px solid var(--border);
    border-radius:var(--radius-xl);overflow:hidden;container-type:inline-size}
  .card-header{display:flex;flex-direction:column;gap:.25rem;padding:1.5rem;padding-bottom:0}
  .card-title{font-size:1.25rem;font-weight:600;line-height:1.25;letter-spacing:-.01em;margin:0}
  .card-description{font-size:.875rem;color:var(--muted-foreground);margin:0;line-height:1.6}
  .card-content{padding:1.5rem}

  .badge{display:inline-flex;align-items:center;border-radius:9999px;padding:.125rem .625rem;
    font-size:.6875rem;font-weight:500;letter-spacing:.01em;line-height:1.5rem}
  .badge[data-variant="outline"]{border:1px solid var(--border);color:var(--muted-foreground)}
  .badge[data-variant="secondary"]{background:var(--secondary);color:var(--secondary-foreground)}
  .badge[data-variant="default"]{background:var(--primary);color:var(--primary-foreground)}

  .table-container{width:100%;overflow-x:auto;border:1px solid var(--border);
    border-radius:var(--radius-xl);container-type:inline-size}
  .table{width:100%;border-collapse:collapse;font-size:.875rem;text-align:left;caption-side:bottom}
  .table-caption{padding:.75rem 1rem;font-size:.8125rem;color:var(--muted-foreground);text-align:left}
  .table-head{padding:.75rem 1rem;font-weight:500;color:var(--muted-foreground);
    background-color:var(--muted);white-space:nowrap;border-bottom:1px solid var(--border)}
  .table-row{border-bottom:1px solid var(--border)}
  .table-row:last-child{border-bottom:none}
  tbody .table-row:hover{background-color:var(--muted)}
  .table-cell{padding:.75rem 1rem;vertical-align:top;color:var(--foreground)}

  .tab-list{display:inline-flex;align-items:center;background:var(--muted);
    border-radius:var(--radius-lg);padding:.25rem;gap:.125rem;flex-wrap:wrap}
  .tab-trigger{display:inline-flex;align-items:center;justify-content:center;gap:.5rem;
    padding:.375rem .75rem;border-radius:calc(var(--radius) * .6);font-size:.875rem;font-weight:500;
    border:none;background:transparent;color:var(--muted-foreground);cursor:pointer;
    transition:all 150ms ease;white-space:nowrap;outline:none}
  .tab-trigger[aria-selected="true"]{background:var(--background);color:var(--foreground);
    box-shadow:0 1px 3px oklch(0 0 0 / .08), 0 0 0 1px var(--border)}
  .tab-trigger:focus-visible{outline:2px solid var(--ring);outline-offset:2px}
  .tab-content{margin-top:1.25rem}
  .tab-content:focus-visible{outline:2px solid var(--ring);outline-offset:2px}
  .tab-content[hidden]{display:none}
  @media (prefers-reduced-motion: reduce){.tab-trigger{transition:none}}
}
`

const pageLayout = `
*{box-sizing:border-box}
html{scrollbar-gutter:stable}
body{margin:0;background:var(--background);color:var(--foreground);font-family:var(--font-sans);
  font-size:15px;line-height:1.6}
main{max-width:1180px;margin:0 auto;padding:40px 28px 120px}
h1{font-size:28px;letter-spacing:-.02em;margin:0 0 6px}
h2{font-size:20px;letter-spacing:-.01em;margin:0 0 10px}
h3{font-size:15px;margin:26px 0 8px}
p{margin:0 0 14px;max-width:88ch}
.lede{color:var(--muted-foreground);margin-bottom:28px}
.fig{font-variant-numeric:tabular-nums;font-weight:650;white-space:nowrap}
.sample{white-space:normal;font-weight:500;min-width:16ch;display:inline-block}
.detail{display:block;color:var(--muted-foreground);font-size:.8125rem;font-weight:400;
  white-space:normal;margin-top:3px;max-width:42ch}
.card-header{padding-bottom:0}
.card-content{padding-top:.5rem}
.card-content .card-title{font-size:1.5rem;font-variant-numeric:tabular-nums;margin:0 0 4px}
.win .fig{color:var(--chart-2)}
.loss .fig{color:var(--destructive)}
.missing{color:var(--muted-foreground)}
.cards{display:grid;gap:14px;margin-bottom:22px;
  grid-template-columns:repeat(var(--cols,3),minmax(0,1fr))}
@media (max-width:1000px){.cards{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media (max-width:620px){.cards{grid-template-columns:minmax(0,1fr)}}
.card-content ul{margin:0;padding-left:18px}
.card-content li{margin-bottom:6px}
blockquote{margin:0 0 14px;padding:10px 16px;border-left:3px solid var(--border);
  background:var(--muted);border-radius:0 var(--radius-md) var(--radius-md) 0}
blockquote cite{display:block;font-style:normal;font-size:.75rem;margin-top:6px;
  color:var(--muted-foreground);font-family:var(--font-mono)}
details{border:1px solid var(--border);border-radius:var(--radius-lg);padding:12px 16px;margin-bottom:10px}
details[open]{background:var(--card)}
summary{cursor:pointer;font-weight:550}
summary .badge{margin-left:8px}
code,.path{font-family:var(--font-mono);font-size:.8125rem;color:var(--muted-foreground)}
svg.gapchart{width:100%;height:auto;margin:6px 0 4px}
svg.gapchart text{fill:var(--muted-foreground);font-size:11px;font-family:var(--font-sans)}
svg.gapchart text.point{fill:var(--foreground);text-anchor:end}
svg.gapchart text.reading{fill:var(--foreground);text-anchor:start;font-variant-numeric:tabular-nums}
svg.gapchart rect.free{fill:var(--muted-foreground)}
svg.gapchart rect.judged{fill:var(--chart-2)}
footer{margin-top:60px;color:var(--muted-foreground);font-size:.8125rem}
`

const tabScript = `
for (const list of document.querySelectorAll('[role="tablist"]')) {
  const triggers = Array.from(list.querySelectorAll('[role="tab"]'));
  const show = tab => {
    for (const other of triggers) {
      other.setAttribute('aria-selected', String(other === tab));
      if (other === tab) { other.removeAttribute('tabindex'); } else { other.setAttribute('tabindex', '-1'); }
      document.getElementById(other.getAttribute('aria-controls')).hidden = other !== tab;
    }
  };
  triggers.forEach((trigger, at) => {
    trigger.addEventListener('click', () => show(trigger));
    trigger.addEventListener('keydown', event => {
      const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
      if (!step) return;
      event.preventDefault();
      const next = triggers[(at + step + triggers.length) % triggers.length];
      show(next);
      next.focus();
    });
  });
}
`
