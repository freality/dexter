var __defProp = Object.defineProperty;
var __getOwnPropSymbols = Object.getOwnPropertySymbols;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __propIsEnum = Object.prototype.propertyIsEnumerable;
var __defNormalProp = (obj, key, value) => key in obj ? __defProp(obj, key, { enumerable: true, configurable: true, writable: true, value }) : obj[key] = value;
var __spreadValues = (a, b) => {
  for (var prop in b || (b = {}))
    if (__hasOwnProp.call(b, prop))
      __defNormalProp(a, prop, b[prop]);
  if (__getOwnPropSymbols)
    for (var prop of __getOwnPropSymbols(b)) {
      if (__propIsEnum.call(b, prop))
        __defNormalProp(a, prop, b[prop]);
    }
  return a;
};
var __publicField = (obj, key, value) => __defNormalProp(obj, typeof key !== "symbol" ? key + "" : key, value);

// packages/sdk/dist/errors.js
var DexError = class _DexError extends Error {
  constructor(code, message) {
    super(message);
    __publicField(this, "code");
    this.name = "DexError";
    this.code = code;
    Object.setPrototypeOf(this, _DexError.prototype);
  }
};

// packages/sdk/dist/env.js
var HOST_ENV_GLOBAL = "__dex_env";
function createHostEnvSource(scope = globalThis) {
  return Object.freeze({
    get(key) {
      const vars = scope[HOST_ENV_GLOBAL];
      if (vars === null || typeof vars !== "object")
        return void 0;
      const value = vars[key];
      return typeof value === "string" ? value : void 0;
    }
  });
}
function envFrom(source, key, fallback) {
  const value = source.get(key);
  if (typeof value === "string")
    return value;
  if (fallback !== void 0)
    return fallback;
  throw new DexError("MISSING_ENV", `environment variable "${key}" is not set and no fallback was provided`);
}

// packages/sdk/dist/registry.js
function createResourceRegistry() {
  const nodes = /* @__PURE__ */ new Map();
  return Object.freeze({
    add(node) {
      if (nodes.has(node.name)) {
        throw new DexError("DUPLICATE_RESOURCE", `resource "${node.name}" is already defined`);
      }
      nodes.set(node.name, node);
    },
    has(name) {
      return nodes.has(name);
    },
    all() {
      return Object.freeze([...nodes.values()]);
    }
  });
}

// packages/sdk/dist/properties.js
function isPlainObject(value) {
  if (value === null || typeof value !== "object")
    return false;
  const proto = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}
function sanitizeProperties(resourceName, properties) {
  if (!isPlainObject(properties)) {
    throw new DexError("INVALID_PROPERTY", `resource "${resourceName}" properties must be an object`);
  }
  const ancestors = /* @__PURE__ */ new Set();
  const reject = (path, descriptor) => {
    throw new DexError("INVALID_PROPERTY", `resource "${resourceName}" property "${path}" is not JSON-serializable (${descriptor})`);
  };
  const enter = (path, value) => {
    if (ancestors.has(value)) {
      throw new DexError("INVALID_PROPERTY", `resource "${resourceName}" property "${path}" contains a circular reference`);
    }
    ancestors.add(value);
  };
  const cloneObject = (obj, prefix) => {
    const out = {};
    for (const [key, val] of Object.entries(obj)) {
      if (val === void 0)
        continue;
      out[key] = clone(val, prefix === "" ? key : `${prefix}.${key}`);
    }
    return out;
  };
  const clone = (value, path) => {
    switch (typeof value) {
      case "string":
      case "boolean":
        return value;
      case "number":
        return Number.isFinite(value) ? value : reject(path, "non-finite number");
      case "object": {
        if (value === null)
          return null;
        if (!Array.isArray(value) && !isPlainObject(value))
          return reject(path, "non-plain object");
        enter(path, value);
        const out = Array.isArray(value) ? Array.from(value, (item, i) => clone(item, `${path}[${i}]`)) : cloneObject(value, path);
        ancestors.delete(value);
        return out;
      }
      default:
        return reject(path, typeof value);
    }
  };
  ancestors.add(properties);
  return cloneObject(properties, "");
}

