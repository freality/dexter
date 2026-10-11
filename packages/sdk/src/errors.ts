export type DexErrorCode =
  | "DUPLICATE_COMMAND"
  | "DUPLICATE_CONFIG"
  | "DUPLICATE_RESOURCE"
  | "INVALID_DEPENDENCY"
  | "INVALID_PLUGIN"
  | "INVALID_SECRET"
  | "INVALID_PROJECT"
  | "INVALID_PROPERTY"
  | "INVALID_RESOURCE_NAME"
  | "INVALID_RESOURCE_TYPE"
  | "MISSING_CONFIG"
  | "MISSING_ENV";

export class DexError extends Error {
  readonly code: DexErrorCode;

  constructor(code: DexErrorCode, message: string) {
    super(message);
    this.name = "DexError";
    this.code = code;
    Object.setPrototypeOf(this, DexError.prototype); // survives downlevel transpilation
  }
}
