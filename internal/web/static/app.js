const main = document.getElementById("sections");

// the fragment names the tab to show; anything else shows the first
function showTab() {
  const tabs = Array.from(document.querySelectorAll(".tabs a"), (a) => a.dataset.tab);
  const want = location.hash.slice(1);
  const tab = tabs.includes(want) ? want : tabs[0];
  for (const el of document.querySelectorAll("[data-tab]")) {
    if (el.closest(".tabs")) {
      if (el.dataset.tab === tab) el.setAttribute("aria-current", "page");
      else el.removeAttribute("aria-current");
    } else {
      el.hidden = el.dataset.tab !== tab;
    }
  }
}

window.addEventListener("hashchange", showTab);
showTab();

async function refresh() {
  const open = new Set(
    Array.from(main.querySelectorAll("details[open]"), (d) => d.dataset.id),
  );
  const y = window.scrollY;
  const res = await fetch("/sections", { cache: "no-store" });
  if (!res.ok) return;
  main.innerHTML = await res.text();
  showTab();
  for (const d of main.querySelectorAll("details")) {
    if (open.has(d.dataset.id)) d.open = true;
  }
  const root = main.firstElementChild;
  if (root && root.dataset.title) document.title = root.dataset.title;
  window.scrollTo(0, y);
}

new EventSource("/events").addEventListener("snapshot", () => {
  refresh().catch(() => {});
});

const dialog = document.getElementById("settings");
if (dialog) {
  const form = document.getElementById("settings-form");
  const poll = document.getElementById("poll");
  const ignore = document.getElementById("ignore");
  const status = document.getElementById("settings-status");

  document.addEventListener("click", async (e) => {
    if (e.target.closest("[data-close-settings]")) {
      dialog.close();
      return;
    }
    if (!e.target.closest("[data-open-settings]")) return;
    const res = await fetch("/settings", { cache: "no-store" });
    if (!res.ok) return;
    const s = await res.json();
    poll.replaceChildren(
      ...s.choices.map((c) => new Option(c, c, c === s.poll, c === s.poll)),
    );
    ignore.value = s.ignore_actors.join(", ");
    status.textContent = "";
    dialog.showModal();
  });

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const res = await fetch("/settings", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        poll: poll.value,
        ignore_actors: ignore.value.split(/[\s,]+/).filter(Boolean),
      }),
    });
    if (res.ok) {
      dialog.close();
      return;
    }
    const body = await res.json().catch(() => ({ error: res.statusText }));
    status.textContent = body.error;
  });
}

const toast = document.getElementById("toast");

document.addEventListener("click", async (e) => {
  const button = e.target.closest("[data-archive]");
  if (!button) return;
  button.disabled = true;
  const res = await fetch("/archive", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      id: button.dataset.archive,
      archived: button.dataset.archived !== "true",
    }),
  }).catch(() => null);
  if (res && res.ok) return; // the event stream brings the new state
  button.disabled = false;
  const body = res
    ? await res.json().catch(() => ({ error: res.statusText }))
    : { error: "docket isn't answering" };
  toast.textContent = body.error;
  toast.hidden = false;
  setTimeout(() => {
    toast.hidden = true;
  }, 5000);
});
