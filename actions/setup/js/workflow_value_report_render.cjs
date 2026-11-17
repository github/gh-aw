// @ts-check

const COLORS = ["#0969da", "#1a7f37", "#bf8700", "#8250df"];

/** @param {unknown} value */
function xml(value) {
  return String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&apos;");
}

/** @param {unknown} value */
function markdown(value) {
  return String(value).replaceAll("\\", "\\\\").replaceAll("|", "\\|").replaceAll("\n", " ");
}

/** @param {number | null | undefined} value */
function count(value) {
  return Number.isSafeInteger(value) ? Number(value).toLocaleString("en-US") : "unavailable";
}

/** @param {Record<string, any>} observation */
function deliveryLabel(observation) {
  if (!observation || observation.coverage === "unavailable") return "unavailable";
  return `${count(observation.delivered)} (${observation.coverage})`;
}

/** @param {Record<string, any>} observation */
function adoptionLabel(observation) {
  if (!observation || observation.coverage === "unavailable") return "unavailable";
  return `${count(observation.accepted_human)} human / ${count(observation.accepted_delivery_fallback)} fallback (${observation.coverage})`;
}

/** @param {Record<string, any>} observation */
function survivalLabel(observation) {
  if (!observation || observation.coverage === "unavailable") return "unavailable";
  return `${count(observation.survival_observed)} observed / ${count(observation.survival_reversed)} reversed (${observation.coverage})`;
}

/**
 * @param {{
 *   title: string,
 *   subtitle: string,
 *   dates: string[],
 *   series: Array<{name: string, points: Array<{index: number, value: number, coverage: string}>}>,
 *   reference?: {name: string, value: number} | null,
 * }} options
 */
