# Install ncli from GitHub Releases.
#
#   irm https://raw.githubusercontent.com/idarciooliveira/namecheap-cli/main/install.ps1 | iex
#
# Options (env vars):
#   NCLI_VERSION      tag to install, e.g. v0.1.0 (default: latest)
#   NCLI_INSTALL_DIR  where to put ncli.exe (default: %LOCALAPPDATA%\ncli\bin)
#   NCLI_NO_PATH=1    don't add the folder to your user PATH

function Install-Ncli {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = 'idarciooliveira/namecheap-cli'

    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "ncli install: unsupported CPU $env:PROCESSOR_ARCHITECTURE" }
    }

    $tag = $env:NCLI_VERSION
    if (-not $tag) {
        $rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -Headers @{ 'User-Agent' = 'ncli-installer' }
        $tag = $rel.tag_name
    }
    $version = $tag.TrimStart('v')
    $archive = "ncli_${version}_windows_${arch}.zip"
    $base = "https://github.com/$repo/releases/download/$tag"

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("ncli-" + [Guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "Downloading ncli $tag (windows/$arch)..."
        Invoke-WebRequest -Uri "$base/$archive" -OutFile (Join-Path $tmp $archive) -UseBasicParsing
        Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing

        $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { ($_ -split '\s+')[1] -eq $archive }
        if (-not $line) { throw "ncli install: $archive not listed in checksums.txt" }
        $expected = ($line -split '\s+')[0].ToLower()
        $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $archive)).Hash.ToLower()
        if ($expected -ne $actual) { throw "ncli install: checksum mismatch for $archive" }

        Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath (Join-Path $tmp 'x') -Force

        $dir = $env:NCLI_INSTALL_DIR
        if (-not $dir) { $dir = Join-Path $env:LOCALAPPDATA 'ncli\bin' }
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
        Copy-Item (Join-Path $tmp 'x\ncli.exe') (Join-Path $dir 'ncli.exe') -Force
    }
    finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $env:NCLI_NO_PATH -and -not (($userPath -split ';') -contains $dir)) {
        $newPath = if ($userPath) { "$userPath;$dir" } else { $dir }
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Write-Host "Added $dir to your user PATH (open a new terminal to pick it up)."
    }
    if (-not (($env:Path -split ';') -contains $dir)) { $env:Path = "$env:Path;$dir" }

    $v = & (Join-Path $dir 'ncli.exe') --version
    Write-Host "Installed $v to $dir\ncli.exe"
    Write-Host ''
    Write-Host 'Next: ncli auth login'
}

Install-Ncli
