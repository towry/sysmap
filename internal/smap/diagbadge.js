"use strict";
function diagBadge(n) {
  const count = (n.diagrams || []).length;
  return count ? `<span class="diagbadge" title="${count} 个 Mermaid 图">图 ${count}</span>` : "";
}