function renderLineChart({ title, subtitle, dates, series, reference = null }) {
  const width = 1080;
  const height = 420;
  const margin = { top: 82, right: 36, bottom: 72, left: 76 };
  const plotWidth = width - margin.left - margin.right;
  const plotHeight = height - margin.top - margin.bottom;
  const allPoints = series.flatMap(item => item.points).filter(point => Number.isFinite(point.value));
  const values = allPoints.map(point => point.value);
  if (reference && Number.isFinite(reference.value)) values.push(reference.value);
  const maximum = values.reduce((current, value) => Math.max(current, value), 1);
  const x = index => margin.left + (dates.length <= 1 ? plotWidth / 2 : (index / (dates.length - 1)) * plotWidth);
  const y = value => margin.top + plotHeight - (value / maximum) * plotHeight;
  const ticks = Array.from({ length: 5 }, (_, index) => Math.round((maximum * index) / 4));
  const xTickIndexes = [...new Set(Array.from({ length: Math.min(7, Math.max(1, dates.length)) }, (_, index) => Math.round((index * Math.max(0, dates.length - 1)) / Math.max(1, Math.min(6, dates.length - 1)))))];
  const coverage = { complete: 0, partial: 0, unavailable: 0 };
  for (const point of allPoints) {
    if (point.coverage === "complete") coverage.complete++;
    else if (point.coverage === "partial") coverage.partial++;
    else coverage.unavailable++;
  }
  const lines = [
    `<svg xmlns="http://www.w3.org/2000/svg" role="img" aria-labelledby="title description" viewBox="0 0 ${width} ${height}">`,
    `<title id="title">${xml(title)}</title>`,
    `<desc id="description">${xml(subtitle)}</desc>`,
    `<rect width="${width}" height="${height}" fill="#ffffff"/>`,
    `<text x="${margin.left}" y="30" font-family="system-ui,sans-serif" font-size="20" font-weight="600" fill="#1f2328">${xml(title)}</text>`,
    `<text x="${margin.left}" y="53" font-family="system-ui,sans-serif" font-size="13" fill="#59636e">${xml(subtitle)}</text>`,
    `<text x="${margin.left}" y="72" font-family="system-ui,sans-serif" font-size="12" fill="#59636e">${xml(`Evidence points: complete ${coverage.complete}, partial ${coverage.partial}; unavailable periods are gaps.`)}</text>`,
  ];
  for (const tick of ticks) {
    const py = y(tick);
    lines.push(`<line x1="${margin.left}" y1="${py}" x2="${width - margin.right}" y2="${py}" stroke="#d0d7de" stroke-width="1"/>`);
    lines.push(`<text x="${margin.left - 10}" y="${py + 4}" text-anchor="end" font-family="system-ui,sans-serif" font-size="11" fill="#59636e">${tick}</text>`);
  }
  for (const index of xTickIndexes) {
    if (!dates[index]) continue;
    const px = x(index);
    lines.push(`<line x1="${px}" y1="${margin.top + plotHeight}" x2="${px}" y2="${margin.top + plotHeight + 5}" stroke="#57606a"/>`);
    lines.push(`<text x="${px}" y="${height - 42}" text-anchor="middle" font-family="system-ui,sans-serif" font-size="11" fill="#59636e">${xml(dates[index])}</text>`);
  }
  if (reference) {
    const py = y(reference.value);
    lines.push(`<line x1="${margin.left}" y1="${py}" x2="${width - margin.right}" y2="${py}" stroke="#cf222e" stroke-width="1.5" stroke-dasharray="6 4"/>`);
    lines.push(`<text x="${width - margin.right}" y="${py - 6}" text-anchor="end" font-family="system-ui,sans-serif" font-size="11" fill="#cf222e">${xml(`${reference.name}: ${reference.value}`)}</text>`);
  }
  series.forEach((item, seriesIndex) => {
    const color = COLORS[seriesIndex % COLORS.length];
    const points = [...item.points].sort((a, b) => a.index - b.index);
    /** @type {Array<Array<{index: number, value: number, coverage: string}>>} */
    const segments = [];
    for (const point of points) {
      const current = segments.at(-1);
      const previous = current?.at(-1);
      if (!previous || previous.index + 1 !== point.index) segments.push([point]);
      else if (current) current.push(point);
    }
    for (const segment of segments) {
      if (segment.length > 1) {
        lines.push(`<polyline fill="none" stroke="${color}" stroke-width="2.5" points="${segment.map(point => `${x(point.index)},${y(point.value)}`).join(" ")}"/>`);
      }
    }
    for (const point of points) {
      const fill = point.coverage === "complete" ? color : "#ffffff";
      const dash = point.coverage === "partial" ? ' stroke-dasharray="2 1"' : "";
      lines.push(`<circle cx="${x(point.index)}" cy="${y(point.value)}" r="4" fill="${fill}" stroke="${color}" stroke-width="2"${dash}/>`);
    }
    const legendX = margin.left + seriesIndex * 210;
    lines.push(`<line x1="${legendX}" y1="${height - 16}" x2="${legendX + 24}" y2="${height - 16}" stroke="${color}" stroke-width="3"/>`);
    lines.push(`<text x="${legendX + 31}" y="${height - 12}" font-family="system-ui,sans-serif" font-size="12" fill="#1f2328">${xml(item.name)}</text>`);
  });
  if (allPoints.length === 0) {
    lines.push(`<text x="${margin.left + plotWidth / 2}" y="${margin.top + plotHeight / 2}" text-anchor="middle" font-family="system-ui,sans-serif" font-size="16" fill="#59636e">No comparable evidence is available for this series.</text>`);
  }
  lines.push("</svg>\n");
  return lines.join("\n");
}

