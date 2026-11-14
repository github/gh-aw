const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { _resetModelCostsCache } = require("../model_costs.cjs");

/** Install only the model metadata needed by a test; return its cleanup function. */
function useModelOverrides(providers) {
  const previous = process.env.GH_AW_MODELS_JSON_PATH;
  let directory;
  try {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-model-overrides-"));
    const file = path.join(directory, "models.json");
    fs.writeFileSync(file, JSON.stringify({ providers }));
    process.env.GH_AW_MODELS_JSON_PATH = file;
  } catch (error) {
    throw new Error("Failed to create explicit model overrides fixture", { cause: error });
  }
  _resetModelCostsCache();
  return () => {
    if (previous === undefined) delete process.env.GH_AW_MODELS_JSON_PATH;
    else process.env.GH_AW_MODELS_JSON_PATH = previous;
    _resetModelCostsCache();
    try {
      fs.rmSync(directory, { recursive: true, force: true });
    } catch (error) {
      throw new Error("Failed to remove explicit model overrides fixture", { cause: error });
    }
  };
}

module.exports = { useModelOverrides };
