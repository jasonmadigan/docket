const main = document.getElementById("sections");

async function refresh() {
  const open = new Set(
    Array.from(main.querySelectorAll("details[open]"), (d) => d.dataset.id),
  );
  const y = window.scrollY;
  const res = await fetch("/sections", { cache: "no-store" });
  if (!res.ok) return;
  main.innerHTML = await res.text();
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
