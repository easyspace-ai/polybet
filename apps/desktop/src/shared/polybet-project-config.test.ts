import { describe, expect, it } from "vitest";
import {
  applyPolybetProjectConfigToEnv,
  defaultPolybetProjectConfig,
  migrateRelativeFileDatabaseUrlToHomeEmbedded,
  parsePolybetProjectConfig,
  sqliteDatabaseUrlUnderDir,
  validatePolybetProjectConfigInput,
} from "./polybet-project-config";

describe("parsePolybetProjectConfig", () => {
  it("parses minimal valid JSON", () => {
    const c = parsePolybetProjectConfig(
      JSON.stringify({
        schemaVersion: 1,
        databaseUrl: "file:./db.sqlite?_pragma=foreign_keys(1)",
      }),
    );
    expect(c.host).toBe("127.0.0.1");
    expect(c.port).toBe("7633");
    expect(c.outboundProxyUrl).toBeUndefined();
  });

  it("accepts numeric port", () => {
    const c = parsePolybetProjectConfig(
      JSON.stringify({
        schemaVersion: 1,
        databaseUrl: "file:./x.db",
        port: 7640,
      }),
    );
    expect(c.port).toBe("7640");
  });

  it("rejects bad databaseUrl scheme", () => {
    expect(() =>
      parsePolybetProjectConfig(
        JSON.stringify({ schemaVersion: 1, databaseUrl: "mysql://x" }),
      ),
    ).toThrow();
  });
});

describe("validatePolybetProjectConfigInput", () => {
  it("returns ok for valid object", () => {
    const r = validatePolybetProjectConfigInput({
      schemaVersion: 1,
      databaseUrl: "file:./a.db",
      outboundProxyUrl: "http://127.0.0.1:1",
    });
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.config.outboundProxyUrl).toBe("http://127.0.0.1:1");
  });

  it("returns error for invalid proxy", () => {
    const r = validatePolybetProjectConfigInput({
      schemaVersion: 1,
      databaseUrl: "file:./a.db",
      outboundProxyUrl: "not-a-url",
    });
    expect(r.ok).toBe(false);
  });
});

describe("sqliteDatabaseUrlUnderDir", () => {
  it("builds absolute file URL with pragma", () => {
    const u = sqliteDatabaseUrlUnderDir("/tmp/polybet-embedded");
    expect(u.startsWith("file:")).toBe(true);
    expect(u).toContain("polybet-embedded");
    expect(u).toContain("_pragma=foreign_keys(1)");
  });
});

describe("migrateRelativeFileDatabaseUrlToHomeEmbedded", () => {
  it("rewrites file:./router.db", () => {
    const next = migrateRelativeFileDatabaseUrlToHomeEmbedded(
      "file:./router.db?_pragma=foreign_keys(1)",
      "/tmp/h",
    );
    expect(next.startsWith("file:")).toBe(true);
    expect(next).toContain("/tmp/h");
    expect(next).toContain("router.db");
  });

  it("leaves absolute URLs unchanged", () => {
    const abs = "file:/var/data/x.db?_pragma=foreign_keys(1)";
    expect(migrateRelativeFileDatabaseUrlToHomeEmbedded(abs, "/tmp/h")).toBe(
      abs,
    );
  });
});

describe("defaultPolybetProjectConfig", () => {
  it("dev requires embedded dir", () => {
    const c = defaultPolybetProjectConfig("dev", {
      devEmbeddedDataDir: "/tmp/embed",
    });
    expect(c.databaseUrl).toContain("/tmp/embed");
    expect(c.databaseUrl).toContain("router.db");
  });
});

describe("applyPolybetProjectConfigToEnv", () => {
  it("sets proxy and clears SPORTS_ROUTER_ENV_FILE", () => {
    const env = applyPolybetProjectConfigToEnv(
      { ...process.env, SPORTS_ROUTER_ENV_FILE: "/tmp/x" },
      {
        schemaVersion: 1,
        databaseUrl: "file:./db",
        host: "127.0.0.1",
        port: "7633",
        outboundProxyUrl: "http://127.0.0.1:9",
        readOnlyMode: false,
        logLevel: "warn",
      },
    );
    expect(env.DATABASE_URL).toBe("file:./db");
    expect(env.HTTP_PLATFORM_PROXY_URL).toBe("http://127.0.0.1:9");
    expect(env.SPORTS_ROUTER_ENV_FILE).toBeUndefined();
    expect(env.LOG_LEVEL).toBe("warn");
  });
});