/** @param {Record<string, any>} portfolio */
function renderCharts(portfolio) {
  const dates = portfolio.periods.map(period => period.date);
  const inventoryPoints = portfolio.periods
    .map((period, index) => ({
      index,
      value: period.inventory.reported_count,
      coverage: period.inventory.coverage,
    }))
    .filter(point => Number.isSafeInteger(point.value));
  const selected = (field, name) => ({
    name,
    points: portfolio.periods
      .map((period, index) => ({
        index,
        value: period.selected_window.aggregate[field],
        coverage: period.selected_window.aggregate.coverage,
      }))
      .filter(point => periodValue(point)),
  });
  const retained = (field, name) => ({
    name,
    points: portfolio.periods
      .map((period, index) => ({
        index,
        value: period.retained_population.aggregate[field],
        coverage: period.retained_population.aggregate.coverage,
      }))
      .filter(point => periodValue(point)),
  });
  const needsDefinitionPoints = portfolio.periods
    .map((period, index) => ({
      index,
      value: period.attributed_value.state_counts.needs_definition,
      coverage: period.attributed_value.status_coverage,
    }))
    .filter(point => periodValue(point));
  const measuredPoints = portfolio.periods
    .map((period, index) => ({
      index,
      value: period.attributed_value.state_counts.measured,
      coverage: period.attributed_value.status_coverage,
    }))
    .filter(point => periodValue(point));
  return {
    "inventory-coverage.svg": renderLineChart({
      title: "Workflow inventory coverage",
      subtitle: "Reported workflow identities per retained period. The red reference is the current paired source/lock inventory.",
      dates,
      series: [{ name: "Reported workflows", points: inventoryPoints }],
      reference: { name: "Current inventory", value: portfolio.inventory.count },
    }),
    "selected-lifecycle.svg": renderLineChart({
      title: "Selected-window delivery and adoption",
      subtitle: "Exact 24-hour selected-run windows only. Legacy outcomes are excluded; unavailable periods are not zero.",
      dates,
      series: [selected("delivered", "Delivered"), selected("accepted_human", "Human accepted"), selected("accepted_delivery_fallback", "Delivery fallback")],
    }),
    "retained-lifecycle.svg": renderLineChart({
      title: "Retained-population lifecycle",
      subtitle: "Prospective retained-ledger snapshots. These are population snapshots, not selected-window observations or attributed value.",
      dates,
      series: [retained("delivered", "Retained deliveries"), retained("accepted_human", "Human accepted"), retained("survival_reversed", "Reversed")],
    }),
    "value-evidence.svg": renderLineChart({
      title: "Attributed value evidence state",
      subtitle: "Workflow evidence-state counts, not a value score. Native units are never averaged or combined.",
      dates,
      series: [
        { name: "Needs definition", points: needsDefinitionPoints },
        { name: "Measured", points: measuredPoints },
      ],
    }),
  };
}

/** @param {{value: number}} point */
function periodValue(point) {
  return Number.isFinite(point.value);
}

