type Tool = "pointer" | "rect" | "arrow" | "emoji";
interface Point {
  x: number;
  y: number;
}
interface Drawing {
  tool: Exclude<Tool, "pointer">;
  start: Point;
  end: Point;
  color: string;
  emoji: string;
}

const svgNamespace = "http://www.w3.org/2000/svg";

export function initializeSlideshowDrawing(): () => void {
  const dialog = document.querySelector<HTMLDialogElement>("#landing-slideshow");
  const root = dialog?.querySelector<HTMLElement>("[data-slideshow-drawing]");
  const ink = dialog?.querySelector<SVGSVGElement>("[data-slideshow-ink]");
  const toggle = root?.querySelector<HTMLButtonElement>("[data-drawing-toggle]");
  const panel = root?.querySelector<HTMLElement>("#slideshow-drawing-panel");
  const undo = root?.querySelector<HTMLButtonElement>("[data-drawing-undo]");
  const clear = root?.querySelector<HTMLButtonElement>("[data-drawing-clear]");
  const status = root?.querySelector<HTMLElement>("[data-drawing-status]");
  if (!dialog || !root || !ink || !toggle || !panel || !undo || !clear || !status) return () => {};

  const controller = new AbortController();
  const { signal } = controller;
  const history = new Map<number, Drawing[]>();
  let index = 0;
  let tool: Tool = "pointer";
  let color = "#ffe135";
  let emoji = "😄";
  let gesture: { pointerId: number; drawing: Drawing } | undefined;

  const element = (name: string, attributes: Record<string, string>) => {
    const node = document.createElementNS(svgNamespace, name);
    for (const [key, value] of Object.entries(attributes)) node.setAttribute(key, value);
    return node;
  };

  const renderDrawing = (drawing: Drawing) => {
    const { start, end } = drawing;
    const group = element("g", { "data-drawing-kind": drawing.tool });
    if (drawing.tool === "emoji") {
      const distance = Math.hypot(end.x - start.x, end.y - start.y);
      const size = Math.min(512, Math.max(64, distance * 1.8));
      const angle = distance > 1 ? (Math.atan2(end.y - start.y, end.x - start.x) * 180) / Math.PI - 90 : 0;
      const stamp = element("text", {
        x: String(start.x),
        y: String(start.y),
        "font-size": String(size),
        "text-anchor": "middle",
        "dominant-baseline": "central",
        transform: `rotate(${angle} ${start.x} ${start.y})`,
      });
      stamp.textContent = drawing.emoji;
      group.append(stamp);
    } else {
      let attributes: Record<string, string>;
      if (drawing.tool === "arrow") {
        const dx = end.x - start.x;
        const dy = end.y - start.y;
        const angle = Math.atan2(dy, dx);
        const size = Math.min(28, Math.hypot(dx, dy) * 0.35);
        const head = (offset: number) => `${end.x - size * Math.cos(angle + offset)} ${end.y - size * Math.sin(angle + offset)}`;
        attributes = { d: `M ${start.x} ${start.y} L ${end.x} ${end.y} M ${head(-Math.PI / 6)} L ${end.x} ${end.y} L ${head(Math.PI / 6)}` };
      } else {
        attributes = { x: String(Math.min(start.x, end.x)), y: String(Math.min(start.y, end.y)), width: String(Math.abs(end.x - start.x)), height: String(Math.abs(end.y - start.y)) };
      }
      for (const [stroke, width] of [
        ["#ffffff", "8"],
        [drawing.color, "5"],
      ]) {
        group.append(
          element(drawing.tool === "arrow" ? "path" : "rect", {
            ...attributes,
            fill: "none",
            stroke,
            "stroke-width": width,
            "stroke-linecap": "round",
            "stroke-linejoin": "round",
          })
        );
      }
    }
    return group;
  };

  const render = () => {
    const drawings = history.get(index) ?? [];
    ink.replaceChildren(...drawings.map(renderDrawing));
    if (gesture) ink.append(renderDrawing(gesture.drawing));
    undo.disabled = clear.disabled = drawings.length === 0;
  };

  const announce = () => {
    const count = history.get(index)?.length ?? 0;
    status.textContent = `${count} drawing${count === 1 ? "" : "s"} on this slide.`;
  };

  const cancelGesture = () => {
    if (!gesture) return;
    const pointerId = gesture.pointerId;
    gesture = undefined;
    if (ink.hasPointerCapture(pointerId)) ink.releasePointerCapture(pointerId);
    render();
  };

  const setTool = (value: Tool) => {
    cancelGesture();
    tool = value;
    ink.dataset.tool = tool;
    root.querySelectorAll<HTMLButtonElement>("[data-drawing-tool]").forEach(button => {
      button.setAttribute("aria-pressed", String(button.dataset.drawingTool === tool && (tool !== "emoji" || button.dataset.emoji === emoji)));
    });
  };

  const point = (event: PointerEvent): Point => {
    const bounds = ink.getBoundingClientRect();
    return {
      x: Math.max(0, Math.min(ink.viewBox.baseVal.width, ((event.clientX - bounds.x) * ink.viewBox.baseVal.width) / bounds.width)),
      y: Math.max(0, Math.min(ink.viewBox.baseVal.height, ((event.clientY - bounds.y) * ink.viewBox.baseVal.height) / bounds.height)),
    };
  };

  const updateGesture = (event: PointerEvent) => {
    if (!gesture || event.pointerId !== gesture.pointerId) return;
    const end = point(event);
    const { drawing } = gesture;
    if (event.shiftKey && drawing.tool === "rect") {
      const dx = end.x - drawing.start.x;
      const dy = end.y - drawing.start.y;
      const size = Math.max(Math.abs(dx), Math.abs(dy));
      end.x = drawing.start.x + (dx < 0 ? -size : size);
      end.y = drawing.start.y + (dy < 0 ? -size : size);
    }
    drawing.end = end;
    render();
  };

  const undoDrawing = () => {
    cancelGesture();
    history.get(index)?.pop();
    render();
    announce();
  };

  const reset = () => {
    cancelGesture();
    history.clear();
    panel.hidden = true;
    toggle.setAttribute("aria-expanded", "false");
    setTool("pointer");
    render();
    announce();
  };

  toggle.addEventListener(
    "click",
    () => {
      panel.hidden = !panel.hidden;
      toggle.setAttribute("aria-expanded", String(!panel.hidden));
    },
    { signal }
  );
  root.querySelectorAll<HTMLButtonElement>("[data-drawing-tool]").forEach(button => {
    button.addEventListener(
      "click",
      () => {
        const value = button.dataset.drawingTool;
        if (value !== "pointer" && value !== "rect" && value !== "arrow" && value !== "emoji") return;
        if (button.dataset.emoji) emoji = button.dataset.emoji;
        setTool(value);
      },
      { signal }
    );
  });
  root.querySelectorAll<HTMLButtonElement>("[data-drawing-color]").forEach(button => {
    button.addEventListener(
      "click",
      () => {
        color = button.dataset.drawingColor ?? color;
        root.querySelectorAll<HTMLButtonElement>("[data-drawing-color]").forEach(choice => {
          choice.setAttribute("aria-pressed", String(choice === button));
        });
      },
      { signal }
    );
  });
  undo.addEventListener("click", undoDrawing, { signal });
  clear.addEventListener(
    "click",
    () => {
      cancelGesture();
      history.delete(index);
      render();
      announce();
    },
    { signal }
  );

  ink.addEventListener(
    "pointerdown",
    event => {
      if (tool === "pointer" || event.button !== 0 || gesture) return;
      event.preventDefault();
      const start = point(event);
      gesture = { pointerId: event.pointerId, drawing: { tool, start, end: { ...start }, color, emoji } };
      ink.setPointerCapture(event.pointerId);
      render();
    },
    { signal }
  );
  ink.addEventListener("pointermove", updateGesture, { signal });
  ink.addEventListener(
    "pointerup",
    event => {
      if (!gesture || event.pointerId !== gesture.pointerId) return;
      updateGesture(event);
      const { drawing } = gesture;
      gesture = undefined;
      if (drawing.tool === "emoji" || Math.hypot(drawing.end.x - drawing.start.x, drawing.end.y - drawing.start.y) > 3) {
        const drawings = history.get(index) ?? [];
        drawings.push(drawing);
        history.set(index, drawings);
      }
      if (ink.hasPointerCapture(event.pointerId)) ink.releasePointerCapture(event.pointerId);
      render();
      announce();
    },
    { signal }
  );
  ink.addEventListener("pointercancel", cancelGesture, { signal });
  ink.addEventListener("lostpointercapture", cancelGesture, { signal });
  dialog.addEventListener(
    "slideshow:slide-change",
    () => {
      cancelGesture();
      index = Number(dialog.dataset.slideIndex);
      render();
    },
    { signal }
  );
  dialog.addEventListener("slideshow:reset", reset, { signal });
  dialog.addEventListener(
    "keydown",
    event => {
      if (dialog.querySelector("dialog[open]")) return;
      if (event.target instanceof HTMLElement && event.target.closest("input, textarea, select, [contenteditable]")) return;
      if ((event.ctrlKey || event.metaKey) && !event.shiftKey && event.key.toLowerCase() === "z") {
        event.preventDefault();
        undoDrawing();
      } else if (event.key === "Escape" && (tool !== "pointer" || !panel.hidden || Array.from(history.values()).some(drawings => drawings.length > 0))) {
        event.preventDefault();
        event.stopPropagation();
        reset();
        toggle.focus();
      }
    },
    { signal }
  );
  setTool("pointer");
  return () => {
    controller.abort();
    reset();
  };
}
