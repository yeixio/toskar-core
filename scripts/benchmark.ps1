# Measure a local AI model on this computer, with Toskar's quality tests (#1037).
#
#   irm https://toskar.ai/benchmark.ps1 | iex
#
# Downloads Toskar and its tests from the latest release, each checked against
# SHA256SUMS.txt, into one folder, without admin rights, a service, Go, or Python.
# It runs its own throwaway Toskar, so your everyday Toskar and its chats aren't
# touched. It suggests a model and asks before downloading one. A real model runs
# with real tools, in an empty folder, and every tool that asks first is answered
# no. Nothing is sent anywhere unless you send the results form yourself.
#
# Environment:
#   TOSKAR_BENCHMARK_DIR   the folder (default %USERPROFILE%\.toskar-benchmark); delete it to remove everything
#   TOSKAR_BENCHMARK_ARGS  more arguments, such as --model qwen2.5-3b-q4
#   TOSKAR_VERSION         a release such as 1.13.0 (default: the latest)
#   TOSKAR_RELEASE_URL     where the release files are (overrides the version)

# In a function, and throw and not exit: iex runs in the person's own window.
function Invoke-ToskarBenchmark {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'

    if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
        throw "Toskar is built for 64-bit Intel and AMD Windows, not $($env:PROCESSOR_ARCHITECTURE)"
    }
    if ($env:TOSKAR_RELEASE_URL) {
        $base = $env:TOSKAR_RELEASE_URL.TrimEnd('/')
    } elseif ($env:TOSKAR_VERSION) {
        $base = "https://github.com/yeixio/toskar-core/releases/download/v$($env:TOSKAR_VERSION.TrimStart('v'))"
    } else {
        $base = 'https://github.com/yeixio/toskar-core/releases/latest/download'
    }
    $dir = if ($env:TOSKAR_BENCHMARK_DIR) { $env:TOSKAR_BENCHMARK_DIR } else { Join-Path $env:USERPROFILE '.toskar-benchmark' }
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("toskar-benchmark-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $dir 'results') -Force | Out-Null
    try {
        Invoke-WebRequest -Uri "$base/SHA256SUMS.txt" -OutFile "$tmp\SHA256SUMS.txt" -UseBasicParsing
        $sums = Get-Content "$tmp\SHA256SUMS.txt"
        foreach ($part in @(
                @{ Pattern = '  (toskar-[0-9]\S*-windows-amd64-headless\.tar\.gz)$'; Folder = 'toskar' },
                @{ Pattern = '  (toskar-quality-\S*-windows-amd64\.tar\.gz)$'; Folder = 'quality' })) {
            $line = $sums | Where-Object { $_ -match $part.Pattern } | Select-Object -First 1
            if (-not $line) { throw 'this release has no file for Windows, or was made before the benchmark' }
            $want, $name = $line -split '\s+', 2
            Write-Host "Downloading $name..."
            Invoke-WebRequest -Uri "$base/$name" -OutFile "$tmp\$name" -UseBasicParsing
            if ((Get-FileHash -Algorithm SHA256 "$tmp\$name").Hash.ToLowerInvariant() -ne $want.ToLowerInvariant()) {
                throw "$name doesn't match its checksum, so it wasn't used"
            }
            $folder = Join-Path $dir $part.Folder
            if (Test-Path $folder) { Remove-Item -Recurse -Force $folder }
            New-Item -ItemType Directory -Path $folder | Out-Null
            # tar ships with Windows 10 1803 and later.
            tar -xzf "$tmp\$name" -C $folder --strip-components=1
            if ($LASTEXITCODE -ne 0) { throw "couldn't unpack $name" }
        }
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    $more = if ($env:TOSKAR_BENCHMARK_ARGS) { $env:TOSKAR_BENCHMARK_ARGS -split '\s+' } else { @() }
    Push-Location (Join-Path $dir 'results')
    try {
        & (Join-Path $dir 'quality\toskar-quality.exe') run --toskar (Join-Path $dir 'toskar\toskar.exe') --harness (Join-Path $dir 'quality') --data (Join-Path $dir 'data') @more
        if ($LASTEXITCODE -ne 0) { throw 'the benchmark stopped; see the message above' }
    } finally {
        Pop-Location
    }
}
Invoke-ToskarBenchmark
