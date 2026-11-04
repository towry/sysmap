"use strict";
function escBadge(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"}[c]));
}
function taskBadge(n) {
  const p = n.progress;
  if (!p || !p.total) return "";
  const done = Math.round(100 * p.done / p.total);
  const doing = Math.round(100 * p.doing / p.total);
  const title = `${p.done}/${p.total} 完成` + (p.doing ? `，${p.doing} 进行中` : "");
  return `<span class="taskbadge" title="${title}"><span class="tbar"><i class="done" style="width:${done}%"></i><i class="doing" style="left:${done}%;width:${doing}%"></i></span><span>${p.done}/${p.total}</span></span>`;
}
function taskDetail(n) {
  const p = n.progress, tasks = n.tasks || [];
  if (!p || !p.total) return "";
  const todo = p.total - p.done - p.doing;
  const rows = tasks.map((t) => {
    const word = t.state === "done" ? "完成" : t.state === "doing" ? "进行中" : "待办";
    return `<li class="t-${escBadge(t.state)}"><span class="tstate">${word}</span><span>${escBadge(t.text)}</span></li>`;
  }).join("");
  return `<div class="taskprogress">${taskBadge(n)}<span class="lab">${p.done} 完成 · ${p.doing} 进行中 · ${todo} 待办</span></div>` +
    (rows ? `<h3>任务 <span>${tasks.length}</span></h3><ul class="tasklist">${rows}</ul>` : "");
}
