import { readFile, readdir, stat } from "node:fs/promises";

import { verifyHttpBasic } from "eve/channels/auth";
import { describe, expect, it } from "vitest";

import { contentScoutAgent } from "../agent/agent.ts";
import instrumentation from "../agent/instrumentation.ts";
import {
  contentScoutModel,
  withContentScoutModelDefaults,
} from "../agent/lib/model.ts";
import {
  CONTENT_SCOUT_DISABLED_TOOLS,
  CONTENT_SCOUT_GATEWAY_OPTIONS,
  CONTENT_SCOUT_MAX_INPUT_TOKENS,
  CONTENT_SCOUT_MAX_OUTPUT_TOKENS,
  CONTENT_SCOUT_MODEL_CONTEXT_TOKENS,
  CONTENT_SCOUT_MODEL_ID,
  CONTENT_SCOUT_ROUTE_USERNAME,
  CONTENT_SCOUT_SESSION_LIMITS,
  CONTENT_SCOUT_TELEMETRY,
  CONTENT_SCOUT_TEMPERATURE,
  loadRouteCredentials,
} from "../agent/lib/policy.ts";

const AGENT_ROOT = new URL("../agent/", import.meta.url);
const PACKAGE_ROOT = new URL("../", import.meta.url);

describe("Content Scout policy", () => {
  it("pins the reviewed Node, npm, and Eve versions", async () => {
    const packageJson = JSON.parse(
      await readFile(new URL("package.json", PACKAGE_ROOT), "utf8"),
    ) as {
      dependencies?: Record<string, string>;
      engines?: Record<string, string>;
      packageManager?: string;
      scripts?: Record<string, string>;
    };
    const npmConfiguration = await readFile(
      new URL(".npmrc", PACKAGE_ROOT),
      "utf8",
    );

    expect(packageJson.engines).toEqual({
      node: "24.18.0",
      npm: "11.16.0",
    });
    expect(packageJson.packageManager).toBe("npm@11.16.0");
    expect(packageJson.dependencies?.eve).toBe("0.27.8");
    expect(packageJson.scripts?.dev).toBe("eve dev --host 127.0.0.1");
    expect(packageJson.scripts?.start).toBe("eve start --host 127.0.0.1");
    expect(npmConfiguration).toContain("engine-strict=true");
  });

  it("pins the reviewed model route and session limits", () => {
    expect(CONTENT_SCOUT_MODEL_ID).toBe("openai/gpt-5.4-mini");
    expect(CONTENT_SCOUT_GATEWAY_OPTIONS).toEqual({
      disallowPromptTraining: false,
      only: ["azure"],
      order: ["azure"],
      serviceTier: "flex",
      zeroDataRetention: false,
    });
    expect(CONTENT_SCOUT_GATEWAY_OPTIONS).not.toHaveProperty("models");
    expect(CONTENT_SCOUT_TEMPERATURE).toBe(0);
    expect(CONTENT_SCOUT_MAX_INPUT_TOKENS).toBe(8_192);
    expect(CONTENT_SCOUT_MAX_OUTPUT_TOKENS).toBe(4_096);
    expect(CONTENT_SCOUT_SESSION_LIMITS).toEqual({
      maxInputTokensPerSession: 8_192,
      maxOutputTokensPerSession: 4_096,
    });
    expect(CONTENT_SCOUT_MODEL_CONTEXT_TOKENS).toBe(400_000);

    expect(contentScoutAgent.model).toBe(contentScoutModel);
    expect(contentScoutModel.provider).toBe("gateway");
    expect(contentScoutModel.modelId).toBe("openai/gpt-5.4-mini");
    expect(contentScoutAgent.modelContextWindowTokens).toBe(400_000);
    expect(contentScoutAgent.modelOptions).toEqual({
      providerOptions: {
        gateway: CONTENT_SCOUT_GATEWAY_OPTIONS,
      },
    });
    expect(contentScoutAgent.limits).toBe(CONTENT_SCOUT_SESSION_LIMITS);
    expect(contentScoutAgent).not.toHaveProperty("outputSchema");
    expect(contentScoutAgent).not.toHaveProperty("experimental");
    expect(contentScoutAgent).not.toHaveProperty("compaction");
  });

  it("applies the reviewed call defaults in the model wrapper", async () => {
    const calls: Array<{ maxOutputTokens?: number; temperature?: number }> = [];
    const stopAfterCapture = new Error("captured model call");
    const innerModel = {
      doGenerate: async (options) => {
        calls.push(options);
        throw stopAfterCapture;
      },
      doStream: async () => {
        throw new Error("unexpected stream");
      },
      modelId: "test-model",
      provider: "test-provider",
      specificationVersion: "v4",
      supportedUrls: {},
    } satisfies Parameters<typeof withContentScoutModelDefaults>[0];
    const wrappedModel = withContentScoutModelDefaults(innerModel);

    await expect(wrappedModel.doGenerate({ prompt: [] })).rejects.toBe(
      stopAfterCapture,
    );
    expect(calls).toEqual([
      {
        maxOutputTokens: 4_096,
        prompt: [],
        temperature: 0,
      },
    ]);
  });

  it("keeps telemetry payload recording off", () => {
    expect(CONTENT_SCOUT_TELEMETRY).toEqual({
      recordInputs: false,
      recordOutputs: false,
    });
    expect(instrumentation).toBe(CONTENT_SCOUT_TELEMETRY);
  });

  it("requires a non-empty route password without returning its value", () => {
    expect(() => loadRouteCredentials({})).toThrowError(
      "NOEMA_EVE_ROUTE_PASSWORD must be set to a non-empty value",
    );
    expect(() =>
      loadRouteCredentials({ NOEMA_EVE_ROUTE_PASSWORD: "   " }),
    ).toThrowError("NOEMA_EVE_ROUTE_PASSWORD must be set to a non-empty value");

    const credentials = loadRouteCredentials({
      NOEMA_EVE_ROUTE_PASSWORD: "test-secret",
    });
    expect(credentials).toEqual({
      password: "test-secret",
      username: CONTENT_SCOUT_ROUTE_USERNAME,
    });
    expect(
      verifyHttpBasic(
        `Basic ${Buffer.from("noema:test-secret").toString("base64")}`,
        credentials,
      ).ok,
    ).toBe(true);
  });

  it("disables every reviewed framework tool and declares no extra assets", async () => {
    const toolFiles = (await readdir(new URL("tools/", AGENT_ROOT)))
      .filter((name) => name.endsWith(".ts"))
      .map((name) => name.slice(0, -3))
      .sort();

    expect(toolFiles).toEqual([...CONTENT_SCOUT_DISABLED_TOOLS]);

    for (const forbiddenPath of [
      "connections",
      "hooks",
      "sandbox",
      "schedules",
      "skills",
      "state",
      "subagents",
    ]) {
      await expect(stat(new URL(`${forbiddenPath}/`, AGENT_ROOT))).rejects.toThrow();
    }
  });
});
