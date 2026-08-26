def has_dependency_label:
  any(.labels[]?; .name == "dependencies");

def go_module_update:
  (.files | length) > 0 and
  any(.files[]; .path == "go.mod") and
  all(.files[]; .path == "go.mod" or .path == "go.sum");

def github_actions_update:
  (.files | length) > 0 and
  all(.files[];
    (.path | startswith(".github/workflows/")) and
    ((.path | endswith(".yml")) or (.path | endswith(".yaml"))));

def parity_toolchain_update:
  (.files | length) == 1 and
  .files[0].path == ".github/dependencies/parity.json";

.isDraft == false and
has_dependency_label and
(
  (.author.login == "dependabot[bot]" and
    (go_module_update or github_actions_update)) or
  (.author.login == "github-actions[bot]" and parity_toolchain_update)
)
