export function initializeSlideshowSnippets(): () => void {
  const presentation = document.querySelector<HTMLDialogElement>("#landing-slideshow");
  const dialog = presentation?.querySelector<HTMLDialogElement>("[data-slideshow-snippet-dialog]");
  const content = dialog?.querySelector<HTMLElement>("[data-snippet-content]");
  const title = dialog?.querySelector<HTMLElement>("#slideshow-snippet-title");
  const editHint = dialog?.querySelector<HTMLElement>("[data-snippet-edit-hint]");
  const close = dialog?.querySelector<HTMLButtonElement>("[data-snippet-close]");
  if (!presentation || !dialog || !content || !title || !editHint || !close) return () => {};
  const controller = new AbortController();
  const { signal } = controller;
  const attributes = new Map<HTMLElement, Map<string, string | null>>();
  let source: HTMLElement | undefined;

  const remember = (element: HTMLElement, name: string) => {
    let saved = attributes.get(element);
    if (!saved) {
      saved = new Map();
      attributes.set(element, saved);
    }
    if (!saved.has(name)) saved.set(name, element.getAttribute(name));
  };

  const enhance = () => {
    presentation.querySelectorAll<HTMLElement>("[data-slideshow-slides] :is([data-slideshow-snippet], pre, code)").forEach(element => {
      if (element.closest("a, button") || element.parentElement?.closest("[data-slideshow-snippet], pre, [data-snippet-trigger]")) return;
      const label = element.dataset.slideshowSnippet || (element.tagName === "CODE" ? element.textContent?.trim() : "snippet");
      for (const name of ["tabindex", "role", "aria-label", "aria-haspopup", "data-snippet-trigger"]) remember(element, name);
      element.tabIndex = 0;
      element.setAttribute("role", "button");
      element.setAttribute("aria-label", `Expand ${label}`);
      element.setAttribute("aria-haspopup", "dialog");
      element.setAttribute("data-snippet-trigger", "");
      for (let ancestor: HTMLElement | null = element; ancestor && ancestor !== presentation; ancestor = ancestor.parentElement) {
        if (ancestor.getAttribute("aria-hidden") === "true") {
          remember(ancestor, "aria-hidden");
          ancestor.removeAttribute("aria-hidden");
        }
      }
    });
  };

  const open = (element: HTMLElement) => {
    if (dialog.open) return;
    source = element;
    const clone = element.cloneNode(true);
    if (!(clone instanceof HTMLElement)) return;
    for (const node of [clone, ...clone.querySelectorAll<HTMLElement>("*")]) {
      for (const name of ["id", "tabindex", "role", "aria-label", "aria-hidden", "aria-haspopup", "aria-describedby", "data-snippet-trigger"]) node.removeAttribute(name);
    }
    if (element.tagName === "CODE") {
      const pre = document.createElement("pre");
      pre.append(clone);
      content.replaceChildren(pre);
    } else {
      content.replaceChildren(clone);
    }
    title.textContent = element.dataset.slideshowSnippet || "Expanded snippet";
    let editorCount = 0;
    for (const editor of content.querySelectorAll<HTMLElement>("pre, code")) {
      if (editor.tagName === "CODE" && editor.closest("pre")) continue;
      editor.setAttribute("contenteditable", "plaintext-only");
      editor.setAttribute("role", "textbox");
      editor.setAttribute("aria-label", `Edit ${title.textContent}${editorCount ? ` (${editorCount + 1})` : ""}`);
      editor.setAttribute("aria-multiline", "true");
      editor.setAttribute("aria-describedby", editHint.id);
      editor.setAttribute("data-snippet-editor", "");
      editor.tabIndex = 0;
      editorCount++;
    }
    editHint.hidden = editorCount === 0;
    dialog.showModal();
    close.focus({ preventScroll: true });
  };

  const dismiss = () => {
    dialog.close();
    if (source?.isConnected) source.focus({ preventScroll: true });
  };

  const reset = () => {
    if (dialog.open) dialog.close();
    content.replaceChildren();
    source = undefined;
    for (const [element, saved] of attributes) {
      for (const [name, value] of saved) {
        if (value === null) element.removeAttribute(name);
        else element.setAttribute(name, value);
      }
    }
    attributes.clear();
  };

  presentation.addEventListener("slideshow:slide-change", enhance, { signal });
  presentation.addEventListener("slideshow:reset", reset, { signal });
  presentation.addEventListener(
    "click",
    event => {
      if (!(event.target instanceof Element) || event.target.closest("a, button, input, select, textarea")) return;
      const element = event.target.closest<HTMLElement>("[data-snippet-trigger]");
      if (element) open(element);
    },
    { signal }
  );
  presentation.addEventListener(
    "keydown",
    event => {
      if (dialog.open || !(event.target instanceof HTMLElement) || !event.target.hasAttribute("data-snippet-trigger")) return;
      if (event.key !== "Enter" && event.key !== " ") return;
      event.preventDefault();
      event.stopPropagation();
      open(event.target);
    },
    { signal }
  );
  close.addEventListener("click", dismiss, { signal });
  dialog.addEventListener(
    "cancel",
    event => {
      event.preventDefault();
      event.stopPropagation();
      dismiss();
    },
    { signal }
  );
  return () => {
    controller.abort();
    reset();
  };
}
