# Publish the latest GitHub release of fzt-picker to \\NelsonNAS\tools as `fzt-picker`
# (Nelson's home machines run it from there; see shell-config-profile-home
# claude\topics\home-tools.md). Cut a release first (release.yml).
#
# One version folder holds the whole picker: the injector (entry), the hook DLL it loads
# into GUI processes, the popup's frontend DLL the hook loads from beside itself, and the
# standalone frontend the `at` commands run. A new version is a new folder, so files a
# running app still has loaded are never replaced; restart the injector to switch.
$ErrorActionPreference = 'Stop'

$repo = 'romaine-life/fzt-picker'
$tag = gh release view -R $repo --json tagName --jq .tagName
if ($LASTEXITCODE -ne 0) { throw "no release on $repo" }
$commit = gh api "repos/$repo/commits/$tag" --jq .sha

$files = 'fzt-picker.exe', 'fzt_picker_hook.dll', 'fzt_picker_frontend.dll', 'fzt-picker-frontend.exe'
$dir = Join-Path ([IO.Path]::GetTempPath()) "fzt-picker-$tag"
Remove-Item $dir -Recurse -Force -ErrorAction SilentlyContinue
foreach ($file in $files) {
    gh release download $tag -R $repo -p $file -D $dir
    if ($LASTEXITCODE -ne 0) { throw "downloading $file from $tag failed" }
}

publish-tool -Name fzt-picker -Version $tag -Path $dir `
    -Entry fzt-picker.exe -Repo "https://github.com/$repo" -Commit $commit
