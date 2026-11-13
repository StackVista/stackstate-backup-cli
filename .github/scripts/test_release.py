import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("release.py"))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)
RELEASER = "v2.19.0"


class ClassifierTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.previous = Path.cwd()
        os.chdir(self.directory.name)
        self.git("init", "-q")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.write("go.mod", "module example.invalid/tool\ngo 1.26.10\nrequire example.invalid/dependency v1.0.0\n")
        self.write("go.sum", "original sum\n")
        self.write("main.go", "package main\n")
        self.write("Makefile", "build:\n\tgo build\n")
        self.write(".github/workflows/ci.yml", f"env:\n  GORELEASER_VERSION: {RELEASER}\njobs:\n  build:\n    steps:\n      - run: goreleaser release --snapshot --skip=publish\n")
        self.commit()
        self.base = self.tree

    def tearDown(self):
        os.chdir(self.previous)
        self.directory.cleanup()

    def git(self, *args):
        return subprocess.check_output(["git", *args], text=True).strip()

    def write(self, name, content):
        path = Path(name)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)

    def commit(self):
        self.git("add", ".")
        self.tree = self.git("write-tree")

    def classify(self):
        self.commit()
        return release.classify(self.base, self.tree)

    def test_manual_dependency_repair_is_maintenance(self):
        self.write("go.sum", "repaired sum\n")
        self.assertEqual(self.classify(), (True, []))

    def test_unreleased_feature_survives_later_dependency_merge(self):
        self.write("main.go", "package main\nfunc feature() {}\n")
        self.commit()
        self.write("go.sum", "new sum\n")
        self.assertEqual(self.classify(), (True, ["main.go"]))

    def test_completely_reverted_feature_does_not_block(self):
        self.write("main.go", "feature\n")
        self.commit()
        self.write("main.go", "package main\n")
        self.write("go.sum", "new sum\n")
        self.assertEqual(self.classify(), (True, []))

    def test_build_command_changed(self):
        self.write("Makefile", Path("Makefile").read_text().replace("go build", "go build -race"))
        self.assertEqual(self.classify(), (False, ["Makefile"]))

    def test_workflow_releaser_pin_changed(self):
        path = ".github/workflows/ci.yml"
        self.write(path, Path(path).read_text().replace("v2.19.0", "v2.19.1"))
        self.assertEqual(self.classify(), (True, []))

    def test_workflow_build_command_changed(self):
        path = ".github/workflows/ci.yml"
        self.write(path, Path(path).read_text().replace("--snapshot", "--snapshot --single-target"))
        self.assertEqual(self.classify(), (False, [path]))

    def test_continued_build_flags_require_manual_selection(self):
        path = ".github/workflows/release.yml"
        original = "jobs:\n  release:\n    steps:\n      - run: |\n          goreleaser release \\\n            --clean\n"
        self.write(path, original)
        self.commit()
        self.base = self.tree
        self.write(path, original.replace("--clean", "--clean --skip=publish"))
        self.write("go.sum", "new sum\n")
        self.assertEqual(self.classify(), (True, [path]))

    def test_unrelated_workflow_and_docs_do_not_release(self):
        self.write(".github/workflows/renovate.yml", "name: Renovate\n")
        self.write("README.md", "Documentation\n")
        self.assertEqual(self.classify(), (False, []))

    def test_unrelated_ci_command_change_does_not_release(self):
        path = ".github/workflows/ci.yml"
        self.write(path, Path(path).read_text() + "      - run: echo new CI diagnostic\n")
        self.assertEqual(self.classify(), (False, []))

    def test_markdown_in_embedded_assets_is_application_change(self):
        self.write("static/template.md", "embedded text\n")
        self.assertEqual(self.classify(), (False, ["static/template.md"]))

    def test_new_release_build_command_requires_manual_selection(self):
        self.write(".github/workflows/release.yml", "jobs:\n  release:\n    steps:\n      - run: goreleaser release --skip=publish\n")
        self.assertEqual(self.classify(), (False, [".github/workflows/release.yml"]))

    def test_packaging_and_templates_require_manual_selection(self):
        self.write(".goreleaser.yml", "archives: []\n")
        self.write("static/template.txt", "template\n")
        self.assertEqual(self.classify(), (False, [".goreleaser.yml", "static/template.txt"]))

    def test_template_moved_to_docs_still_requires_manual_selection(self):
        self.write("static/template.txt", "template\n")
        self.commit()
        self.base = self.tree
        self.write("docs/template.txt", "template\n")
        Path("static/template.txt").unlink()
        self.write("go.sum", "new sum\n")
        self.assertEqual(self.classify(), (True, ["static/template.txt"]))

    def test_module_rename_is_application_change(self):
        self.write("go.mod", Path("go.mod").read_text().replace("example.invalid/tool", "example.invalid/other"))
        self.assertEqual(self.classify(), (False, ["go.mod"]))

    def test_runtime_defaults_require_manual_selection(self):
        self.write("go.mod", Path("go.mod").read_text() + "godebug panicnil=1\n")
        self.assertEqual(self.classify(), (False, ["go.mod"]))

    def test_runtime_default_block_requires_manual_selection(self):
        self.write("go.mod", Path("go.mod").read_text() + "godebug (\n panicnil=1\n)\n")
        self.assertEqual(self.classify(), (False, ["go.mod"]))

    def test_dependency_and_compiler_directives_are_maintenance(self):
        self.write("go.mod", "module example.invalid/tool\ngo 1.26.11\ntoolchain go1.26.11\n"
            "require (\n example.invalid/dependency v1.0.1 // repaired\n)\n"
            "replace example.invalid/dependency => example.invalid/fork v1.0.1\n"
            "exclude (\n example.invalid/dependency v1.0.0\n)\n")
        self.assertEqual(self.classify(), (True, []))

