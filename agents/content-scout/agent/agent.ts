import { defineAgent } from "eve";

import { contentScoutModel } from "./lib/model.ts";
import {
  CONTENT_SCOUT_GATEWAY_OPTIONS,
  CONTENT_SCOUT_MODEL_CONTEXT_TOKENS,
  CONTENT_SCOUT_SESSION_LIMITS,
} from "./lib/policy.ts";

export const contentScoutAgent = defineAgent({
  description:
    "Finds evidence-backed content ideas in one prepared Noema analysis.",
  limits: CONTENT_SCOUT_SESSION_LIMITS,
  model: contentScoutModel,
  modelContextWindowTokens: CONTENT_SCOUT_MODEL_CONTEXT_TOKENS,
  modelOptions: {
    providerOptions: {
      gateway: CONTENT_SCOUT_GATEWAY_OPTIONS,
    },
  },
});

export default contentScoutAgent;
