// @ts-check

function summarizeHelpText(value, maxLen) {
  const normalized = String(value || "")
    .replace(/\s+/g, " ")
    .trim();
  if (!Number.isFinite(maxLen) || maxLen <= 0) {
    return normalized;
  }
  if (normalized.length <= maxLen) {
    return normalized;
  }
  return `${normalized.slice(0, maxLen - 1)}…`;
}

function schemaAllowsType(schema, type) {
  if (!schema || typeof schema !== "object") {
    return false;
  }
  if (schema.type === type) {
    return true;
  }
  if (Array.isArray(schema.type)) {
    return schema.type.includes(type);
  }
  return false;
}

function isScalarSchema(schema) {
  if (!schema || typeof schema !== "object") {
    return false;
  }
  if (Array.isArray(schema.enum) && schema.enum.length > 0) {
    return true;
  }
  if (Array.isArray(schema.oneOf) && schema.oneOf.length > 0) {
    return schema.oneOf.every(option => isScalarSchema(option));
  }
  if (Array.isArray(schema.anyOf) && schema.anyOf.length > 0) {
    return schema.anyOf.every(option => isScalarSchema(option));
  }
  return schemaAllowsType(schema, "string") || schemaAllowsType(schema, "number") || schemaAllowsType(schema, "integer") || schemaAllowsType(schema, "boolean");
}

function scalarKinds(schema) {
  const kinds = new Set();
  if (!schema || typeof schema !== "object") {
    return kinds;
  }
  if (Array.isArray(schema.oneOf) && schema.oneOf.length > 0) {
    for (const option of schema.oneOf) {
      for (const kind of scalarKinds(option)) {
        kinds.add(kind);
      }
    }
    return kinds;
  }
  if (Array.isArray(schema.anyOf) && schema.anyOf.length > 0) {
    for (const option of schema.anyOf) {
      for (const kind of scalarKinds(option)) {
        kinds.add(kind);
      }
    }
    return kinds;
  }
  if (schemaAllowsType(schema, "string")) {
    kinds.add("string");
  }
  if (schemaAllowsType(schema, "number") || schemaAllowsType(schema, "integer")) {
    kinds.add("number");
  }
  if (schemaAllowsType(schema, "boolean")) {
    kinds.add("boolean");
  }
  return kinds;
}

function scalarPlaceholder(paramName, schema) {
  if (Array.isArray(schema?.enum) && schema.enum.length > 0) {
    return `<${schema.enum.join("|")}>`;
  }
  const kinds = scalarKinds(schema);
  if (kinds.size > 1) {
    const combined = [];
    if (kinds.has("boolean")) {
      combined.push("true|false");
    }
    if (kinds.has("number")) {
      combined.push("number");
    }
    if (kinds.has("string")) {
      combined.push("string");
    }
    return `<${combined.join("|")}>`;
  }
  if (kinds.has("boolean")) {
    return "<true|false>";
  }
  if (kinds.has("number")) {
    return "<number>";
  }
  if (kinds.has("string")) {
    if (paramName === "rationale") {
      return `<reason, max ${typeof schema.maxLength === "number" ? schema.maxLength : 280} characters>`;
    }
    if (typeof schema.maxLength === "number") {
      return `<text, max ${schema.maxLength} characters>`;
    }
    if (paramName === "issue_type") {
      return "<type>";
    }
    return "<value>";
  }
  return "<value>";
}

function chooseExampleSchema(schema, preferObject) {
  if (!schema || typeof schema !== "object") {
    return {};
  }
  if (Array.isArray(schema.oneOf) && schema.oneOf.length > 0) {
    if (preferObject) {
      const objectOption = schema.oneOf.find(option => schemaAllowsType(option, "object"));
      if (objectOption) {
        return objectOption;
      }
    }
    return schema.oneOf[0];
  }
  if (Array.isArray(schema.anyOf) && schema.anyOf.length > 0) {
    if (preferObject) {
      const objectOption = schema.anyOf.find(option => schemaAllowsType(option, "object"));
      if (objectOption) {
        return objectOption;
      }
    }
    return schema.anyOf[0];
  }
  return schema;
}

function exampleValueForKey(key, schema) {
  const chosenSchema = chooseExampleSchema(schema, key === "labels");
  if (Array.isArray(chosenSchema.enum) && chosenSchema.enum.length > 0) {
    return chosenSchema.enum.includes("HIGH") ? "HIGH" : chosenSchema.enum[0];
  }
  if (schemaAllowsType(chosenSchema, "boolean")) {
    return key === "suggest" ? true : false;
  }
  if (schemaAllowsType(chosenSchema, "integer") || schemaAllowsType(chosenSchema, "number")) {
    if (key.endsWith("_number") || key === "item_number") {
      return 123;
    }
    return 1;
  }
  if (schemaAllowsType(chosenSchema, "array")) {
    const itemSchema = chooseExampleSchema(chosenSchema.items, key === "labels");
    return [exampleValueForKey(key === "labels" ? "label" : key, itemSchema)];
  }
  if (schemaAllowsType(chosenSchema, "object")) {
    const properties = chosenSchema.properties && typeof chosenSchema.properties === "object" ? chosenSchema.properties : {};
    const required = new Set(Array.isArray(chosenSchema.required) ? chosenSchema.required : []);
    const result = {};
    const keys = Object.keys(properties);
    for (const propertyKey of keys) {
      if (required.has(propertyKey)) {
        result[propertyKey] = exampleValueForKey(propertyKey, properties[propertyKey]);
      }
    }
    return result;
  }
  if (schemaAllowsType(chosenSchema, "string")) {
    switch (key) {
      case "issue_type":
        return "Bug";
      case "name":
        return "bug";
      case "rationale":
        return "The report describes reproducible incorrect behavior.";
      case "confidence":
        return "HIGH";
      case "agent":
        return "copilot";
      default:
        return "value";
    }
  }
  return "value";
}