// packages/sdk/dist/resource.js
var RESOURCE_NAME_PATTERN = /^[a-z0-9][a-z0-9._-]*$/;
function defineResource(kind, collector) {
  return (name, spec) => {
    var _a, _b, _c;
    if (typeof name !== "string" || !RESOURCE_NAME_PATTERN.test(name)) {
      throw new DexError("INVALID_RESOURCE_NAME", `invalid resource name "${String(name)}": must match ${String(RESOURCE_NAME_PATTERN)}`);
    }
    if (spec.type !== void 0 && (typeof spec.type !== "string" || spec.type === "")) {
      throw new DexError("INVALID_RESOURCE_TYPE", `resource "${name}" has an invalid type`);
    }
    if (typeof spec.plugin !== "string" || spec.plugin === "") {
      throw new DexError("INVALID_PLUGIN", `resource "${name}" must declare a non-empty plugin`);
    }
    const properties = sanitizeProperties(name, (_a = spec.properties) != null ? _a : {});
    const deps = ((_b = spec.dependsOn) != null ? _b : []).map((entry) => {
      if (typeof entry === "string")
        return entry;
      if (entry !== null && typeof entry === "object" && "name" in entry && typeof entry.name === "string") {
        return entry.name;
      }
      throw new DexError("INVALID_DEPENDENCY", `resource "${name}" has an invalid dependency entry`);
    });
    const node = Object.freeze({
      name,
      type: (_c = spec.type) != null ? _c : kind,
      plugin: spec.plugin,
      properties: Object.freeze(properties),
      // The DAG is a set; sorting keeps the IR deterministic.
      dependsOn: Object.freeze([...new Set(deps)].sort())
    });
    collector.add(node);
    return node;
  };
}

// packages/sdk/dist/synth.js
function buildIR(config, resources) {
  if (typeof config.project !== "string" || config.project === "") {
    throw new DexError("INVALID_PROJECT", "invalid project name: must be a non-empty string");
  }
  const byName = new Map(resources.map((node) => [node.name, node]));
  const out = {};
  for (const name of [...byName.keys()].sort()) {
    const node = byName.get(name);
    for (const target of node.dependsOn) {
      if (target === node.name) {
        throw new DexError("INVALID_DEPENDENCY", `resource "${node.name}" cannot depend on itself`);
      }
      if (!byName.has(target)) {
        throw new DexError("INVALID_DEPENDENCY", `resource "${node.name}" depends on unknown resource "${target}"`);
      }
    }
    out[node.name] = {
      type: node.type,
      plugin: node.plugin,
      properties: node.properties,
      depends_on: [...node.dependsOn]
    };
  }
  return { project: config.project, resources: out };
}
function stableStringify(value) {
  return JSON.stringify(value, (_key, val) => val !== null && typeof val === "object" && !Array.isArray(val) ? Object.fromEntries(Object.keys(val).sort().map((k) => [k, val[k]])) : val);
}

// packages/sdk/dist/scope.js
function createDexScope(options) {
  var _a;
  const registry = createResourceRegistry();
  const envSource = (_a = options == null ? void 0 : options.envSource) != null ? _a : createHostEnvSource();
  let config;
  const synth2 = () => {
    if (config === void 0) {
      throw new DexError("MISSING_CONFIG", "no dex config defined: call defineDexConfig() in dex.config.ts");
    }
    return buildIR(config, registry.all());
  };
  return Object.freeze({
    defineDexConfig(input) {
      if (config !== void 0) {
        throw new DexError("DUPLICATE_CONFIG", "defineDexConfig() was called more than once");
      }
      config = Object.freeze(__spreadValues({}, input));
      return config;
    },
    service: defineResource("container", registry),
    volume: defineResource("volume", registry),
    network: defineResource("network", registry),
    env: (key, fallback) => envFrom(envSource, key, fallback),
    synth: synth2,
    synthJSON: () => stableStringify(synth2())
  });
}

// packages/sdk/dist/index.js
var SYNTH_GLOBAL = "__dex_synth";
var defaultScope = createDexScope();
var defineDexConfig = defaultScope.defineDexConfig;
var service = defaultScope.service;
var volume = defaultScope.volume;
var network = defaultScope.network;
var env = defaultScope.env;
var synth = defaultScope.synth;
globalThis[SYNTH_GLOBAL] = defaultScope.synthJSON;
export {
  DexError,
  SYNTH_GLOBAL,
  defineDexConfig,
  env,
  network,
  service,
  synth,
  volume
};
