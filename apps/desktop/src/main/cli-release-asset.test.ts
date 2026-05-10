import { describe, expect, it } from "vitest";

import { selectPlatformReleaseAssetName } from "./cli-release-asset";

describe("selectPlatformReleaseAssetName", () => {
  it("prefers polybet_<os>_<arch> when present", () => {
    const assetNames = [
      "checksums.txt",
      "polybet_darwin_amd64.tar.gz",
      "polybet-cli-1.2.3-darwin-amd64.tar.gz",
    ];

    expect(selectPlatformReleaseAssetName(assetNames, "darwin", "x64")).toBe(
      "polybet_darwin_amd64.tar.gz",
    );
  });

  it("falls back to polybet-cli-* when only versioned archives exist", () => {
    const assetNames = [
      "checksums.txt",
      "polybet-cli-1.2.3-darwin-amd64.tar.gz",
    ];

    expect(selectPlatformReleaseAssetName(assetNames, "darwin", "x64")).toBe(
      "polybet-cli-1.2.3-darwin-amd64.tar.gz",
    );
  });

  it("falls back to multica_* when only legacy archives exist", () => {
    const assetNames = ["checksums.txt", "multica_darwin_amd64.tar.gz"];

    expect(selectPlatformReleaseAssetName(assetNames, "darwin", "x64")).toBe(
      "multica_darwin_amd64.tar.gz",
    );
  });

  it("matches polybet_darwin_arm64 when that is the only darwin arm asset", () => {
    const assetNames = [
      "checksums.txt",
      "polybet_darwin_arm64.tar.gz",
      "polybet_linux_amd64.tar.gz",
    ];

    expect(selectPlatformReleaseAssetName(assetNames, "darwin", "arm64")).toBe(
      "polybet_darwin_arm64.tar.gz",
    );
  });

  it("matches the windows zip archive", () => {
    const assetNames = [
      "polybet_windows_amd64.zip",
      "polybet_linux_amd64.tar.gz",
    ];

    expect(selectPlatformReleaseAssetName(assetNames, "win32", "x64")).toBe(
      "polybet_windows_amd64.zip",
    );
  });

  it("fails when the current platform asset is missing", () => {
    expect(() =>
      selectPlatformReleaseAssetName(
        ["polybet_linux_amd64.tar.gz", "multica_linux_amd64.tar.gz"],
        "darwin",
        "arm64",
      ),
    ).toThrow(/no release asset found/);
  });
});
