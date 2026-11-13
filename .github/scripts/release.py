#!/usr/bin/env python3
"""Select release versions; GoReleaser owns building and uploading artifacts."""
import json
import os
import re
import subprocess
import urllib.request

VERSION = re.compile(r"^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
RELEASER = re.compile(r"^  GORELEASER_VERSION: (v\d+\.\d+\.\d+)\s*$", re.M)
IGNORED = {"README.adoc", "README.md", "CLAUDE.md", "AGENTS.md", ".gitignore", "renovate.json", ".golangci.yml"}


def github(path, data=None):
    repo = os.environ["GITHUB_REPOSITORY"]
    if repo != "StackVista/stackstate-backup-cli":
        raise ValueError("Release operations are restricted to StackVista/stackstate-backup-cli")
    request = urllib.request.Request(f"https://api.github.com/repos/{repo}/{path}",
        data=json.dumps(data).encode() if data is not None else None,
        headers={"Authorization": f"Bearer {os.environ['GITHUB_TOKEN']}", "Accept": "application/vnd.github+json", "Content-Type": "application/json"},
        method="POST" if data is not None else "GET")
    with urllib.request.urlopen(request, timeout=60) as response:
        return json.load(response)


def releases():
    result = []
    for page in range(1, 1000):
        batch = github(f"releases?per_page=100&page={page}")
        result.extend(batch)
        if len(batch) < 100:
            return result
    raise ValueError("Release pagination limit exceeded")


def git(*args):
    return subprocess.check_output(["git", *args], text=True).strip()


def file_at(ref, path):
    result = subprocess.run(["git", "show", f"{ref}:{path}"], capture_output=True, text=True)
    return result.stdout if result.returncode == 0 else ""


def version(tag):
    match = VERSION.fullmatch(tag)
    if not match:
        raise ValueError(f"Invalid release version: {tag}")
    return tuple(map(int, match.groups()))


def module_behavior(text):
    text = re.sub(r"//[^\n]*", "", text)
    text = re.sub(r"(?m)^[ \t]*(?:require|replace|exclude)[ \t]+\([^)]*\)", "", text)
    text = re.sub(r"(?m)^[ \t]*(?:go|toolchain|require|replace|exclude)[ \t]+[^\n]*", "", text)
    return text.split()


def classify(base, candidate):
    maintenance, application = False, []
    paths = subprocess.check_output(["git", "diff", "--no-renames", "--name-only", "-z", base, candidate]).decode().split("\0")
    commands = lambda text: re.findall(r"^.*\b(?:goreleaser\s+(?:build|release)|go\s+(?:build|generate)|args:\s*(?:build|release))\b.*$",
        re.sub(r"\\\n[ \t]*", " ", text), re.M)
    for path in filter(None, paths):
        if path in {"go.mod", "go.sum"}:
            if path == "go.mod" and module_behavior(file_at(base, path)) != module_behavior(file_at(candidate, path)):
                application.append(path)
            else:
                maintenance = True
        elif path in {".github/workflows/ci.yml", ".github/workflows/release.yml"}:
            before, after = file_at(base, path), file_at(candidate, path)
            if commands(before) != commands(after):
                application.append(path)
            elif RELEASER.findall(before) != RELEASER.findall(after):
                maintenance = True
        elif path in IGNORED or path.startswith(("docs/", ".github/")) or ("/" not in path and path.endswith(".md")):
            continue
        else:
            application.append(path)
    return maintenance, application


def report(message, **outputs):
    print(message)
    if os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as stream:
            stream.write(message + "\n\n")
    if os.environ.get("GITHUB_OUTPUT"):
        with open(os.environ["GITHUB_OUTPUT"], "a") as stream:
            for key, value in outputs.items():
                if "\n" in str(value):
                    raise ValueError("Multiline workflow output rejected")
                stream.write(f"{key}={value}\n")


def require_ci(commit):
    runs = github(f"actions/workflows/ci.yml/runs?head_sha={commit}&event=push&per_page=100")["workflow_runs"]
    latest = next((run for run in runs if run["head_sha"] == commit), None)
    if not latest or latest["status"] != "completed" or latest["conclusion"] != "success":
        raise ValueError(f"Main CI must finish successfully for {commit} before releasing")


def plan():
    if os.environ["GITHUB_REF"] != "refs/heads/main":
        raise ValueError("Releases must be requested from main")
    entries = releases()
    published = [entry for entry in entries if not entry["draft"] and not entry["prerelease"] and VERSION.fullmatch(entry["tag_name"])]
    base = max(published, key=lambda entry: version(entry["tag_name"]))["tag_name"]
    operation = os.environ.get("RELEASE_OPERATION") or "automatic"
    tags = [tag for tag in git("tag", "--list").splitlines() if VERSION.fullmatch(tag)]
    pending = sorted({tag for tag in tags + [entry["tag_name"] for entry in entries if entry["draft"]]
        if VERSION.fullmatch(tag) and version(tag) > version(base)}, key=version)
    if operation == "retry":
        tag = os.environ["RELEASE_VERSION"]
        version(tag)
        if tag not in tags or tag not in pending or any(entry["tag_name"] == tag and not entry["draft"] for entry in entries):
            raise ValueError("Retry requires an existing unpublished tag newer than the last release")
        commit = git("rev-parse", f"{tag}^{{commit}}")
        if re.search(r"^  draft: true$", file_at(commit, ".goreleaser.yml"), re.M):
            raise ValueError("Legacy tags need manual recovery; their configuration requires separate draft publication")
    else:
        if pending:
            report(f"Unfinished releases: {', '.join(pending)}. Retry or manually clean up before releasing.", eligible="false")
            return
        commit = os.environ.get("RELEASE_COMMIT") if operation == "automatic" else git("rev-parse", "origin/main")
        if not commit or not re.fullmatch(r"[a-f0-9]{40}", commit) or subprocess.run(["git", "merge-base", "--is-ancestor", base, commit]).returncode:
            report("Candidate precedes or diverges from the published release; skipping.", eligible="false")
            return
        maintenance, application = classify(base, commit)
        if application and operation == "automatic":
            report(f"Manual release required: {', '.join(application)}.", eligible="false")
            return
        if not maintenance and not application:
            report("No releasable changes since the last published release.", eligible="false")
            return
        parts = list(version(base))
        bump = os.environ.get("RELEASE_BUMP") or "patch"
        index = {"major": 0, "minor": 1, "patch": 2}[bump]
        parts[index] += 1
        parts[index + 1:] = [0] * (2 - index)
        tag = "v" + ".".join(map(str, parts))
    require_ci(commit)
    pins = RELEASER.findall(file_at(commit, ".github/workflows/ci.yml"))
    if len(pins) != 1:
        raise ValueError("CI must declare one exact GORELEASER_VERSION")
    report(f"Selected {tag} at {commit}.", eligible="true", tag=tag, commit=commit, releaser=pins[0], base=base)


def tag_commit():
    tag, commit = os.environ["RELEASE_VERSION"], os.environ["RELEASE_COMMIT"]
    version(tag)
    local_tag = subprocess.run(["git", "show-ref", "--verify", "--quiet", f"refs/tags/{tag}"]).returncode == 0
    if not local_tag:
        github("git/refs", {"ref": f"refs/tags/{tag}", "sha": commit})
        git("tag", tag, commit)


if __name__ == "__main__":
    {"plan": plan, "tag": tag_commit}[__import__("sys").argv[1]]()
