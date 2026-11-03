# Publish the latest Forgejo release of fzt-picker to \\NelsonNAS\tools as `fzt-picker`
# (Nelson's home machines run it from there; see shell-config-profile-home
# claude\topics\home-tools.md). Cut a release first (release.yml).
#
# One version folder holds the whole picker: the injector (entry), the hook DLL it loads
# into GUI processes, the popup's frontend DLL the hook loads from beside itself, and the
# standalone frontend the `at` commands run. A new version is a new folder, so files a
# running app still has loaded are never replaced; restart the injector to switch.
$ErrorActionPreference = 'Stop'

$repo = 'romaine-life/fzt-picker'
$token = @("protocol=https`nhost=forgejo.romaine.life`n" | git credential fill) -match '^password=' -replace '^password=', ''
if (-not $token) { throw 'git has no credential for forgejo.romaine.life' }
$headers = @{ Authorization = "token $token" }
$api = "https://forgejo.romaine.life/api/v1/repos/$repo"
$release = Invoke-RestMethod "$api/releases/latest" -Headers $headers
$tag = $release.tag_name
$commit = (Invoke-RestMethod "$api/tags/$tag" -Headers $headers).commit.sha

$files = 'fzt-picker.exe', 'fzt_picker_hook.dll', 'fzt_picker_frontend.dll', 'fzt-picker-frontend.exe'
$dir = Join-Path ([IO.Path]::GetTempPath()) "fzt-picker-$tag"
Remove-Item $dir -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory $dir | Out-Null
foreach ($file in $files) {
    $asset = $release.assets | Where-Object name -eq $file
    if (-not $asset) { throw "$tag has no $file" }
    Invoke-WebRequest $asset.browser_download_url -Headers $headers -OutFile (Join-Path $dir $file)
}

# -Repo is the GitHub push mirror: publish-tool's rollback guard compares commits there.
publish-tool -Name fzt-picker -Version $tag -Path $dir `
    -Entry fzt-picker.exe -Repo "https://github.com/$repo" -Commit $commit
