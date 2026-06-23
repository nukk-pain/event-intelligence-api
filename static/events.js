(function() {
  "use strict";

  function eventDateKey(event) {
    return event.start_date || "9999-12-31";
  }

  function sort(events) {
    return events.slice().sort(function(a, b) {
      var byDate = eventDateKey(a).localeCompare(eventDateKey(b));
      if (byDate) return byDate;
      return (a.name || "").localeCompare(b.name || "", "ko");
    });
  }

  function hasCategory(event) {
    return !event.excluded && (event.categories || []).length > 0;
  }

  function scoped(events, scope) {
    if (scope === "all") return events;
    if (scope === "opportunity") return events;
    return events.filter(hasCategory);
  }

  function mergeUnique(existing, incoming) {
    var seen = {};
    var merged = [];
    var added = 0;
    existing.forEach(function(event) {
      seen[event.event_id] = true;
      merged.push(event);
    });
    incoming.forEach(function(event) {
      if (seen[event.event_id]) return;
      seen[event.event_id] = true;
      merged.push(event);
      added++;
    });
    return { events: merged, added: added };
  }

  window.EventIntelEvents = {
    mergeUnique: mergeUnique,
    scoped: scoped,
    sort: sort
  };
})();
