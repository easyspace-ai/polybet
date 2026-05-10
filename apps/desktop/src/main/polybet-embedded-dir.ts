import { join } from "node:path";
import { getUserProfileHomeDir } from "./user-profile-paths";

/** Dev + local data: SQLite and process cwd for the embedded Go server. */
export function getPolybetEmbeddedServerDataDir(): string {
  return join(getUserProfileHomeDir(), ".polybet", "embedded");
}
