export default {
  extends: ["@commitlint/config-conventional"],
  rules: {
    "scope-enum": [
      2,
      "always",
      [
        "server",
        "dashboard",
        "desktop",
        "core",
        "ui",
        "views",
        "types",
        "e2e",
        "deps",
        "ci",
        "docs",
        "config",
      ],
    ],
    "type-enum": [
      2,
      "always",
      ["feat", "fix", "refactor", "test", "docs", "chore", "perf", "ci"],
    ],
    "subject-case": [2, "never", ["sentence-case", "start-case", "pascal-case", "upper-case"]],
  },
};
