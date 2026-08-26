((.author.login == "dependabot") or
 (.author.login == "dependabot[bot]")) and
((.files | length) > 0) and
all(.files[]; .path == "go.mod" or .path == "go.sum")