function buildOrderedOptionEntries(schema) {
  const properties = schema?.properties && typeof schema.properties === "object" ? schema.properties : {};
  const requiredSet = new Set(Array.isArray(schema?.required) ? schema.required : []);
  const entries = Object.entries(properties).map(([key, propertySchema]) => ({ key, schema: propertySchema, required: requiredSet.has(key) }));
  entries.sort((a, b) => {
    if (a.required !== b.required) {
      return a.required ? -1 : 1;
    }
    return a.key.localeCompare(b.key);
  });
  return entries;
}

function collectRecommendedKeys(schema, properties) {
  const selected = new Set(Array.isArray(schema?.required) ? schema.required : []);
  const variants = [];
  if (Array.isArray(schema?.oneOf)) {
    variants.push(...schema.oneOf);
  }
  if (Array.isArray(schema?.anyOf)) {
    variants.push(...schema.anyOf);
  }
  for (const variant of variants) {
    const variantRequired = Array.isArray(variant?.required) ? variant.required : [];
    if (variantRequired.length === 0) {
      continue;
    }
    if (!variantRequired.every(key => key in properties)) {
      continue;
    }
    for (const key of variantRequired) {
      selected.add(key);
    }
    break;
  }
  return selected;
}

function shouldUseJsonMode(schema) {
  return buildOrderedOptionEntries(schema).some(entry => !isScalarSchema(entry.schema));
}

function formatShellMultiline(lines) {
  if (!Array.isArray(lines) || lines.length === 0) {
    return "";
  }
  if (lines.length === 1) {
    return lines[0];
  }
  return lines.map((line, index) => (index === lines.length - 1 ? line : `${line} \\`)).join("\n");
}

function renderToolSignature(serverName, tool, options = {}) {
  const schema = tool?.inputSchema && typeof tool.inputSchema === "object" ? tool.inputSchema : {};
  const optionEntries = buildOrderedOptionEntries(schema);
  if (!optionEntries.length) {
    return `${serverName} ${tool.name}`;
  }
  if (shouldUseJsonMode(schema)) {
    return `${serverName} ${tool.name} '<json object>'`;
  }
  const tokens = [];
  for (const entry of optionEntries) {
    const optionText = `--${entry.key} ${scalarPlaceholder(entry.key, entry.schema)}`;
    tokens.push(entry.required ? optionText : `[${optionText}]`);
  }
  if (!options.multiline) {
    return `${serverName} ${tool.name} ${tokens.join(" ")}`;
  }
  const lines = [`${serverName} ${tool.name}`, ...tokens.map(token => `  ${token}`)];
  return formatShellMultiline(lines);
}

function renderToolRecommendedExample(serverName, tool, options = {}) {
  const schema = tool?.inputSchema && typeof tool.inputSchema === "object" ? tool.inputSchema : {};
  const properties = schema.properties && typeof schema.properties === "object" ? schema.properties : {};
  const required = collectRecommendedKeys(schema, properties);
  const selectedKeys = new Set(required);
  Object.keys(properties).forEach(key => {
    if (required.has(key) || key === "rationale" || key === "confidence") {
      selectedKeys.add(key);
    }
  });
  if (!selectedKeys.size) {
    return `${serverName} ${tool.name}`;
  }

  const orderedKeys = Array.from(selectedKeys).sort((a, b) => {
    const aRequired = required.has(a);
    const bRequired = required.has(b);
    if (aRequired !== bRequired) {
      return aRequired ? -1 : 1;
    }
    return a.localeCompare(b);
  });

  if (shouldUseJsonMode(schema)) {
    const payload = {};
    for (const key of orderedKeys) {
      payload[key] = exampleValueForKey(key, properties[key]);
    }
    return `${serverName} ${tool.name} '${JSON.stringify(payload, null, 2)}'`;
  }

  const segments = [];
  for (const key of orderedKeys) {
    const value = exampleValueForKey(key, properties[key]);
    const encoded = typeof value === "string" ? `"${String(value).replace(/"/g, '\\"')}"` : String(value);
    segments.push(`--${key} ${encoded}`);
  }
  if (!options.multiline) {
    return `${serverName} ${tool.name} ${segments.join(" ")}`;
  }
  const lines = [`${serverName} ${tool.name}`, ...segments.map(segment => `  ${segment}`)];
  return formatShellMultiline(lines);
}

function renderSafeOutputsPromptDocs(serverName, tools) {
  const lines = [`- \`${serverName}\` — schema-derived command syntax (from final input schemas):`];
  for (const tool of tools) {
    lines.push(`  - \`${tool.name}\``);
    lines.push("    Signature:");
    lines.push("    ```bash");
    lines.push(`    ${renderToolSignature(serverName, tool, { multiline: true }).replace(/\n/g, "\n    ")}`);
    lines.push("    ```");
    lines.push("    Recommended:");
    lines.push("    ```bash");
    lines.push(`    ${renderToolRecommendedExample(serverName, tool, { multiline: true }).replace(/\n/g, "\n    ")}`);
    lines.push("    ```");
  }
  return lines.join("\n");
}

module.exports = {
  summarizeHelpText,
  renderToolSignature,
  renderToolRecommendedExample,
  renderSafeOutputsPromptDocs,
};
