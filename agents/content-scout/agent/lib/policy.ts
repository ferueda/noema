import type { GatewayProviderOptions } from "@ai-sdk/gateway";
import { z } from "zod";

export const CONTENT_SCOUT_MODEL_ID = "openai/gpt-5.4-mini";
export const CONTENT_SCOUT_MODEL_CONTEXT_TOKENS = 400_000;
export const CONTENT_SCOUT_MAX_INPUT_TOKENS = 8_192;
export const CONTENT_SCOUT_TEMPERATURE = 0;
export const CONTENT_SCOUT_MAX_OUTPUT_TOKENS = 4_096;

export const CONTENT_SCOUT_GATEWAY_OPTIONS = {
  disallowPromptTraining: false,
  only: ["azure"],
  order: ["azure"],
  serviceTier: "flex",
  zeroDataRetention: false,
} satisfies GatewayProviderOptions;

export const CONTENT_SCOUT_SESSION_LIMITS = {
  maxInputTokensPerSession: CONTENT_SCOUT_MAX_INPUT_TOKENS,
  maxOutputTokensPerSession: CONTENT_SCOUT_MAX_OUTPUT_TOKENS,
} as const;

export const CONTENT_SCOUT_DISABLED_TOOLS = [
  "agent",
  "ask_question",
  "bash",
  "glob",
  "grep",
  "read_file",
  "todo",
  "web_fetch",
  "web_search",
  "write_file",
] as const;

export const CONTENT_SCOUT_TELEMETRY = {
  recordInputs: false,
  recordOutputs: false,
} as const;

export const CONTENT_SCOUT_ROUTE_USERNAME = "noema";

const routePasswordSchema = z
  .string()
  .refine((value) => value.trim().length > 0);

export function loadRouteCredentials(
  env: NodeJS.ProcessEnv = process.env,
): { password: string; username: typeof CONTENT_SCOUT_ROUTE_USERNAME } {
  const parsed = routePasswordSchema.safeParse(env.NOEMA_EVE_ROUTE_PASSWORD);
  if (!parsed.success) {
    throw new Error("NOEMA_EVE_ROUTE_PASSWORD must be set to a non-empty value");
  }

  return {
    password: parsed.data,
    username: CONTENT_SCOUT_ROUTE_USERNAME,
  };
}
