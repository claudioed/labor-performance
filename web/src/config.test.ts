import { describe, expect, it } from "vitest";
import { resolveLaborApiBase } from "./config";

describe("resolveLaborApiBase", () => {
  it("builds the production API base from the runtime API origin", () => {
    expect(resolveLaborApiBase({ apiOrigin: "http://localhost:8000" }, true)).toBe(
      "http://localhost:8000/api/labor-performance",
    );
  });

  it("normalizes a trailing slash on the runtime API origin", () => {
    expect(resolveLaborApiBase({ apiOrigin: "https://warehouse.example/" }, true)).toBe(
      "https://warehouse.example/api/labor-performance",
    );
  });

  it("fails loudly when production runtime configuration has no API origin", () => {
    expect(() => resolveLaborApiBase({}, true)).toThrow(
      "window.__WAREHOUSE_CONFIG__.apiOrigin is required in production",
    );
  });

  it("retains the existing standalone API origin in Vite development", () => {
    expect(resolveLaborApiBase({}, false)).toBe("http://localhost:8088");
  });
});
