import { initializeSlideshowDrawing } from "./slideshow-drawing";
import { initializeSlideshowSnippets } from "./slideshow-snippets";

export interface SlideshowController {
  open(): void;
  destroy(): void;
}

export function createSlideshow(trigger: HTMLButtonElement): SlideshowController {
  const dialog = document.querySelector<HTMLDialogElement>("#landing-slideshow");
  const deck = document.querySelector<HTMLElement>("[data-slideshow-slides]");
  const viewport = dialog?.querySelector<HTMLElement>("[data-slideshow-viewport]");
  const stage = dialog?.querySelector<HTMLElement>("[data-slideshow-stage]");
  const ink = dialog?.querySelector<SVGSVGElement>("[data-slideshow-ink]");
  const previous = dialog?.querySelector<HTMLButtonElement>("[data-slideshow-previous]");
  const next = dialog?.querySelector<HTMLButtonElement>("[data-slideshow-next]");
  const close = dialog?.querySelector<HTMLButtonElement>("[data-slideshow-close]");
  const status = dialog?.querySelector<HTMLElement>("[data-slideshow-status]");
  if (!dialog || !deck || !viewport || !stage || !ink || !previous || !next || !close || !status) {
    throw new Error("The slideshow markup is incomplete.");
  }
  const slides = Array.from(deck.querySelectorAll<HTMLElement>(":scope > section:not([data-slideshow-hide])"));
  if (!slides.length) throw new Error("No presentation slides are available.");

  const controller = new AbortController();
  const { signal } = controller;
  const desktop = window.matchMedia("(min-width: 50rem)");
  const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
  const placeholder = document.createComment("Landing slideshow");
  const cleanupDrawing = initializeSlideshowDrawing();
  const cleanupSnippets = initializeSlideshowSnippets();
  let index = 0;
  let requestedIndex = 0;
  let scrollY = 0;
  let transition: ViewTransition | undefined;

  const fit = () => {
    if (!dialog.open) return;
    const width = deck.offsetWidth;
    const height = deck.offsetHeight;
    if (!width || !height) return;
    const scale = Math.min(1, viewport.clientWidth / width, viewport.clientHeight / height);
    stage.style.width = `${width * scale}px`;
    stage.style.height = `${height * scale}px`;
    deck.style.transform = `scale(${scale})`;
    ink.setAttribute("viewBox", `0 0 ${width} ${height}`);
  };

  const observer = new ResizeObserver(fit);
  observer.observe(viewport);
  observer.observe(deck);

  const pauseVideos = (slide: HTMLElement) => {
    slide.querySelectorAll("video").forEach(video => video.pause());
  };

  const showSlide = (target: number) => {
    const newIndex = Math.max(0, Math.min(slides.length - 1, target));
    if (newIndex !== index) pauseVideos(slides[index]);
    index = newIndex;
    slides.forEach((slide, i) => {
      slide.hidden = i !== index;
    });
    previous.disabled = index === 0;
    next.disabled = index === slides.length - 1;
    const heading = Array.from(slides[index].querySelectorAll<HTMLElement>("h1, h2")).find(element => element.checkVisibility());
    status.textContent = `${index + 1} / ${slides.length} — ${heading?.textContent?.trim() ?? "Slide"}`;
    // Keep focus out of hidden slides and disabled navigation buttons.
    if (document.activeElement instanceof HTMLElement && (document.activeElement.closest("section[hidden]") || (document.activeElement === previous && previous.disabled) || (document.activeElement === next && next.disabled))) {
      close.focus({ preventScroll: true });
    }
    fit();
    dialog.dataset.slideIndex = String(index);
    dialog.dispatchEvent(new Event("slideshow:slide-change"));
  };

  const navigate = (target: number) => {
    const newIndex = Math.max(0, Math.min(slides.length - 1, target));
    if (newIndex === requestedIndex) return;
    const direction = newIndex > requestedIndex ? "forward" : "backward";
    requestedIndex = newIndex;
    transition?.skipTransition();
    if (!document.startViewTransition || reducedMotion.matches) {
      showSlide(newIndex);
      return;
    }
    document.documentElement.dataset.slideshowDirection = direction;
    const current = document.startViewTransition(() => {
      if (dialog.open && requestedIndex === newIndex) showSlide(newIndex);
    });
    transition = current;
    current.finished.then(() => {
      if (transition !== current) return;
      transition = undefined;
      delete document.documentElement.dataset.slideshowDirection;
    });
  };

  const restore = () => {
    transition?.skipTransition();
    transition = undefined;
    delete document.documentElement.dataset.slideshowDirection;
    if (!placeholder.parentNode) return;
    slides.forEach(slide => {
      pauseVideos(slide);
      slide.hidden = false;
    });
    deck.style.removeProperty("transform");
    placeholder.replaceWith(deck);
    dialog.dispatchEvent(new Event("slideshow:reset"));
    window.scrollTo({ top: scrollY, behavior: "instant" });
    trigger.focus({ preventScroll: true });
  };

  const open = () => {
    if (!desktop.matches || dialog.open) return;
    scrollY = window.scrollY;
    deck.replaceWith(placeholder);
    stage.prepend(deck);
    dialog.showModal();
    requestedIndex = 0;
    showSlide(0);
    close.focus({ preventScroll: true });
  };

  previous.addEventListener("click", () => navigate(requestedIndex - 1), { signal });
  next.addEventListener("click", () => navigate(requestedIndex + 1), { signal });
  close.addEventListener(
    "click",
    () => {
      dialog.close();
      restore();
    },
    { signal }
  );
  dialog.addEventListener(
    "cancel",
    event => {
      event.preventDefault();
      dialog.close();
      restore();
    },
    { signal }
  );
  dialog.addEventListener(
    "close",
    () => {
      if (!dialog.open) restore();
    },
    { signal }
  );
  desktop.addEventListener(
    "change",
    () => {
      if (!desktop.matches && dialog.open) {
        dialog.close();
        restore();
      }
    },
    { signal }
  );
  dialog.addEventListener(
    "keydown",
    event => {
      if (dialog.querySelector("dialog[open]")) return;
      if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey || (event.target instanceof HTMLElement && event.target.closest("input, textarea, select, video, summary, [contenteditable]"))) return;
      if (event.target instanceof HTMLElement && event.target.closest('[role="tab"]') && ["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
      const destinations: Record<string, number> = {
        ArrowRight: requestedIndex + 1,
        ArrowDown: requestedIndex + 1,
        PageDown: requestedIndex + 1,
        ArrowLeft: requestedIndex - 1,
        ArrowUp: requestedIndex - 1,
        PageUp: requestedIndex - 1,
        Home: 0,
        End: slides.length - 1,
      };
      if (!(event.key in destinations)) return;
      event.preventDefault();
      navigate(destinations[event.key]);
    },
    { signal }
  );

  return {
    open,
    destroy() {
      controller.abort();
      observer.disconnect();
      if (dialog.open) dialog.close();
      restore();
      cleanupDrawing();
      cleanupSnippets();
    },
  };
}
