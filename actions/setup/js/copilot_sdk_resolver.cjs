"use strict";

const { register, registerHooks, createRequire } = require("node:module");
const { pathToFileURL } = require("node:url");

const sdkParentURL = pathToFileURL(__filename).href;
const sdkPackage = require(require("node:path").join(__dirname, "package.json"));
const packages = Object.keys(sdkPackage.dependencies);

function isModuleNotFound(error) {
  return error !== null && typeof error === "object" && "code" in error && typeof error.code === "string" && ["MODULE_NOT_FOUND", "ERR_MODULE_NOT_FOUND"].includes(error.code);
}

function resolve(specifier, context, nextResolve) {
  if (context.parentURL !== sdkParentURL && packages.some(pkg => specifier === pkg || specifier.startsWith(`${pkg}/`))) {
    try {
      return { url: pathToFileURL(createRequire(sdkParentURL).resolve(specifier)).href, shortCircuit: true };
    } catch (error) {
      if (!isModuleNotFound(error)) throw error;
    }
  }
  return nextResolve(specifier, context);
}

if (registerHooks) {
  registerHooks({ resolve });
} else {
  const Module = require("node:module");
  const originalResolve = Reflect.get(Module, "_resolveFilename");
  Reflect.set(Module, "_resolveFilename", function (specifier, parent, ...args) {
    if (parent?.filename !== __filename && packages.some(pkg => specifier === pkg || specifier.startsWith(`${pkg}/`))) {
      try {
        return createRequire(sdkParentURL).resolve(specifier);
      } catch (error) {
        if (!isModuleNotFound(error)) throw error;
      }
    }
    return originalResolve.call(Module, specifier, parent, ...args);
  });

  register(
    "data:text/javascript," +
      encodeURIComponent(
        `import { createRequire } from "node:module"; import { pathToFileURL } from "node:url";` +
          `const packages = ${JSON.stringify(packages)};` +
          `const sdkParentURL = ${JSON.stringify(sdkParentURL)};` +
          `const isModuleNotFound = ${isModuleNotFound.toString()}; export ${resolve.toString()}`
      )
  );
}
