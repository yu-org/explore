// Live updates for the home page.
//
// The server sends rows already rendered as HTML — the same markup the page was
// built with — so the browser only inserts nodes. Nothing is re-templated and
// nothing outside the inserted row is repainted.
(function () {
  "use strict";

  var blocksBody = document.getElementById("live-blocks");
  var txsBody = document.getElementById("live-txs");

  if (blocksBody || txsBody) {
    connect();
  }
  startClock();

  function connect() {
    var source = new EventSource("/stream");

    source.addEventListener("block", function (event) {
      var update;
      try {
        update = JSON.parse(event.data);
      } catch (err) {
        return;
      }

      if (blocksBody && update.blockRow) {
        prepend(blocksBody, update.blockRow);
      }
      if (txsBody && update.txRows && update.txRows.length) {
        // Reverse so the lowest index ends up furthest down, matching the
        // server-rendered ordering within a block.
        for (var i = update.txRows.length - 1; i >= 0; i--) {
          prepend(txsBody, update.txRows[i]);
        }
      }
      if (update.stats) {
        setText("stat-height", update.stats.height);
        setText("stat-txs", update.stats.totalTxs);
        setText("stat-blocktime", update.stats.avgBlockTime);
        setText("stat-tps", update.stats.tps);
      }
    });

    // EventSource reconnects on its own; the `retry` field sets the delay.
  }

  function maxRows(tbody) {
    var n = parseInt(tbody.getAttribute("data-max-rows"), 10);
    return n > 0 ? n : 12;
  }

  function prepend(tbody, html) {
    var template = document.createElement("template");
    template.innerHTML = html.trim();
    var row = template.content.firstElementChild;
    if (!row) return;

    var placeholder = tbody.querySelector(".empty-row");
    if (placeholder) placeholder.remove();

    row.classList.add("row-new");
    tbody.insertBefore(row, tbody.firstElementChild);

    var limit = maxRows(tbody);
    while (tbody.children.length > limit) {
      tbody.removeChild(tbody.lastElementChild);
    }
  }

  function setText(id, value) {
    if (value === undefined || value === null || value === "") return;
    var el = document.getElementById(id);
    if (el && el.textContent !== value) el.textContent = value;
  }

  // Ages are rendered server-side and would otherwise freeze on the page.
  function startClock() {
    var cells = function () { return document.querySelectorAll("[data-ts]"); };
    setInterval(function () {
      var now = Date.now() / 1000;
      cells().forEach(function (el) {
        var ts = parseInt(el.getAttribute("data-ts"), 10);
        if (!ts) return;
        var text = ago(now - ts);
        if (el.textContent !== text) el.textContent = text;
      });
    }, 5000);
  }

  function ago(seconds) {
    if (seconds < 0) return "just now";
    if (seconds < 60) return Math.floor(seconds) + "s ago";
    if (seconds < 3600) return Math.floor(seconds / 60) + "m ago";
    if (seconds < 86400) return Math.floor(seconds / 3600) + "h ago";
    return Math.floor(seconds / 86400) + "d ago";
  }
})();
