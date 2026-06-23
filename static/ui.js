(function() {
  "use strict";

  var CAT_LABELS = {
    "ai": "AI",
    "humanoid-robotics": "로봇",
    "bio": "바이오",
    "medical-devices": "의료기기",
    "digital-health": "디지털 헬스"
  };
  // CATEGORY_ORDER mirrors classify.Categories (taxonomy SSOT). Chips render in
  // this order so the UI matches the server's category_counts ordering exactly.
  var CATEGORY_ORDER = ["ai", "humanoid-robotics", "bio", "medical-devices", "digital-health"];
  var STATUS_LABELS = {
    "tentative": "일정 미정",
    "postponed": "연기",
    "cancelled": "취소",
    "ended": "종료"
  };
  var COST_LABELS = { "free": "무료", "paid": "유료", "mixed": "무료+유료" };

  function escapeHtml(s) {
    if (s == null) return "";
    return String(s).replace(/[&<>"']/g, function(c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  function humanizeCat(slug) {
    return CAT_LABELS[slug] || slug;
  }

  function chipFor(cat) {
    return '<span class="chip cat-' + escapeHtml(cat) + '">' + escapeHtml(humanizeCat(cat)) + '</span>';
  }

  // categoryChip renders a clickable filter chip with an optional count badge.
  // count === undefined hides the badge (counts not loaded yet); count === 0
  // dims and disables the chip unless it is the currently selected one (so the
  // user can always toggle it back off).
  function categoryChip(slug, count, selected) {
    var badge = (count == null) ? "" : '<span class="chip-count num">' + escapeHtml(String(count)) + "</span>";
    var disabled = (count === 0 && !selected) ? " disabled" : "";
    return '<button type="button" class="chip chip-toggle cat-' + slug + '"' +
      ' data-cat="' + slug + '" aria-pressed="' + (selected ? "true" : "false") + '"' + disabled +
      ">" + escapeHtml(humanizeCat(slug)) + badge + "</button>";
  }

  function statusChip(e) {
    if (!e.status || e.status === "scheduled") return "";
    return '<span class="chip status-' + escapeHtml(e.status) + '">' + escapeHtml(STATUS_LABELS[e.status] || e.status) + '</span>';
  }

  function costChip(e) {
    if (!e.cost_hint || e.cost_hint === "unknown") return "";
    return '<span class="chip cost-' + escapeHtml(e.cost_hint) + '">' + escapeHtml(COST_LABELS[e.cost_hint] || e.cost_hint) + '</span>';
  }

  function otherChip(e) {
    if (e.excluded || !(e.categories || []).length) return '<span class="chip cat-other">기타</span>';
    return "";
  }

  function badgeHTML(e) {
    return (e.categories || []).map(chipFor).join("") + otherChip(e) + statusChip(e) + costChip(e);
  }

  window.EventIntelUI = {
    badgeHTML: badgeHTML,
    escapeHtml: escapeHtml,
    humanizeCat: humanizeCat,
    categories: CATEGORY_ORDER,
    categoryChip: categoryChip
  };
})();
