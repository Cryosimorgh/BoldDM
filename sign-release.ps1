param(
    [Parameter(Mandatory=$true)][string]$PfxPath,
    [Parameter(Mandatory=$true)][SecureString]$Password,
    [string]$TimestampUrl = "http://timestamp.digicert.com"
)
$ErrorActionPreference = "Stop"
$plain = [System.Net.NetworkCredential]::new('', $Password).Password
$signtool = Get-Command signtool.exe -ErrorAction Stop
& $signtool.Source sign /fd SHA256 /f $PfxPath /p $plain /tr $TimestampUrl /td SHA256 "$PSScriptRoot\dist\BoltDM.exe"
& $signtool.Source verify /pa /v "$PSScriptRoot\dist\BoltDM.exe"
