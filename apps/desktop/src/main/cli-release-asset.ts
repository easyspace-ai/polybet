const VERSIONED_CLI_PREFIX = "polybet-cli-";

function platformArchiveDescriptor(
  platform: NodeJS.Platform = process.platform,
  arch: string = process.arch,
): { os: string; arch: string; ext: string } {
  const osMap: Record<string, string> = {
    darwin: "darwin",
    linux: "linux",
    win32: "windows",
  };
  const archMap: Record<string, string> = {
    x64: "amd64",
    arm64: "arm64",
  };
  const os = osMap[platform];
  const mappedArch = archMap[arch];
  if (!os || !mappedArch) {
    throw new Error(
      `unsupported platform for CLI auto-install: ${platform}/${arch}`,
    );
  }
  const ext = platform === "win32" ? "zip" : "tar.gz";
  return { os, arch: mappedArch, ext };
}

export function selectPlatformReleaseAssetName(
  assetNames: Iterable<string>,
  platform: NodeJS.Platform = process.platform,
  arch: string = process.arch,
): string {
  const { os, arch: mappedArch, ext } = platformArchiveDescriptor(
    platform,
    arch,
  );
  const names = [...assetNames];

  const canonicalName = `polybet_${os}_${mappedArch}.${ext}`;
  if (names.includes(canonicalName)) {
    return canonicalName;
  }

  // Older releases shipped `polybet-cli-<semver>-<os>-<arch>.<ext>`.
  const versionedSuffix = `-${os}-${mappedArch}.${ext}`;
  const versionedMatches = names.filter(
    (name) =>
      name.startsWith(VERSIONED_CLI_PREFIX) && name.endsWith(versionedSuffix),
  );
  if (versionedMatches.length === 1) {
    return versionedMatches[0];
  }
  if (versionedMatches.length > 1) {
    throw new Error(
      `multiple release assets matched current platform ${versionedSuffix}: ${versionedMatches.join(", ")}`,
    );
  }

  const multicaLegacy = `multica_${os}_${mappedArch}.${ext}`;
  if (names.includes(multicaLegacy)) {
    return multicaLegacy;
  }

  throw new Error(
    `no release asset found for current platform (expected ${canonicalName} or older naming)`,
  );
}
