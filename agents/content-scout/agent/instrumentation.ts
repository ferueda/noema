import { defineInstrumentation } from "eve/instrumentation";

import { CONTENT_SCOUT_TELEMETRY } from "./lib/policy.ts";

export default defineInstrumentation(CONTENT_SCOUT_TELEMETRY);
