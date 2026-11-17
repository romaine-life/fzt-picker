# No workflows here

fzt-picker's CI is pipeline-templates' `go`, `rust` and `release` suites, which Forgejo
runs from the repo's `ci-` topics. This folder stays so Forgejo reads it, finds nothing,
and leaves `.github/workflows/` to GitHub: with no `.forgejo/workflows`, Forgejo runs
GitHub's workflows itself, and `macos.yml` would wait forever for a Mac runner.
