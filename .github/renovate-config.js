"use strict";
// https://github.com/renovatebot/github-action/blob/main/.github/renovate.json
// https://docs.renovatebot.com/configuration-options/

module.exports = {
  "extends": [
    ":disableRateLimiting",
    ":semanticCommits",
    "helpers:pinGitHubActionDigests", // pin GitHub Actions to full SHAs
  ],
  "onboarding": false,
  "platform": "github",
  "repositories": [
    "kubernetes-sigs/mcp-lifecycle-operator",
  ],
  "prConcurrentLimit": 0,
  "prHourlyLimit": 0,
  "minimumReleaseAge": "3 days",
  "pruneStaleBranches": true,
  "dependencyDashboard": false,
  "requireConfig": "optional",
  "rebaseWhen": "behind-base-branch",
  "baseBranchPatterns": ["main"],
  "recreateWhen": "always",
  "labels": ["dependencies"],
  "addLabels": ["renovate-bot"],
  "enabledManagers": [
    "gomod",
    "dockerfile",
    "github-actions",
  ],
  "postUpdateOptions": [
    "gomodTidy",        // run go mod tidy after updating go.mod
    "gomodUpdateImportPaths", // update import paths on major updates
  ],
  "packageRules": [
    // Group all k8s.io/* dependencies together
    {
      "matchManagers": ["gomod"],
      "groupName": "k8s.io dependencies",
      "matchPackageNames": ["k8s.io/{/,}**"],
    },
    // Group sigs.k8s.io/* dependencies together
    {
      "matchManagers": ["gomod"],
      "groupName": "sigs.k8s.io dependencies",
      "matchPackageNames": ["sigs.k8s.io/{/,}**"],
    },
    // Keep the preferred Go toolchain and Dockerfile image in sync
    {
      "matchManagers": ["dockerfile"],
      "matchPackageNames": ["golang"],
      "groupName": "go version",
    },
    // Keep go.mod's toolchain directive grouped with Dockerfile golang updates
    {
      "matchManagers": ["gomod"],
      "matchDepNames": ["go"],
      "matchDepTypes": ["toolchain"],
      "groupName": "go version",
    },
    // Only bump the minimum supported Go version when adopting a new minor
    {
      "matchManagers": ["gomod"],
      "matchDepNames": ["go"],
      "matchDepTypes": ["golang"],
      "matchUpdateTypes": ["minor"],
      "rangeStrategy": "bump",
      "groupName": "go version",
    },
    // Group GitHub Actions updates together
    {
      "matchManagers": ["github-actions"],
      "groupName": "github actions",
    },
  ],
};
