(function() {
  "use strict";

  function escapeHtml(s) {
    if (s == null) return "";
    return String(s).replace(/[&<>"']/g, function(c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  function fmtDateRange(s, e) {
    if (!s) return "일정 미정";
    if (!e || s === e) return s;
    return s + " \u2013 " + e;
  }

  function fmtDate(s) {
    return s || "\u2014";
  }

  function cleanText(s) {
    return String(s || "").replace(/\s+,/g, ",").replace(/,\s+/g, ", ");
  }

  function venueText(e) {
    if (!e.venue) return "";
    var v = e.venue;
    var parts = [];
    if (v.name) parts.push(cleanText(v.name));
    if (v.city) parts.push(cleanText(v.city));
    if (v.hall) parts.push(cleanText(v.hall));
    return parts.join(" \u00b7 ");
  }

  function firstSourceURL(e) {
    var sources = e.sources || [];
    for (var i = 0; i < sources.length; i++) {
      if (sources[i] && sources[i].url) return sources[i].url;
    }
    return "";
  }

  function actionLinks(e) {
    var links = [];
    if (e.homepage_url) {
      links.push('<a href="' + escapeHtml(e.homepage_url) + '" target="_blank" rel="noopener">공식 홈페이지</a>');
    }
    if (e.register_url) {
      links.push('<a href="' + escapeHtml(e.register_url) + '" target="_blank" rel="noopener">참가 신청</a>');
    }
    if (e.exhibit_url) {
      links.push('<a href="' + escapeHtml(e.exhibit_url) + '" target="_blank" rel="noopener">부스 문의</a>');
    }
    if (!links.length && firstSourceURL(e)) {
      links.push('<a href="' + escapeHtml(firstSourceURL(e)) + '" target="_blank" rel="noopener">공식 페이지</a>');
    }
    return links.length ? '<div class="detail-actions">' + links.join("") + '</div>' : "";
  }

  function detailItem(label, value) {
    if (!value) return "";
    return '<div class="detail-item"><span>' + escapeHtml(label) + '</span><strong>' + value + '</strong></div>';
  }

  function costText(e) {
    var labels = { free: "무료", paid: "유료", mixed: "무료+유료" };
    return labels[e.cost_hint] || "";
  }

  function actionSignals(e) {
    var actions = e.actions || {};
    var labels = [];
    if (actions.can_register) labels.push("참가 가능");
    if (actions.can_exhibit) labels.push("부스 문의 가능");
    if (actions.can_sponsor) labels.push("후원 문의 가능");
    if (actions.has_matchmaking) labels.push("비즈니스 상담");
    if (actions.has_startup_program) labels.push("스타트업 프로그램");
    if (!labels.length) return "";
    return '<div class="detail-signals">' + labels.map(function(label) {
      return '<span>' + escapeHtml(label) + '</span>';
    }).join("") + '</div>';
  }

  function renderModal(e, badgeHTML) {
    var nameEn = e.name_en && e.name_en !== e.name
      ? detailItem("영문명", escapeHtml(e.name_en))
      : "";
    var hero = [
      nameEn,
      detailItem("일정", '<span class="num">' + escapeHtml(fmtDateRange(e.start_date, e.end_date)) + '</span>'),
      detailItem("장소", escapeHtml(venueText(e))),
      detailItem("비용", escapeHtml(costText(e))),
      detailItem("참가 신청 마감", '<span class="num">' + escapeHtml(fmtDate(e.registration_deadline)) + '</span>'),
      detailItem("부스 신청 마감", '<span class="num">' + escapeHtml(fmtDate(e.exhibitor_deadline)) + '</span>')
    ].join("");
    var summary = e.summary
      ? '<section class="detail-section"><h3>행사 설명</h3><p>' + escapeHtml(e.summary) + '</p></section>'
      : "";
    var signalHTML = actionSignals(e);
    var linkHTML = actionLinks(e);
    var actions = signalHTML || linkHTML
      ? '<section class="detail-section"><h3>참가 정보</h3>' + signalHTML + linkHTML + '</section>'
      : "";
    var note = firstSourceURL(e)
      ? '<p class="detail-note">일정과 신청 조건은 주최 측 공식 페이지에서 다시 확인하세요.</p>'
      : "";
    return (
      '<div class="modal" role="document">' +
        '<button class="modal-close" aria-label="닫기">\u00d7</button>' +
        '<h2 id="modal-title">' + escapeHtml(e.name) + '</h2>' +
        (badgeHTML ? '<div class="modal-badges">' + badgeHTML + '</div>' : "") +
        '<section class="detail-section"><h3>행사 정보</h3><div class="detail-hero">' + hero + '</div></section>' +
        summary +
        actions +
        note +
      '</div>'
    );
  }

  window.EventIntelDetail = {
    renderModal: renderModal
  };
})();