class SelectionTests(unittest.TestCase):
    def setUp(self):
        from unittest.mock import patch
        self.patches = []
        self.entry = {"tag_name": "v1.2.3", "draft": False, "prerelease": False}
        self.commit = "b" * 40
        self.outputs = {}
        self.calls = []
        self.add(patch.dict(os.environ, {"GITHUB_REF": "refs/heads/main", "RELEASE_OPERATION": "automatic", "RELEASE_COMMIT": self.commit, "RELEASE_BUMP": "patch", "RELEASE_VERSION": "v1.2.4"}))
        self.add(patch.object(release, "releases", return_value=[self.entry]))
        self.add(patch.object(release, "git", side_effect=lambda *args: "v1.2.3" if args[0] == "tag" else self.commit))
        self.add(patch.object(release, "file_at", side_effect=lambda ref, path: "release: {}" if path == ".goreleaser.yml" else f"env:\n  GORELEASER_VERSION: {RELEASER}\n"))
        self.add(patch.object(release.subprocess, "run", return_value=type("Result", (), {"returncode": 0})()))
        self.add(patch.object(release, "classify", return_value=(True, [])))
        self.ci = self.add(patch.object(release, "require_ci"))
        self.add(patch.object(release, "report", side_effect=lambda message, **outputs: self.outputs.update(outputs)))

    def add(self, patcher):
        self.patches.append(patcher)
        return patcher.start()

    def tearDown(self):
        for patcher in reversed(self.patches):
            patcher.stop()

    def test_automatic_patch_selection(self):
        release.plan()
        self.assertEqual(self.outputs["tag"], "v1.2.4")
        self.assertEqual(self.outputs["commit"], self.commit)
        self.assertEqual(self.outputs["releaser"], "v2.19.0")
        self.ci.assert_called_once_with(self.commit)

    def test_unreleased_application_change_needs_manual_selection(self):
        from unittest.mock import patch
        with patch.object(release, "classify", return_value=(True, ["main.go"])):
            release.plan()
        self.assertEqual(self.outputs["eligible"], "false")

    def test_manual_major_selection(self):
        os.environ.update(RELEASE_OPERATION="release", RELEASE_BUMP="major")
        release.plan()
        self.assertEqual(self.outputs["tag"], "v2.0.0")

    def test_manual_minor_selection_includes_application_changes(self):
        from unittest.mock import patch
        os.environ.update(RELEASE_OPERATION="release", RELEASE_BUMP="minor")
        with patch.object(release, "classify", return_value=(False, ["main.go"])):
            release.plan()
        self.assertEqual(self.outputs["tag"], "v1.3.0")

    def test_missing_or_ambiguous_releaser_pin_blocks_publication(self):
        from unittest.mock import patch
        for config in ("env: {}", "  GORELEASER_VERSION: latest", f"  GORELEASER_VERSION: {RELEASER}\n  GORELEASER_VERSION: v2.20.0"):
            with self.subTest(config=config), patch.object(release, "file_at", return_value=config):
                with self.assertRaisesRegex(ValueError, "exact GORELEASER_VERSION"):
                    release.plan()

    def test_retry_uses_releaser_pin_from_original_commit(self):
        from unittest.mock import patch
        os.environ["RELEASE_OPERATION"] = "retry"
        with patch.object(release, "git", side_effect=lambda *args: "v1.2.3\nv1.2.4" if args[0] == "tag" else self.commit):
            with patch.object(release, "file_at", side_effect=lambda ref, path: "  GORELEASER_VERSION: v2.18.0\n" if path.endswith("ci.yml") else "release: {}"):
                release.plan()
        self.assertEqual(self.outputs["releaser"], "v2.18.0")

    def test_docs_only_manual_request_does_not_release(self):
        from unittest.mock import patch
        os.environ["RELEASE_OPERATION"] = "release"
        with patch.object(release, "classify", return_value=(False, [])):
            release.plan()
        self.assertEqual(self.outputs["eligible"], "false")

    def test_unpublished_tag_blocks_new_release_but_can_be_retried(self):
        from unittest.mock import patch
        with patch.object(release, "git", side_effect=lambda *args: "v1.2.3\nv1.2.4" if args[0] == "tag" else self.commit):
            release.plan()
            self.assertEqual(self.outputs["eligible"], "false")
            os.environ["RELEASE_OPERATION"] = "retry"
            release.plan()
        self.assertEqual(self.outputs["tag"], "v1.2.4")
        self.assertEqual(self.outputs["commit"], self.commit)

    def test_published_tag_cannot_be_retried(self):
        os.environ.update(RELEASE_OPERATION="retry", RELEASE_VERSION="v1.2.3")
        with self.assertRaisesRegex(ValueError, "unpublished"):
            release.plan()

    def test_legacy_draft_configuration_requires_manual_recovery(self):
        from unittest.mock import patch
        os.environ["RELEASE_OPERATION"] = "retry"
        with patch.object(release, "git", side_effect=lambda *args: "v1.2.3\nv1.2.4" if args[0] == "tag" else self.commit):
            with patch.object(release, "file_at", return_value="  draft: true"):
                with self.assertRaisesRegex(ValueError, "manual recovery"):
                    release.plan()

    def test_older_legacy_draft_does_not_block(self):
        from unittest.mock import patch
        with patch.object(release, "releases", return_value=[self.entry, dict(self.entry, tag_name="v1.2.2", draft=True)]):
            release.plan()
        self.assertEqual(self.outputs["tag"], "v1.2.4")

    def test_newer_draft_without_tag_requires_manual_cleanup(self):
        from unittest.mock import patch
        with patch.object(release, "releases", return_value=[self.entry, dict(self.entry, tag_name="v1.2.4", draft=True)]):
            release.plan()
            self.assertEqual(self.outputs["eligible"], "false")
            os.environ["RELEASE_OPERATION"] = "retry"
            with self.assertRaisesRegex(ValueError, "existing unpublished tag"):
                release.plan()

    def test_existing_local_tag_is_left_to_native_validation(self):
        from unittest.mock import patch
        with patch.object(release, "github") as api, patch.object(release, "git") as git:
            release.tag_commit()
        api.assert_not_called()
        git.assert_not_called()

    def test_new_tag_is_recorded_before_native_publication(self):
        from unittest.mock import patch
        with patch.object(release.subprocess, "run", return_value=type("Result", (), {"returncode": 1})()):
            with patch.object(release, "github") as api, patch.object(release, "git") as git:
                release.tag_commit()
        api.assert_called_once_with("git/refs", {"ref": "refs/tags/v1.2.4", "sha": self.commit})
        git.assert_called_once_with("tag", "v1.2.4", self.commit)

    def test_remote_tag_conflict_does_not_create_a_local_tag(self):
        from unittest.mock import patch
        from urllib.error import HTTPError
        conflict = HTTPError("https://api.github.com", 422, "Reference already exists", {}, None)
        with patch.object(release.subprocess, "run", return_value=type("Result", (), {"returncode": 1})()):
            with patch.object(release, "github", side_effect=conflict), patch.object(release, "git") as git:
                with self.assertRaises(HTTPError):
                    release.tag_commit()
        git.assert_not_called()
        conflict.close()

class CIGateTests(unittest.TestCase):
    def check(self, runs):
        from unittest.mock import patch
        with patch.object(release, "github", return_value={"workflow_runs": runs}):
            release.require_ci("b" * 40)

    def run_info(self, status="completed", conclusion="success", sha="b" * 40):
        return {"head_sha": sha, "status": status, "conclusion": conclusion}

    def test_successful_ci_for_candidate_allows_publication(self):
        self.check([self.run_info()])

    def test_running_ci_blocks_publication(self):
        with self.assertRaisesRegex(ValueError, "CI must finish successfully"):
            self.check([self.run_info(status="in_progress", conclusion=None)])

    def test_failed_latest_rerun_blocks_despite_earlier_success(self):
        with self.assertRaisesRegex(ValueError, "CI must finish successfully"):
            self.check([self.run_info(conclusion="failure"), self.run_info()])

    def test_ci_for_different_commit_does_not_suffice(self):
        with self.assertRaisesRegex(ValueError, "CI must finish successfully"):
            self.check([self.run_info(sha="c" * 40)])


if __name__ == "__main__":
    unittest.main()
