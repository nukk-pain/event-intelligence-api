(function() {
  "use strict";

  var ui = window.EventIntelUI;

  var el = {
    form: document.getElementById("ask-form"),
    input: document.getElementById("ask-input"),
    submit: document.getElementById("ask-submit"),
    state: document.getElementById("ask-state"),
    results: document.getElementById("ask-results")
  };

  // The server answers within 30s or returns a JSON-RPC timeout error; the
  // client waits slightly longer so the server's own error wins when possible.
  var CLIENT_TIMEOUT_MS = 35000;

  function setState(msg) {
    el.state.hidden = !msg;
    el.state.textContent = msg || "";
  }

  function setBusy(busy) {
    el.input.disabled = busy;
    el.submit.disabled = busy;
  }

  function fmtDateRange(s, e) {
    if (!s) return "일정 미정";
    if (!e || s === e) return s;
    return s + " – " + e;
  }

  function retryMessage(seconds) {
    var min = Math.ceil(seconds / 60);
    if (min <= 1) return "질문이 몰려 잠시 쉬어가요. 잠시 뒤에 다시 물어보세요.";
    return "질문이 몰려 잠시 쉬어가요. 약 " + min + "분 뒤에 다시 물어보세요.";
  }

  function cardHTML(e) {
    var meta = [];
    if (e.venue) meta.push(ui.escapeHtml(e.venue));
    if (e.city && e.city !== e.venue) meta.push(ui.escapeHtml(e.city));
    var links = "";
    if (e.register_url) {
      links += '<a href="' + ui.escapeHtml(e.register_url) + '" target="_blank" rel="noopener">등록 안내</a>';
    }
    if (e.source_url) {
      links += '<a href="' + ui.escapeHtml(e.source_url) + '" target="_blank" rel="noopener">원문 보기</a>';
    }
    return '<article class="ask-card">' +
      '<div class="card-date num">' + ui.escapeHtml(fmtDateRange(e.start_date, e.end_date)) + '</div>' +
      '<div class="card-name">' + ui.escapeHtml(e.name) + '</div>' +
      (meta.length ? '<div class="card-venue">' + meta.join(", ") + '</div>' : "") +
      (links ? '<div class="ask-card-links">' + links + '</div>' : "") +
      '</article>';
  }

  function renderResults(payload) {
    var events = payload.events || [];
    if (!events.length) {
      el.results.hidden = true;
      el.results.innerHTML = "";
      setState("조건에 맞는 행사가 없어요. 질문을 조금 바꿔보세요.");
      return;
    }
    setState("");
    el.results.innerHTML =
      '<p class="ask-count">' + events.length + "개 행사를 찾았어요</p>" +
      events.map(cardHTML).join("") +
      '<button type="button" id="ask-clear" class="ask-clear">결과 지우기</button>';
    el.results.hidden = false;
    var clear = document.getElementById("ask-clear");
    if (clear) clear.addEventListener("click", function() {
      el.results.hidden = true;
      el.results.innerHTML = "";
      setState("");
    });
  }

  // One stateless JSON-RPC message per question; /mcp is same-origin behind the
  // site proxy, so no session and no CORS are involved.
  function ask(question) {
    var controller = new AbortController();
    var timer = setTimeout(function() { controller.abort(); }, CLIENT_TIMEOUT_MS);
    setBusy(true);
    setState("찾는 중이에요");
    el.results.hidden = true;

    fetch("/mcp", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "ask_events", arguments: { question: question } }
      }),
      signal: controller.signal
    }).then(function(res) {
      if (res.status === 429) {
        var retry = parseInt(res.headers.get("Retry-After"), 10);
        throw { copy: retryMessage(isNaN(retry) ? 600 : retry) };
      }
      // Non-JSON bodies (proxy error pages) fail the json() parse below and
      // fall through to the generic message.
      return res.json();
    }).then(function(rpc) {
      if (!rpc || rpc.error || !rpc.result || !rpc.result.content || !rpc.result.content.length) {
        throw {};
      }
      var payload = JSON.parse(rpc.result.content[0].text);
      if (typeof payload.count !== "number") throw {};
      renderResults(payload);
    }).catch(function(err) {
      setState((err && err.copy) || "지금은 답을 가져오지 못했어요. 잠시 뒤 다시 시도해 주세요.");
    }).finally(function() {
      clearTimeout(timer);
      setBusy(false);
    });
  }

  if (el.form) {
    el.form.addEventListener("submit", function(ev) {
      ev.preventDefault();
      var q = el.input.value.trim();
      if (!q) return;
      ask(q);
    });
  }

  window.EventIntelAsk = { ask: ask };
})();
