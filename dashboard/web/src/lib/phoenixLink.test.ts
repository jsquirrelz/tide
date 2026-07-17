import { describe, it, expect } from "vitest";

import { phoenixTraceURL, phoenixSpanURL } from "./phoenixLink";

describe("phoenixTraceURL / phoenixSpanURL (plan 46-05, D-11/D-12)", () => {
  it("phoenixTraceURL builds the /redirects/traces/{id} shape", () => {
    expect(phoenixTraceURL("http://phoenix:6006", "abc123")).toBe(
      "http://phoenix:6006/redirects/traces/abc123",
    );
  });

  it("phoenixSpanURL builds the /redirects/spans/{id} shape", () => {
    expect(phoenixSpanURL("http://phoenix:6006", "def456")).toBe(
      "http://phoenix:6006/redirects/spans/def456",
    );
  });

  it("strips exactly one trailing slash from baseURL (trace)", () => {
    expect(phoenixTraceURL("http://phoenix:6006/", "abc123")).toBe(
      phoenixTraceURL("http://phoenix:6006", "abc123"),
    );
  });

  it("strips exactly one trailing slash from baseURL (span)", () => {
    expect(phoenixSpanURL("http://phoenix:6006/", "def456")).toBe(
      phoenixSpanURL("http://phoenix:6006", "def456"),
    );
  });

  it("encodeURIComponent-encodes the interpolated trace ID", () => {
    expect(phoenixTraceURL("http://phoenix:6006", "a/b")).toBe(
      "http://phoenix:6006/redirects/traces/a%2Fb",
    );
  });

  it("encodeURIComponent-encodes the interpolated span ID", () => {
    expect(phoenixSpanURL("http://phoenix:6006", "a/b")).toBe(
      "http://phoenix:6006/redirects/spans/a%2Fb",
    );
  });
});