/** @param {Record<string, any>} portfolio */
function renderMarkdown(portfolio) {
  const current = portfolio.current;
  const retained = current.retained_population.aggregate;
  const selected = current.selected_window.aggregate;
  const lines = [
    "### GitHub Agentic Workflows value portfolio",
    "",
    `Generated from repository evidence at \`${portfolio.generated_at}\` for \`${portfolio.repository}\`. The inventory contains **${portfolio.inventory.count.toLocaleString("en-US")}** paired top-level workflow source and lock identities.`,
    "",
    "> [!WARNING]",
    `> Attributed value is not measured for ${portfolio.current.attributed_value.state_counts.needs_definition.toLocaleString("en-US")} workflow(s) because no reviewed, frozen repository-local longitudinal definition and comparable observations are recorded. Delivery, adoption, snapshot survival, legacy outcomes, run success, labels, and optional run-scoped operational-value graders are not substituted.`,
    "",
    "### Current evidence",
    "",
    "| Layer | Coverage | Evidence |",
    "|---|---|---:|",
    `| Selected 24-hour delivery | ${selected.coverage} | ${count(selected.delivered)} delivered |`,
    `| Selected 24-hour adoption | ${selected.coverage} | ${count(selected.accepted_human)} human / ${count(selected.accepted_delivery_fallback)} fallback |`,
    `| Retained delivery population | ${retained.coverage} | ${count(retained.delivered)} delivered |`,
    `| Retained snapshot survival | ${retained.coverage} | ${count(retained.survival_observed)} observed / ${count(retained.survival_reversed)} reversed |`,
    `| Attributed value measurement | ${current.attributed_value.measurement_coverage} | ${count(current.attributed_value.state_counts.measured)} measured; ${count(current.attributed_value.state_counts.needs_definition)} need definition |`,
    "",
    "### Time series",
    "",
    "Every plot carries evidence coverage. Missing, legacy, incomparable, partial, and unavailable periods remain gaps rather than healthy zeros.",
    "",
    "![Workflow inventory coverage](plots/inventory-coverage.svg)",
    "",
    "![Selected-window delivery and adoption](plots/selected-lifecycle.svg)",
    "",
    "![Retained-population lifecycle](plots/retained-lifecycle.svg)",
    "",
    "![Attributed value evidence state](plots/value-evidence.svg)",
    "",
    "<details>",
    "<summary><b>Workflow portfolio</b></summary>",
    "",
    "| Workflow | Selected delivery | Selected adoption | Retained delivery | Retained adoption | Snapshot survival | Attributed value | Run-scoped operational value |",
    "|---|---:|---:|---:|---:|---:|---|---|",
  ];
  for (const workflow of portfolio.workflows) {
    lines.push(
      `| \`${markdown(workflow.id)}\` | ${deliveryLabel(workflow.current.selected_window.delivery)} | ${adoptionLabel(workflow.current.selected_window.adoption)} | ${deliveryLabel(workflow.current.retained_population)} | ${adoptionLabel(workflow.current.retained_population)} | ${survivalLabel(workflow.current.retained_population)} | ${markdown(workflow.current.attributed_value.state.replaceAll("_", " "))} | ${markdown(workflow.operational_value.state.replaceAll("_", " "))} |`
    );
  }
  lines.push(
    "",
    "</details>",
    "",
    "<details>",
    "<summary><b>Retained period coverage</b></summary>",
    "",
    "| Date | Collection | Inventory | Comparable window | Selected lifecycle | Retained population | Attributed value |",
    "|---|---|---:|---|---|---|---|"
  );
  for (const period of portfolio.periods) {
    lines.push(
      `| ${period.date} | ${period.collection_status} | ${count(period.inventory.reported_count)} / ${portfolio.inventory.count.toLocaleString("en-US")} (${period.inventory.coverage}) | ${period.comparability} | ${period.selected_window.aggregate.coverage} | ${period.retained_population.aggregate.coverage} | ${period.attributed_value.measurement_coverage} |`
    );
  }
  lines.push(
    "",
    "</details>",
    "",
    "### Evidence boundaries",
    "",
    `- The current selected window is derived from canonical ledger deliveries inside the exact metrics window with their current adoption snapshot. Historical selected-window points come only from \`${portfolio.sources.metrics.daily_glob}\`; latest selection happens before aggregation and only exact 24-hour periods are plotted.`,
    `- Retained-population history comes from \`${portfolio.sources.retained_lifecycle.daily_glob}\` and begins prospectively. The canonical identity ledger remains \`${portfolio.sources.retained_lifecycle.ledger.memory_id}:${portfolio.sources.retained_lifecycle.ledger.path}\` with SHA-256 \`${portfolio.sources.retained_lifecycle.ledger.sha256 || "unavailable"}\`.`,
    "- Legacy `safe_output_outcomes` are recorded as unsupported lifecycle/value evidence and are never reclassified.",
    "- Restricted-JavaScript `graders.operational-value.script` declarations are reported only as optional run-scoped capability; they do not establish longitudinal attributed value.",
    "- Unlike native value units remain separate. This report does not compute a universal score or average.",
    ""
  );
  return lines.join("\n");
}

module.exports = { renderCharts, renderLineChart, renderMarkdown };
