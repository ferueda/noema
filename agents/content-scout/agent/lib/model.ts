import {
  defaultSettingsMiddleware,
  gateway,
  wrapLanguageModel,
} from "ai";

import {
  CONTENT_SCOUT_MAX_OUTPUT_TOKENS,
  CONTENT_SCOUT_MODEL_ID,
  CONTENT_SCOUT_TEMPERATURE,
} from "./policy.ts";

type WrappableLanguageModel = Parameters<typeof wrapLanguageModel>[0]["model"];

export function withContentScoutModelDefaults(model: WrappableLanguageModel) {
  return wrapLanguageModel({
    middleware: defaultSettingsMiddleware({
      settings: {
        maxOutputTokens: CONTENT_SCOUT_MAX_OUTPUT_TOKENS,
        temperature: CONTENT_SCOUT_TEMPERATURE,
      },
    }),
    model,
  });
}

export const contentScoutModel = withContentScoutModelDefaults(
  gateway(CONTENT_SCOUT_MODEL_ID),
);
