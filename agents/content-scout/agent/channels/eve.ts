import { httpBasic } from "eve/channels/auth";
import { eveChannel } from "eve/channels/eve";

import { loadRouteCredentials } from "../lib/policy.ts";

export default eveChannel({
  auth: httpBasic(loadRouteCredentials(), {
    realm: "noema-content-scout",
  }),
  uploadPolicy: "disabled",
});
