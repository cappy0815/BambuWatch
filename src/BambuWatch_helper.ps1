# =============================================================================
#  BambuWatch Helfer v1.6.1  (gehoert zu BambuWatch.au3, wird beim Start nach %TEMP% kopiert)
#  Windows PowerShell 5.1 / .NET Framework 4.x, keine Zusatzmodule.
#
#  Grund: AutoIt kann kein TLS ueber rohe Sockets. Der Bambu A1 spricht auf
#  Port 6000 (Kamera) und 8883 (MQTT) nur TLS mit selbstsigniertem Zertifikat.
#
#  Modi:
#    Camera      Kamerabild im Heimnetz, jedes Bild -> Datei, Zeile "FRAME|pfad"
#                -CamType jpeg : A1/P1 Einzelbilder ueber TLS Port 6000
#                -CamType rtsp : H2/X1/P2 Videostream RTSPS Port 322, per ffmpeg in Einzelbilder
#                -CamType auto : erkennt den Typ an den offenen Ports ("CAMTYPE|jpeg/rtsp")
#    HaCamera    Kamerabild ueber Home Assistant (camera.* -> /api/camera_proxy, image.* -> /api/image_proxy)
#    HaList      Kameras und Bild-Entitaeten aus Home Assistant ("CAM|entity|Name", "IMG|entity|Name")
#    Cover       Bild des Druckobjekts, einmalig: erst HA-Bild-Entitaet (-HaEntity, z. B. ha-bambulab
#                "Titelbild"), sonst Bambu-Cloud (letzter Auftrag des Druckers, Feld "cover").
#                Ausgabe "COVER|pfad|quelle|titel", "NOCOVER|grund" oder "ERR|text"
#    Mqtt        Status (LAN, Port 8883), Zeile "STATUS|k=v|k=v|..."
#    HaStatus    Status ueber Home Assistant (WebSocket, Integration ha-bambulab), gleiche Ausgabe;
#                findet die Entitaeten des Druckers ueber die Seriennummer ("HAENT|cam/cover/light|entity")
#    Light       Druckerlicht im Heimnetz schalten (-LightState on/off), "LIGHT|on/off"
#    HaLight     Druckerlicht ueber Home Assistant schalten (light.turn_on/turn_off)
#    CloudMqtt   Status ueber Bambu-Cloud (us/cn.mqtt.bambulab.com:8883), gleiche Ausgabe
#    Probe       Verbindungstest LAN, Zeile "PROBE|cam|mqtt"
#    CloudLogin  E-Mail + Passwort -> "LOGIN|code" / "LOGIN|tfa|key" / Token
#    CloudCode   E-Mail-Code -> Token        CloudResend  neuen E-Mail-Code anfordern
#    CloudTfa    Code aus Authenticator-App -> Token
#    CloudToken  vorhandenes Token pruefen (z. B. aus dem Browser eingefuegt)
#    Nach erfolgreichem Login: "LOGIN|token|..", "USER|u_..", je Drucker "DEV|...", "DONE"
#  Weitere Ausgaben: "STATE|connecting" / "STATE|connected" / "ERR|text" / "ERR|AUTH|text"
#
#  Geheimnisse kommen NICHT ueber die Kommandozeile, sondern ueber Umgebungsvariablen
#  (BW_CODE, BW_EMAIL, BW_PASS, BW_CODE2, BW_TFAKEY, BW_TOKEN, BW_USER, BW_HATOKEN, BW_HAURLS).
#  "ERR|STOP|text" = dauerhafter Fehler, BambuWatch versucht es erst nach Aenderung der Einstellungen neu.
#
#  Quellen (Community, reverse-engineered, nicht offiziell von Bambu Lab):
#   Kamera   bambu-rs / ha-bambulab: 80-Byte-Auth, dann 16-Byte-Header + JPEG
#   MQTT LAN Benutzer "bblp", Passwort = Access Code, Abo device/<Serie>/report, "pushall"
#   Cloud    ha-bambulab + BambuHelper: api.bambulab.com (China: .cn), Login-Ablauf,
#            Geraeteliste /v1/iot-service/api/user/bind, MQTT-Benutzer "u_<uid>",
#            Passwort = Token, Broker us.mqtt.bambulab.com (auch fuer EU), China cn.mqtt...
#   LAN-IP   print.net.info[].ip als Zahl (little endian), wie in ha-bambulab
#   H2D      Doppelduese: print.device.extruder.info[] (id 0 = rechts, 1 = links),
#            temp: unteres 16-Bit-Wort Ist, oberes Soll; aktive Duese = (state >> 4) & 0xF;
#            Bett print.device.bed.info.temp; Video rtsps://bblp:<code>@<ip>:322/streaming/live/1
#            (alles wie ha-bambulab: models.py / utils.py)
#   Home Assistant  offizielle REST-API, Authorization: Bearer <Long-Lived Access Token>;
#            Bild-Entitaeten ueber /api/image_proxy/<entity> (HA-Quelltext components/image)
#   HA-Status  WebSocket /api/websocket: auth, config/entity_registry/list (unique_id), get_states,
#            subscribe_events state_changed; unique_id = <Serie>_<Schluessel> wie ha-bambulab
#            (sensor/definitions.py, light.py, image.py, camera.py)
#   Licht    LAN: system.ledctrl, led_node chamber_light (ha-bambulab commands.py); Zustand aus
#            print.lights_report[].node/mode (models.py)
#   Cover    Cloud: GET /v1/user-service/my/tasks?deviceId=..&limit=1, Feld hits[].cover (Bild-URL
#            ohne Anmeldung ladbar), wie ha-bambulab bambu_cloud.py / OpenBambuAPI cloud-http.md
# =============================================================================
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Camera', 'Mqtt', 'CloudMqtt', 'Probe', 'CloudLogin', 'CloudCode', 'CloudResend', 'CloudTfa', 'CloudToken', 'HaCamera', 'HaList', 'Cover', 'HaStatus', 'Light', 'HaLight')]
    [string]$Mode,
    [string]$Ip = '',
    [string]$Serial = '',
    [ValidateSet('Global', 'China')][string]$Region = 'Global',
    [string]$OutDir = $env:TEMP,
    [int]$ParentPid = 0,
    [ValidateSet('auto', 'jpeg', 'rtsp')][string]$CamType = 'auto',
    [string]$Ffmpeg = '',
    [string]$HaEntity = '',
    [switch]$HaInsecure,
    [int]$IntervalMs = 1000,
    [ValidateSet('', 'on', 'off')][string]$LightState = '',
    # nur fuer Tests gegen einen simulierten Server (BambuWatch nutzt diese nie):
    [string]$TestApi = '', [string]$TestWeb = '', [string]$TestBroker = '', [switch]$TestInsecure, [string]$TestRtspUrl = ''
)

$ErrorActionPreference = 'Stop'
# Geheimnisse aus der Umgebung holen und sofort entfernen
$code = [string]$env:BW_CODE;     $env:BW_CODE = $null
$email = [string]$env:BW_EMAIL;   $env:BW_EMAIL = $null
$pass = [string]$env:BW_PASS;     $env:BW_PASS = $null
$code2 = [string]$env:BW_CODE2;   $env:BW_CODE2 = $null
$tfaKey = [string]$env:BW_TFAKEY; $env:BW_TFAKEY = $null
$token = [string]$env:BW_TOKEN;   $env:BW_TOKEN = $null
$cloudUser = [string]$env:BW_USER; $env:BW_USER = $null
$haToken = [string]$env:BW_HATOKEN; $env:BW_HATOKEN = $null
$haUrls = [string]$env:BW_HAURLS; $env:BW_HAURLS = $null

$inv = [Globalization.CultureInfo]::InvariantCulture
try { [Console]::OutputEncoding = New-Object Text.UTF8Encoding($false) } catch {}
try {
    # TLS 1.2 (und 1.3, falls das System es kennt) fuer HTTPS zur Bambu-Cloud
    $p = [Net.SecurityProtocolType]::Tls12
    try { $p = $p -bor [Net.SecurityProtocolType]'Tls13' } catch {}
    [Net.ServicePointManager]::SecurityProtocol = $p
} catch {}

$apiBase = if ($Region -eq 'China') { 'https://api.bambulab.cn' } else { 'https://api.bambulab.com' }
$webBase = 'https://bambulab.com'
$cloudBroker = if ($Region -eq 'China') { 'cn.mqtt.bambulab.com' } else { 'us.mqtt.bambulab.com' }
$cloudVerify = $true
if ($TestApi -ne '') { $apiBase = $TestApi }
if ($TestWeb -ne '') { $webBase = $TestWeb }
$cloudPort = 8883
if ($TestBroker -ne '') { $tb = $TestBroker.Split(':'); $cloudBroker = $tb[0]; if ($tb.Length -gt 1) { $cloudPort = [int]$tb[1] } }
if ($TestInsecure) {
    $cloudVerify = $false
}

# ------------------------------------------------------------------ Ausgabe
function Out-Line([string]$s) {
    [Console]::Out.WriteLine($s)
    [Console]::Out.Flush()
}
function Clean([object]$v) {
    if ($null -eq $v) { return '' }
    return (([string]$v) -replace '[\r\n|]', ' ').Trim()
}
function Out-Err([string]$s) { Out-Line ('ERR|' + (Clean $s)) }

# beendet sich, sobald BambuWatch nicht mehr laeuft (kein verwaister Prozess)
function Test-Parent {
    if ($ParentPid -le 0) { return $true }
    try { $null = [Diagnostics.Process]::GetProcessById($ParentPid); return $true } catch { return $false }
}

# ------------------------------------------------------------------ TLS
# Zertifikatspruefung fuer den Drucker (selbstsigniert) per C#-Delegate. Ein PowerShell-
# Scriptblock als Callback kann auf fremden Threads ohne Runspace abstuerzen.
Add-Type -TypeDefinition @'
public static class BWTls {
    public static bool Accept(object s,
        System.Security.Cryptography.X509Certificates.X509Certificate c,
        System.Security.Cryptography.X509Certificates.X509Chain ch,
        System.Net.Security.SslPolicyErrors e) { return true; }
}
'@
$script:certCb = [Delegate]::CreateDelegate([Net.Security.RemoteCertificateValidationCallback], [BWTls].GetMethod('Accept'))

# $verify = $true -> normale Zertifikatspruefung (Bambu-Cloud), sonst alles akzeptieren (Drucker)
function Open-Tls([string]$h, [int]$port, [int]$timeoutMs, [bool]$verify = $false) {
    $tcp = New-Object Net.Sockets.TcpClient
    try {
        $iar = $tcp.BeginConnect($h, $port, $null, $null)
    } catch {
        $tcp.Close()
        throw "Adresse ${h} nicht gefunden (Internet/DNS?)"
    }
    if (-not $iar.AsyncWaitHandle.WaitOne($timeoutMs)) {
        $tcp.Close()
        if ($verify) { throw "Keine Antwort von ${h}:$port (Internet?)" }
        throw "Keine Antwort von ${h}:$port (Drucker an, IP richtig, gleiches Netz?)"
    }
    try { $tcp.EndConnect($iar) } catch {
        $tcp.Close()
        if ($verify) { throw "Verbindung zu ${h}:$port fehlgeschlagen (Internet?)" }
        throw "Verbindung zu ${h}:$port abgelehnt (Dienst am Drucker aus?)"
    }
    $tcp.NoDelay = $true
    if ($verify) {
        $ssl = New-Object Net.Security.SslStream($tcp.GetStream(), $false)
    } else {
        $ssl = New-Object Net.Security.SslStream($tcp.GetStream(), $false, $script:certCb)
    }
    $ssl.ReadTimeout = $timeoutMs
    $ssl.WriteTimeout = $timeoutMs
    try {
        $ssl.AuthenticateAsClient($h, $null, [Security.Authentication.SslProtocols]::Tls12, $verify)
    } catch {
        $tcp.Close()
        $ex = $_.Exception
        while ($null -ne $ex.InnerException) { $ex = $ex.InnerException }
        throw "TLS-Handshake mit ${h}:$port fehlgeschlagen: $($ex.Message)"
    }
    return @{ Tcp = $tcp; Ssl = $ssl }
}

function Close-Tls($c) {
    if ($null -eq $c) { return }
    try { $c.Ssl.Dispose() } catch {}
    try { $c.Tcp.Close() } catch {}
}

function Read-Exact($stream, [int]$n) {
    $buf = New-Object byte[] $n
    $off = 0
    while ($off -lt $n) {
        $r = $stream.Read($buf, $off, $n - $off)
        if ($r -le 0) { throw 'Verbindung vom Drucker geschlossen' }
        $off += $r
    }
    return , $buf
}

# ================================================================== KAMERA
function New-CamAuth {
    $a = New-Object byte[] 80
    [BitConverter]::GetBytes([uint32]0x40).CopyTo($a, 0)
    [BitConverter]::GetBytes([uint32]0x3000).CopyTo($a, 4)
    # Bytes 8..15 bleiben 0
    $u = [Text.Encoding]::ASCII.GetBytes('bblp')
    [Array]::Copy($u, 0, $a, 16, $u.Length)
    $c = [Text.Encoding]::ASCII.GetBytes($code)
    [Array]::Copy($c, 0, $a, 48, [Math]::Min($c.Length, 32))
    return , $a
}

# liest einen Frame; liefert JPEG-Bytes oder $null (kein JPEG, ueberspringen)
function Read-CamFrame($ssl) {
    $hdr = Read-Exact $ssl 16
    $len = [BitConverter]::ToUInt32($hdr, 0)
    if ($len -lt 100 -or $len -gt 8000000) { throw "Unerwarteter Bild-Header (Laenge $len) - Kamera aus oder Protokoll geaendert" }
    $jpg = Read-Exact $ssl ([int]$len)
    if ($jpg[0] -ne 0xFF -or $jpg[1] -ne 0xD8) { return $null }
    return , $jpg
}

function Test-Port([string]$h, [int]$port, [int]$ms) {
    $t = New-Object Net.Sockets.TcpClient
    try {
        $iar = $t.BeginConnect($h, $port, $null, $null)
        if (-not $iar.AsyncWaitHandle.WaitOne($ms)) { return $false }
        $t.EndConnect($iar)
        return $true
    } catch { return $false } finally { $t.Close() }
}

function Sleep-Parent([int]$sec) {
    for ($i = 0; $i -lt $sec * 2 -and (Test-Parent); $i++) { Start-Sleep -Milliseconds 500 }
}

function Run-Camera {
    $type = $CamType
    if ($TestRtspUrl -ne '') { $type = 'rtsp' }
    if ($type -eq 'auto') {
        # A1/P1: Port 6000 (Einzelbilder), H2/X1/P2: Port 322 (RTSPS-Video)
        while (Test-Parent) {
            if (Test-Port $Ip 6000 2500) { $type = 'jpeg'; break }
            if (Test-Port $Ip 322 2500) { $type = 'rtsp'; break }
            Out-Err 'Kamera nicht erreichbar (Port 6000/322) - Drucker an, LAN Mode Liveview an, gleiches Netz?'
            Sleep-Parent 5
        }
        if (-not (Test-Parent)) { return }
        Out-Line "CAMTYPE|$type"
    }
    if ($type -eq 'rtsp') { Run-CameraRtsp } else { Run-CameraJpeg }
}

# sucht die Bytefolge FF <m2> ab $start (IndexOf ist nativ und schnell)
function Find-Marker([byte[]]$d, [byte]$m2, [int]$start) {
    $i = $start
    while ($i -ge 0 -and $i -lt $d.Length - 1) {
        $i = [Array]::IndexOf($d, [byte]0xFF, $i)
        if ($i -lt 0 -or $i -ge $d.Length - 1) { return -1 }
        if ($d[$i + 1] -eq $m2) { return $i }
        $i++
    }
    return -1
}

# H2D, X1, P2S: RTSPS-Videostream -> ffmpeg macht daraus 2 JPEG pro Sekunde
function Run-CameraRtsp {
    if ($Ffmpeg -eq '' -or -not (Test-Path -LiteralPath $Ffmpeg)) {
        Out-Line 'ERR|STOP|Dieser Drucker sendet einen Videostream - dafür ffmpeg.exe in den Einstellungen angeben'
        return
    }
    if ($TestRtspUrl -ne '') {
        $inArgs = '-re -stream_loop -1 -i "' + $TestRtspUrl + '"'
    } else {
        $url = 'rtsps://bblp:' + [Uri]::EscapeDataString($code) + '@' + $Ip + ':322/streaming/live/1'
        # -tls_verify 0: der Drucker hat ein selbstsigniertes Zertifikat; neuere ffmpeg-Versionen
        # pruefen standardmaessig (tls.h: tls_verify Vorgabe 1) -> unter Windows Fehler 0x80090325
        $inArgs = '-rtsp_transport tcp -tls_verify 0 -i "' + $url + '"'
    }
    $slot = 0
    while (Test-Parent) {
        $p = $null
        try {
            Out-Line 'STATE|connecting'
            $psi = New-Object Diagnostics.ProcessStartInfo
            $psi.FileName = $Ffmpeg
            $psi.Arguments = '-hide_banner -loglevel error -nostdin ' + $inArgs + ' -an -vf fps=2 -q:v 5 -f mjpeg pipe:1'
            $psi.UseShellExecute = $false
            $psi.RedirectStandardOutput = $true
            $psi.RedirectStandardError = $true
            $psi.CreateNoWindow = $true
            $p = [Diagnostics.Process]::Start($psi)
            $errTask = $p.StandardError.ReadToEndAsync() # Fehlertext nebenher einsammeln (kein Blockieren)
            $out = $p.StandardOutput.BaseStream
            $buf = New-Object byte[] 65536
            $ms = New-Object IO.MemoryStream
            $first = $true
            while (Test-Parent) {
                $ar = $out.BeginRead($buf, 0, $buf.Length, $null, $null)
                if (-not $ar.AsyncWaitHandle.WaitOne(20000)) { throw 'Kein Bild aus dem Videostream (20 s) - LAN Mode Liveview an?' }
                $n = $out.EndRead($ar)
                if ($n -le 0) { break }
                $ms.Write($buf, 0, $n)
                $data = $ms.ToArray()
                $pos = 0
                while ($true) {
                    $s = Find-Marker $data 0xD8 $pos
                    if ($s -lt 0) { $pos = [Math]::Max(0, $data.Length - 1); break }
                    $e = Find-Marker $data 0xD9 ($s + 2)
                    if ($e -lt 0) { $pos = $s; break }
                    $jpg = New-Object byte[] ($e + 2 - $s)
                    [Array]::Copy($data, $s, $jpg, 0, $jpg.Length)
                    $slot = ($slot + 1) % 3
                    $path = Join-Path $OutDir ("bw_frame_$slot.jpg")
                    [IO.File]::WriteAllBytes($path, $jpg)
                    if ($first) { Out-Line 'STATE|connected'; $first = $false }
                    Out-Line "FRAME|$path"
                    $pos = $e + 2
                }
                $rest = $data.Length - $pos
                $ms = New-Object IO.MemoryStream
                if ($rest -gt 0) { $ms.Write($data, $pos, $rest) }
            }
            # ffmpeg beendet: Grund aus der Fehlerausgabe melden
            if (-not $p.WaitForExit(3000)) { $p.Kill() }
            $et = ''
            if ($errTask.Wait(1000)) { $et = ($errTask.Result -split "`n" | Where-Object { $_.Trim() -ne '' } | Select-Object -Last 1) }
            if ($et -match '(?i)401|unauthorized') { $et = 'Videostream: Access Code falsch?' }
            elseif ($et -match '(?i)refused|timed out|timeout') { $et = 'Videostream nicht erreichbar - LAN Mode Liveview an?' }
            Out-Err ('ffmpeg beendet' + $(if ($et) { ': ' + $et } else { '' }))
        } catch {
            Out-Err $_.Exception.Message
        } finally {
            if ($null -ne $p) { try { if (-not $p.HasExited) { $p.Kill() } } catch {} }
        }
        Sleep-Parent 5
    }
}

function Run-CameraJpeg {
    $auth = New-CamAuth
    $slot = 0
    while (Test-Parent) {
        $conn = $null
        try {
            Out-Line 'STATE|connecting'
            $conn = Open-Tls $Ip 6000 8000
            $conn.Ssl.Write($auth, 0, $auth.Length)
            $conn.Ssl.Flush()
            Out-Line 'STATE|connected'
            while (Test-Parent) {
                $jpg = Read-CamFrame $conn.Ssl
                if ($null -eq $jpg) { continue }
                # drei Dateien im Wechsel: BambuWatch liest immer die zuletzt gemeldete
                $slot = ($slot + 1) % 3
                $path = Join-Path $OutDir ("bw_frame_$slot.jpg")
                [IO.File]::WriteAllBytes($path, $jpg)
                Out-Line "FRAME|$path"
            }
        } catch {
            $m = $_.Exception.Message
            if ($_.Exception -is [IO.IOException] -or $m -match 'Zeit|time') {
                $m = 'Kein Bild vom Drucker (Kamera aus, LAN-Liveview deaktiviert oder Access Code falsch?)'
            }
            Out-Err $m
        } finally { Close-Tls $conn }
        for ($i = 0; $i -lt 6 -and (Test-Parent); $i++) { Start-Sleep -Milliseconds 500 }
    }
}

# ================================================================== MQTT
function Add-MqttStr($list, [string]$s) {
    $b = [Text.Encoding]::UTF8.GetBytes($s)
    $list.Add([byte][Math]::Floor($b.Length / 256))
    $list.Add([byte]($b.Length % 256))
    $list.AddRange($b)
}

function New-Packet([byte]$type, [byte[]]$body) {
    $ms = New-Object IO.MemoryStream
    $ms.WriteByte($type)
    $len = $body.Length
    do {
        $d = $len % 128
        $len = [Math]::Floor($len / 128)
        if ($len -gt 0) { $d = $d -bor 128 }
        $ms.WriteByte([byte]$d)
    } while ($len -gt 0)
    if ($body.Length -gt 0) { $ms.Write($body, 0, $body.Length) }
    return , $ms.ToArray()
}

function Send-Packet($ssl, [byte[]]$p) {
    $ssl.Write($p, 0, $p.Length)
    $ssl.Flush()
}

function Mqtt-Connect($ssl, [string]$user, [string]$pw) {
    $l = New-Object 'System.Collections.Generic.List[byte]'
    Add-MqttStr $l 'MQTT'
    $l.Add(4)          # Protokoll 3.1.1
    $l.Add(0xC2)       # Benutzername + Passwort + Clean Session
    $l.Add(0); $l.Add(60) # Keep-Alive 60 s
    Add-MqttStr $l ('bw_' + [Guid]::NewGuid().ToString('N').Substring(0, 10))
    Add-MqttStr $l $user
    Add-MqttStr $l $pw
    Send-Packet $ssl (New-Packet 0x10 $l.ToArray())
}

function Mqtt-Subscribe($ssl, [string]$topic) {
    $l = New-Object 'System.Collections.Generic.List[byte]'
    $l.Add(0); $l.Add(1) # Paket-ID
    Add-MqttStr $l $topic
    $l.Add(0)            # QoS 0
    Send-Packet $ssl (New-Packet 0x82 $l.ToArray())
}

function Mqtt-Publish($ssl, [string]$topic, [string]$payload) {
    $l = New-Object 'System.Collections.Generic.List[byte]'
    Add-MqttStr $l $topic
    $l.AddRange([Text.Encoding]::UTF8.GetBytes($payload))
    Send-Packet $ssl (New-Packet 0x30 $l.ToArray())
}

# liest den Rest eines Pakets, dessen erstes Byte schon da ist
function Mqtt-ReadRest($ssl, [byte]$first) {
    $mult = 1; $len = 0
    for ($i = 0; $i -lt 4; $i++) {
        $b = (Read-Exact $ssl 1)[0]
        $len += ($b -band 127) * $mult
        $mult *= 128
        if (($b -band 128) -eq 0) { break }
    }
    $body = if ($len -gt 0) { Read-Exact $ssl $len } else { , (New-Object byte[] 0) }
    return @{ Type = ($first -band 0xF0); Flags = ($first -band 0x0F); Body = $body }
}

# --- Status zusammenfuehren (A1 schickt nach dem Vollstatus nur Aenderungen)
$script:st = @{}
$script:amsUnits = $null   # Liste der AMS-Einheiten mit ihren Slots
$script:trayNow = -1       # aktiver Slot
$script:vt = $null         # externe Spule
$script:lanIp = ''         # LAN-IP aus print.net.info
$script:dual = $false      # H2D: zwei Duesen (device.extruder.info)
$script:noz = @{ 0 = $null; 1 = $null }; $script:nozt = @{ 0 = $null; 1 = $null }
$script:snow = @{ 0 = -1; 1 = -1 }; $script:activeNoz = 0
$script:bedDev = $null     # Bett aus device.bed.info.temp (neuere Firmware)
$script:rtsp = ''          # ipcam.rtsp_url ("disable" = Liveview aus)
$script:lastOut = ''

function Merge-Report($json) {
    $p = $json.print
    if ($null -eq $p) { return $false }
    foreach ($prop in $p.PSObject.Properties) {
        switch ($prop.Name) {
            'ams' {
                # Teilupdates moeglich: nur uebernehmen, was mitkommt
                $a = $prop.Value
                if ($null -ne $a.ams) { $script:amsUnits = @($a.ams) }
                if ($null -ne $a.tray_now -and [string]$a.tray_now -ne '') { $script:trayNow = [int]$a.tray_now }
            }
            'vt_tray' { $script:vt = $prop.Value }
            'device' {
                # neuere Firmware / H2D: Temperaturen als 32-Bit (unten Ist, oben Soll)
                $d = $prop.Value
                if ($null -ne $d.extruder) {
                    if ($null -ne $d.extruder.info) {
                        foreach ($e in @($d.extruder.info)) {
                            $id = [int]$e.id
                            if ($id -ne 0 -and $id -ne 1) { continue }
                            if ($null -ne $e.temp) {
                                $t = [int64]$e.temp
                                $script:noz[$id] = $t -band 0xFFFF
                                $script:nozt[$id] = ($t -shr 16) -band 0xFFFF
                            }
                            if ($null -ne $e.snow) { $script:snow[$id] = [int]$e.snow }
                        }
                        if (@($d.extruder.info).Count -ge 2) { $script:dual = $true }
                    }
                    if ($null -ne $d.extruder.state) { $script:activeNoz = ([int]$d.extruder.state -shr 4) -band 0xF }
                }
                if ($null -ne $d.bed -and $null -ne $d.bed.info -and $null -ne $d.bed.info.temp) {
                    $t = [int64]$d.bed.info.temp
                    $script:bedDev = @(($t -band 0xFFFF), (($t -shr 16) -band 0xFFFF))
                }
            }
            'ipcam' { if ($null -ne $prop.Value.rtsp_url) { $script:rtsp = [string]$prop.Value.rtsp_url } }
            'net' {
                # IP als Zahl, Bytes in little-endian-Reihenfolge (wie ha-bambulab)
                foreach ($n in @($prop.Value.info)) {
                    try {
                        $v = [uint32]([int64]$n.ip -band 0xFFFFFFFF)
                        if ($v -ne 0) { $script:lanIp = ([Net.IPAddress]([BitConverter]::GetBytes($v))).ToString(); break }
                    } catch {}
                }
            }
            default { $script:st[$prop.Name] = $prop.Value }
        }
    }
    return $true
}

function Num($v, [string]$fmt = '0') {
    if ($null -eq $v -or [string]$v -eq '') { return '' }
    try { return ([double]$v).ToString($fmt, $inv) } catch { return '' }
}

# aktuelles Filament: tray_now 0..n = AMS-Slot (ams_id*4 + tray_id), 254 = externe Spule, 255 = keins
function Get-Tray {
    $res = @{ c = ''; t = ''; s = ''; a = ''; now = -1 }
    $now = $script:trayNow
    if ($script:dual) {
        # H2D: Slot der aktiven Duese aus "snow" (oben AMS-Nr., unten Slot) wie ha-bambulab
        $sn = $script:snow[[int]$script:activeNoz]
        if ($null -ne $sn -and $sn -ge 0) { $now = (($sn -shr 8) * 4) + ($sn -band 0x3) }
    }
    $res.now = $now
    if ($now -eq 254 -and $null -ne $script:vt) {
        $res.c = [string]$script:vt.tray_color; $res.t = [string]$script:vt.tray_type; $res.s = 'Ext'
        return $res
    }
    if ($now -ge 0 -and $now -lt 254 -and $null -ne $script:amsUnits) {
        $aId = [Math]::Floor($now / 4); $tId = $now % 4
        foreach ($unit in $script:amsUnits) {
            if ([int]$unit.id -ne $aId) { continue }
            foreach ($tr in @($unit.tray)) {
                if ([int]$tr.id -eq $tId) {
                    $res.c = [string]$tr.tray_color; $res.t = [string]$tr.tray_type; $res.s = [string]($tId + 1); $res.a = [string]($aId + 1)
                }
            }
        }
    }
    return $res
}

# Kammerlicht aus print.lights_report (node chamber_light, mode on/off/flashing)
function Get-Light {
    foreach ($l in @($script:st['lights_report'])) {
        if ($null -ne $l -and [string]$l.node -eq 'chamber_light') { return [string]$l.mode }
    }
    return ''
}

# alle Slots: "Einheit.Slot,Typ,RRGGBB,aktiv" durch ";" getrennt; externe Spule als "E"
function Get-AmsList([int]$now) {
    $out = New-Object Collections.Generic.List[string]
    if ($null -ne $script:amsUnits) {
        foreach ($unit in @($script:amsUnits)) {
            if ($null -eq $unit) { continue }
            $uid = [int]$unit.id
            $ul = $(if ($uid -ge 128) { 'HT' + ($uid - 127) } else { [string]($uid + 1) })
            foreach ($tr in @($unit.tray)) {
                if ($null -eq $tr) { continue }
                $tid = [int]$tr.id
                $col = [string]$tr.tray_color; if ($col.Length -ge 6) { $col = $col.Substring(0, 6) }
                $act = $(if ($uid -lt 128 -and $now -eq $uid * 4 + $tid) { '1' } else { '0' })
                $out.Add($ul + '.' + ($tid + 1) + ',' + ((Clean $tr.tray_type) -replace '[,;]', ' ') + ',' + $col + ',' + $act)
            }
        }
    }
    if ($null -ne $script:vt -and [string]$script:vt.tray_type -ne '') {
        $col = [string]$script:vt.tray_color; if ($col.Length -ge 6) { $col = $col.Substring(0, 6) }
        $out.Add('E,' + ((Clean $script:vt.tray_type) -replace '[,;]', ' ') + ',' + $col + ',' + $(if ($now -eq 254) { '1' } else { '0' }))
    }
    return ($out -join ';')
}

function Out-Status {
    $s = $script:st
    $tr = Get-Tray
    $f = [ordered]@{
        st   = Clean $s['gcode_state']
        pct  = Num $s['mc_percent']
        rem  = Num $s['mc_remaining_time']
        lay  = Num $s['layer_num']
        tlay = Num $s['total_layer_num']
        noz  = Num $s['nozzle_temper']
        nozt = Num $s['nozzle_target_temper']
        bed  = $(if ($null -ne $script:bedDev) { Num $script:bedDev[0] } else { Num $s['bed_temper'] })
        bedt = $(if ($null -ne $script:bedDev) { Num $script:bedDev[1] } else { Num $s['bed_target_temper'] })
        job  = Clean $s['subtask_name']
        err  = Num $s['print_error']
        spd  = Num $s['spd_lvl']
        col  = Clean $tr.c
        fil  = Clean $tr.t
        slot = Clean $tr.s
        ams  = Clean $tr.a
        ip   = Clean $script:lanIp
        cam  = $(if ($script:rtsp -eq 'disable') { 'off' } else { '' })
        dual = $(if ($script:dual) { '1' } else { '' })
        nozR = Num $script:noz[0]; noztR = Num $script:nozt[0]
        nozL = Num $script:noz[1]; noztL = Num $script:nozt[1]
        act  = $(if ($script:dual) { [string]$script:activeNoz } else { '' })
        light = Clean (Get-Light)
        amsl = Clean (Get-AmsList $tr.now)
    }
    if ($script:dual) {
        $f.noz = Num $script:noz[[int]$script:activeNoz]
        $f.nozt = Num $script:nozt[[int]$script:activeNoz]
    }
    $line = 'STATUS'
    foreach ($k in $f.Keys) { $line += "|$k=" + $f[$k] }
    if ($line -ne $script:lastOut) {
        Out-Line $line
        $script:lastOut = $line
    }
}

# gemeinsame MQTT-Schleife fuer LAN und Cloud
function Run-MqttLoop([string]$h, [int]$port, [bool]$verify, [string]$user, [string]$pw, [bool]$isCloud) {
    if ($Serial -eq '') { Out-Err 'Seriennummer fehlt (Einstellungen)'; return }
    $topicRep = "device/$Serial/report"
    $topicReq = "device/$Serial/request"
    $one = New-Object byte[] 1
    # Schonung des Druckers: "pushall" (kompletter Status) hoechstens alle 5 min, nach Fehlern
    # wachsende Wartezeit (5, 10, 20, 40, 60 s) statt alle 5 s neu verbinden
    $pushSw = $null
    $fails = 0
    while (Test-Parent) {
        $conn = $null
        try {
            Out-Line 'STATE|connecting'
            $conn = Open-Tls $h $port 8000 $verify
            $ssl = $conn.Ssl
            Mqtt-Connect $ssl $user $pw
            $first = (Read-Exact $ssl 1)[0]
            $pk = Mqtt-ReadRest $ssl $first
            if ($pk.Type -ne 0x20) { throw 'Unerwartete Antwort auf MQTT-Login' }
            $rc = $pk.Body[1]
            if ($rc -eq 4 -or $rc -eq 5) {
                if ($isCloud) {
                    # Cloud: Token abgelaufen/ungueltig -> nicht endlos weiterprobieren
                    Out-Line 'ERR|AUTH|Bambu-Anmeldung abgelaufen oder ungültig - bitte neu anmelden'
                    return
                }
                throw 'MQTT-Login abgelehnt - Access Code pruefen'
            }
            if ($rc -ne 0) { throw "MQTT-Login abgelehnt (Code $rc)" }
            Mqtt-Subscribe $ssl $topicRep
            if ($null -eq $pushSw -or $pushSw.Elapsed.TotalSeconds -ge 300) {
                Mqtt-Publish $ssl $topicReq '{"pushing":{"sequence_id":"0","command":"pushall"}}'
                $pushSw = [Diagnostics.Stopwatch]::StartNew()
                # nur bei vollem Status von vorn anfangen; sonst gilt der bisherige Stand weiter
                $script:st = @{}; $script:amsUnits = $null; $script:trayNow = -1; $script:vt = $null
                $script:dual = $false; $script:bedDev = $null; $script:rtsp = ''
            }
            $script:lastOut = ''
            Out-Line 'STATE|connected'
            $ping = [Diagnostics.Stopwatch]::StartNew()
            $silence = [Diagnostics.Stopwatch]::StartNew()
            $ar = $null
            while (Test-Parent) {
                # erstes Byte asynchron lesen, damit nebenbei Pings rausgehen koennen
                if ($null -eq $ar) { $ar = $ssl.BeginRead($one, 0, 1, $null, $null) }
                if ($ar.AsyncWaitHandle.WaitOne(1000)) {
                    $n = $ssl.EndRead($ar); $ar = $null
                    if ($n -le 0) { throw 'Verbindung geschlossen' }
                    $pk = Mqtt-ReadRest $ssl $one[0]
                    $silence.Restart()
                    if ($pk.Type -eq 0x90 -and $pk.Body.Length -ge 3 -and $pk.Body[2] -eq 0x80) {
                        throw 'Abo abgelehnt - Seriennummer pruefen'
                    }
                    if ($pk.Type -eq 0x30) {
                        $b = $pk.Body
                        $tl = $b[0] * 256 + $b[1]
                        $start = 2 + $tl
                        if ((($pk.Flags -shr 1) -band 3) -gt 0) { $start += 2 }
                        if ($b.Length -gt $start) {
                            $txt = [Text.Encoding]::UTF8.GetString($b, $start, $b.Length - $start)
                            try { $j = $txt | ConvertFrom-Json } catch { $j = $null }
                            if ($null -ne $j -and (Merge-Report $j)) { Out-Status; $fails = 0 }
                        }
                    }
                }
                if ($ping.Elapsed.TotalSeconds -ge 30) {
                    Send-Packet $ssl ([byte[]](0xC0, 0x00))
                    $ping.Restart()
                }
                if ($silence.Elapsed.TotalSeconds -ge 95) { throw 'Keine Daten mehr vom Drucker' }
            }
        } catch {
            Out-Err $_.Exception.Message
        } finally { Close-Tls $conn }
        $fails++
        $wait = [Math]::Min(60, 5 * [Math]::Pow(2, [Math]::Min($fails - 1, 4)))
        for ($i = 0; $i -lt $wait * 2 -and (Test-Parent); $i++) { Start-Sleep -Milliseconds 500 }
    }
}

# ================================================================== PROBE (LAN)
function Run-Probe {
    $cam = ''
    $mq = ''
    $c = $null
    $camRtsp = $false
    if ($CamType -eq 'rtsp' -or ($CamType -eq 'auto' -and -not (Test-Port $Ip 6000 2500) -and (Test-Port $Ip 322 2500))) { $camRtsp = $true }
    if ($camRtsp) {
        if ($Ffmpeg -eq '' -or -not (Test-Path -LiteralPath $Ffmpeg)) { $cam = 'warn:Videostream-Drucker (Port 322) erkannt - für das Bild ffmpeg.exe angeben' }
        else { $cam = 'ok:Videostream-Drucker (Port 322) erkannt - Bild über ffmpeg' }
    }
    try {
        if ($camRtsp) { throw 'skip' }
        $c = Open-Tls $Ip 6000 6000
        $a = New-CamAuth
        $c.Ssl.Write($a, 0, $a.Length); $c.Ssl.Flush()
        $c.Ssl.ReadTimeout = 8000
        try {
            $jpg = Read-CamFrame $c.Ssl
            $cam = if ($null -ne $jpg) { 'ok:Bild empfangen (' + [Math]::Round($jpg.Length / 1024) + ' KB)' } else { 'warn:Daten empfangen, aber kein JPEG' }
        } catch { $cam = 'warn:Verbunden, aber kein Bild in 8 s (Kamera aus? Access Code?)' }
    } catch { if (-not $camRtsp) { $cam = 'err:' + (Clean $_.Exception.Message) } } finally { Close-Tls $c }

    $c = $null
    try {
        if ($Serial -eq '') { throw 'Seriennummer fehlt' }
        $c = Open-Tls $Ip 8883 6000
        Mqtt-Connect $c.Ssl 'bblp' $code
        $first = (Read-Exact $c.Ssl 1)[0]
        $pk = Mqtt-ReadRest $c.Ssl $first
        if ($pk.Type -ne 0x20) { throw 'Unerwartete Antwort' }
        if ($pk.Body[1] -ne 0) { throw ('Login abgelehnt (Code ' + $pk.Body[1] + ') - Access Code pruefen') }
        Mqtt-Subscribe $c.Ssl "device/$Serial/report"
        Mqtt-Publish $c.Ssl "device/$Serial/request" '{"pushing":{"sequence_id":"0","command":"pushall"}}'
        $c.Ssl.ReadTimeout = 8000
        $mq = 'warn:Angemeldet, aber kein Status in 8 s (Seriennummer? Bei H2/X1: LAN Only + Developer Mode)'
        $sw = [Diagnostics.Stopwatch]::StartNew()
        while ($sw.Elapsed.TotalSeconds -lt 8) {
            $first = (Read-Exact $c.Ssl 1)[0]
            $pk = Mqtt-ReadRest $c.Ssl $first
            if ($pk.Type -eq 0x90 -and $pk.Body.Length -ge 3 -and $pk.Body[2] -eq 0x80) { $mq = 'err:Abo abgelehnt - Seriennummer pruefen'; break }
            if ($pk.Type -eq 0x30) { $mq = 'ok:Status empfangen'; break }
        }
    } catch {
        if ($mq -eq '') { $mq = 'err:' + (Clean $_.Exception.Message) }
    } finally { Close-Tls $c }
    Out-Line "PROBE|$cam|$mq"
}

# ================================================================== CLOUD (HTTPS)
$script:cookies = New-Object Net.CookieContainer

# HTTPS-Aufruf mit den Kopfzeilen, die auch ha-bambulab / BambuHelper (OrcaSlicer) senden
function Invoke-Bambu([string]$method, [string]$url, $body = $null, [string]$bearer = '', [hashtable]$extra = $null) {
    $req = [Net.HttpWebRequest]::Create($url)
    $req.Method = $method
    $req.Timeout = 15000
    $req.ReadWriteTimeout = 15000
    $req.UserAgent = 'bambu_network_agent/01.09.05.01'
    $req.Accept = 'application/json'
    $req.AutomaticDecompression = [Net.DecompressionMethods]::GZip -bor [Net.DecompressionMethods]::Deflate
    $req.CookieContainer = $script:cookies
    if ($TestInsecure) { $req.ServerCertificateValidationCallback = $script:certCb }
    $req.Headers.Add('X-BBL-Client-Name', 'OrcaSlicer')
    $req.Headers.Add('X-BBL-Client-Type', 'slicer')
    $req.Headers.Add('X-BBL-Client-Version', '01.09.05.51')
    $req.Headers.Add('X-BBL-Language', 'de-DE')
    $req.Headers.Add('X-BBL-OS-Type', 'windows')
    $req.Headers.Add('X-BBL-Agent-Version', '01.09.05.01')
    if ($bearer -ne '') { $req.Headers.Add('Authorization', "Bearer $bearer") }
    if ($null -ne $extra) { foreach ($k in $extra.Keys) { $req.Headers.Add($k, [string]$extra[$k]) } }
    if ($null -ne $body) {
        $bytes = [Text.Encoding]::UTF8.GetBytes(($body | ConvertTo-Json -Compress))
        $req.ContentType = 'application/json'
        $req.ContentLength = $bytes.Length
        $rs = $req.GetRequestStream(); $rs.Write($bytes, 0, $bytes.Length); $rs.Close()
    }
    $resp = $null
    try {
        $resp = $req.GetResponse()
    } catch {
        $ex = $_.Exception
        while ($null -ne $ex -and -not ($ex -is [Net.WebException])) { $ex = $ex.InnerException }
        if ($null -ne $ex -and $null -ne $ex.Response) { $resp = $ex.Response }
        else { throw "Bambu-Server nicht erreichbar ($($_.Exception.Message))" }
    }
    $status = [int]$resp.StatusCode
    $sr = New-Object IO.StreamReader($resp.GetResponseStream(), [Text.Encoding]::UTF8)
    $text = $sr.ReadToEnd()
    $resp.Close()
    if (($status -eq 403 -or $status -eq 429) -and $text -match '(?i)cloudflare') {
        throw 'CLOUDFLARE'
    }
    $json = $null
    try { $json = $text | ConvertFrom-Json } catch {}
    return @{ Status = $status; Text = $text; Json = $json }
}

# Wert eines JSON-Feldes; nur echte Texte gelten als leer. Objekte/Listen nie per [string]
# pruefen: in Windows PowerShell 5.1 kann das fuer ein JSON-Objekt einen leeren Text ergeben.
function Get-JsonProp($o, [string[]]$names) {
    if ($null -eq $o) { return $null }
    foreach ($n in $names) {
        $p = $o.PSObject.Properties[$n]
        if ($null -eq $p -or $null -eq $p.Value) { continue }
        if ($p.Value -is [string] -and $p.Value -eq '') { continue }
        return $p.Value
    }
    return $null
}

# Token aus einer Login-Antwort (accessToken direkt oder unter data)
function Get-TokenFrom($json) {
    $t = Get-JsonProp $json @('accessToken', 'token')
    if ($null -eq $t) { $t = Get-JsonProp (Get-JsonProp $json @('data')) @('accessToken', 'token') }
    return $t
}

# MQTT-Benutzername "u_<uid>": erst aus dem Token (JWT), dann ueber die API
function Resolve-User([string]$tok) {
    $parts = $tok.Split('.')
    if ($parts.Length -eq 3) {
        try {
            $b64 = $parts[1].Replace('-', '+').Replace('_', '/')
            $b64 += '=' * ((4 - $b64.Length % 4) % 4)
            $pl = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($b64)) | ConvertFrom-Json
            $un = Get-JsonProp $pl @('username')
            if ($null -ne $un -and ([string]$un).StartsWith('u_')) { return [string]$un }
            $uid = Get-JsonProp $pl @('uid', 'sub', 'user_id')
            if ($null -ne $uid) { return 'u_' + [string]$uid }
        } catch {}
    }
    foreach ($path in @('/v1/design-user-service/my/preference', '/v1/user-service/my/profile')) {
        try {
            $r = Invoke-Bambu 'GET' ($apiBase + $path) $null $tok
            if ($r.Status -ne 200) { continue }
            $uid = Get-JsonProp $r.Json @('uidStr', 'uid', 'userId', 'user_id')
            if ($null -eq $uid) { $uid = Get-JsonProp (Get-JsonProp $r.Json @('data')) @('uidStr', 'uid', 'userId', 'user_id') }
            if ($null -ne $uid) { return 'u_' + [string]$uid }
        } catch { if ($_.Exception.Message -eq 'CLOUDFLARE') { throw } }
    }
    return ''
}

# nach erfolgreichem Login: Benutzer + Druckerliste ausgeben
function Out-Account([string]$tok) {
    $r = Invoke-Bambu 'GET' ($apiBase + '/v1/iot-service/api/user/bind') $null $tok
    if ($r.Status -eq 401 -or $r.Status -eq 403) { Out-Line 'ERR|AUTH|Token ungültig oder abgelaufen'; return }
    if ($r.Status -ne 200) { Out-Err "Druckerliste nicht abrufbar (HTTP $($r.Status))"; return }
    $user = Resolve-User $tok
    if ($user -eq '') { Out-Err 'Benutzerkennung (uid) nicht ermittelbar'; return }
    Out-Line "LOGIN|token|$tok"
    Out-Line "USER|$user"
    $devs = Get-JsonProp $r.Json @('devices')
    if ($null -eq $devs) { $devs = Get-JsonProp $r.Json @('data') }
    $n = 0
    foreach ($d in @($devs)) {
        if ($null -eq $d) { continue }
        $sn = ([string](Get-JsonProp $d @('dev_id'))).ToUpper()
        if ($sn -eq '') { continue }
        $n++
        Out-Line ('DEV|' + (Clean $sn) + '|' + (Clean (Get-JsonProp $d @('name'))) + '|' + (Clean (Get-JsonProp $d @('dev_access_code'))) + '|' + `
            (Clean (Get-JsonProp $d @('online'))) + '|' + (Clean (Get-JsonProp $d @('dev_product_name', 'dev_model_name'))))
    }
    if ($n -eq 0) {
        # Diagnose: nur Feldnamen und Anzahl, keine Werte (keine Codes/Token)
        $keys = ''
        if ($null -ne $r.Json) { $keys = ($r.Json.PSObject.Properties | ForEach-Object { $_.Name }) -join ',' }
        $cnt = @($devs | Where-Object { $null -ne $_ }).Count
        Out-Err ("Keine Drucker in diesem Konto gefunden (Antwort: Felder [" + $keys + "], Einträge " + $cnt + ")")
    }
    Out-Line 'DONE'
}

function Run-Cloud {
    switch ($Mode) {
        'CloudLogin' {
            if ($email -eq '' -or $pass -eq '') { Out-Err 'E-Mail und Passwort eintragen'; return }
            $r = Invoke-Bambu 'POST' ($apiBase + '/v1/user-service/user/login') @{ account = $email; password = $pass; apiError = '' }
            $tok = Get-TokenFrom $r.Json
            if ($null -ne $tok) { Out-Account $tok; return }
            $lt = [string](Get-JsonProp $r.Json @('loginType'))
            if ($lt -eq 'verifyCode') { Out-Line 'LOGIN|code'; return }   # Bambu verschickt den Code selbst
            if ($lt -eq 'tfa') { Out-Line ('LOGIN|tfa|' + (Clean (Get-JsonProp $r.Json @('tfaKey')))); return }
            if ($r.Status -eq 400 -or $r.Status -eq 401) { Out-Err 'Anmeldung abgelehnt - E-Mail oder Passwort falsch?'; return }
            Out-Err "Unbekannte Antwort vom Bambu-Server (HTTP $($r.Status))"
        }
        'CloudResend' {
            $null = Invoke-Bambu 'POST' ($apiBase + '/v1/user-service/user/sendemail/code') @{ email = $email; type = 'codeLogin' }
            Out-Line 'LOGIN|code'
        }
        'CloudCode' {
            $r = Invoke-Bambu 'POST' ($apiBase + '/v1/user-service/user/login') @{ account = $email; code = $code2 }
            $tok = Get-TokenFrom $r.Json
            if ($null -ne $tok) { Out-Account $tok; return }
            $ec = [string](Get-JsonProp $r.Json @('code'))
            if ($ec -eq '1') {
                # Code abgelaufen -> neuen anfordern (wie ha-bambulab)
                try { $null = Invoke-Bambu 'POST' ($apiBase + '/v1/user-service/user/sendemail/code') @{ email = $email; type = 'codeLogin' } } catch {}
                Out-Err 'Code abgelaufen - ein neuer Code wurde per E-Mail geschickt'; return
            }
            if ($ec -eq '2') { Out-Err 'Code falsch - bitte nochmal eingeben'; return }
            Out-Err "Code nicht akzeptiert (HTTP $($r.Status))"
        }
        'CloudTfa' {
            # 2FA laeuft ueber bambulab.com und braucht dort ein CSRF-Cookie (Ablauf wie ha-bambulab)
            $null = Invoke-Bambu 'GET' ($webBase + '/api/csrf')
            $csrf = $script:cookies.GetCookies([Uri]$webBase)['bbl_csrf_token']
            if ($null -eq $csrf) {
                # Rueckfall wie ha-bambulab: Login per E-Mail-Code
                try { $null = Invoke-Bambu 'POST' ($apiBase + '/v1/user-service/user/sendemail/code') @{ email = $email; type = 'codeLogin' } } catch {}
                Out-Line 'LOGIN|code'; return
            }
            $hdr = @{ 'x-bbl-csrf-token' = $csrf.Value }
            $r = Invoke-Bambu 'POST' ($webBase + '/api/sign-in/tfa') @{ tfaKey = $tfaKey; tfaCode = $code2 } '' $hdr
            if ($r.Status -eq 403 -and $r.Text -match 'missing_cookie') {
                try { $null = Invoke-Bambu 'POST' ($apiBase + '/v1/user-service/user/sendemail/code') @{ email = $email; type = 'codeLogin' } } catch {}
                Out-Line 'LOGIN|code'; return
            }
            $tc = $script:cookies.GetCookies([Uri]$webBase)['token']
            $tok = if ($null -ne $tc) { $tc.Value } else { Get-TokenFrom $r.Json }
            if ($null -ne $tok -and [string]$tok -ne '') { Out-Account $tok; return }
            Out-Err 'Code aus der Authenticator-App nicht akzeptiert'
        }
        'CloudToken' {
            $t = $token.Trim().Trim('"')
            if ($t.StartsWith('Bearer ')) { $t = $t.Substring(7) }
            if ($t -eq '') { Out-Err 'Token ist leer'; return }
            Out-Account $t
        }
    }
}

# ================================================================== HOME ASSISTANT
function Get-HaUrls { return @($haUrls.Split('|') | ForEach-Object { $_.Trim().TrimEnd('/') } | Where-Object { $_ -ne '' }) }

# GET auf die HA-REST-API; liefert Status, Bytes und Text
function Invoke-Ha([string]$base, [string]$path) {
    $req = [Net.HttpWebRequest]::Create($base + $path)
    $req.Method = 'GET'
    $req.Timeout = 8000
    $req.ReadWriteTimeout = 8000
    $req.UserAgent = 'BambuWatch'
    $req.Headers.Add('Authorization', "Bearer $haToken")
    if ($HaInsecure -or $TestInsecure) { $req.ServerCertificateValidationCallback = $script:certCb }
    $resp = $null
    try {
        $resp = $req.GetResponse()
    } catch {
        $ex = $_.Exception
        while ($null -ne $ex -and -not ($ex -is [Net.WebException])) { $ex = $ex.InnerException }
        if ($null -ne $ex -and $null -ne $ex.Response) { $resp = $ex.Response }
        else {
            # ganze Fehlerkette durchsehen: Zertifikatsproblem verstaendlich melden
            $all = ''; $x = $_.Exception
            while ($null -ne $x) { $all += ' ' + $x.Message; $x = $x.InnerException }
            if (($null -ne $ex -and ($ex.Status -eq [Net.WebExceptionStatus]::TrustFailure -or $ex.Status -eq [Net.WebExceptionStatus]::SecureChannelFailure)) -or $all -match '(?i)SSL|certificate|Zertifikat|trust') {
                throw "$base`: Zertifikat wird nicht akzeptiert (selbstsigniert? Option ""Zertifikat nicht prüfen"")"
            }
            $m = $_.Exception.Message
            if ($null -ne $_.Exception.InnerException) { $m = $_.Exception.InnerException.Message }
            throw "$base nicht erreichbar ($m)"
        }
    }
    $ms = New-Object IO.MemoryStream
    $resp.GetResponseStream().CopyTo($ms)
    $status = [int]$resp.StatusCode
    $resp.Close()
    $bytes = $ms.ToArray()
    return @{ Status = $status; Bytes = $bytes; Text = [Text.Encoding]::UTF8.GetString($bytes) }
}

function Run-HaCamera {
    $urls = @(Get-HaUrls) # @() haelt auch eine einzelne Adresse als Liste
    if ($urls.Count -eq 0 -or $haToken -eq '' -or $HaEntity -eq '') { Out-Line 'ERR|STOP|Home Assistant nicht vollständig eingerichtet'; return }
    $cur = 0; $slot = 0; $first = $true; $last = ''
    # ha-bambulab legt die A1/P1-Kamera je nach Option als camera.* oder als image.* an
    $proxy = $(if ($HaEntity.StartsWith('image.')) { '/api/image_proxy/' } else { '/api/camera_proxy/' })
    Out-Line 'STATE|connecting'
    while (Test-Parent) {
        $ok = $false
        for ($k = 0; $k -lt $urls.Count; $k++) {
            $idx = ($cur + $k) % $urls.Count
            try {
                $r = Invoke-Ha $urls[$idx] ($proxy + $HaEntity)
                if ($r.Status -eq 401) { Out-Line 'ERR|AUTH|Home-Assistant-Token ungültig'; return }
                if ($r.Status -eq 404) { Out-Line "ERR|STOP|Kamera $HaEntity gibt es in Home Assistant nicht"; return }
                if ($r.Status -ne 200 -or $r.Bytes.Length -lt 100) { throw "Home Assistant liefert kein Bild (HTTP $($r.Status) - Drucker aus?)" }
                $cur = $idx; $ok = $true
                $slot = ($slot + 1) % 3
                $path = Join-Path $OutDir ("bw_ha_$slot.jpg")
                [IO.File]::WriteAllBytes($path, $r.Bytes)
                if ($first) { Out-Line ('STATE|connected'); Out-Line ('VIA|' + $urls[$idx]); $first = $false }
                Out-Line "FRAME|$path"
                break
            } catch { $last = $_.Exception.Message }
        }
        if (-not $ok) {
            Out-Err $last
            $first = $true
            Sleep-Parent 5
        } else {
            for ($w = 0; $w -lt [Math]::Max(1, [int]($IntervalMs / 250)) -and (Test-Parent); $w++) { Start-Sleep -Milliseconds 250 }
        }
    }
}

function Run-HaList {
    $urls = @(Get-HaUrls) # @() haelt auch eine einzelne Adresse als Liste
    if ($urls.Count -eq 0 -or $haToken -eq '') { Out-Err 'Adresse und Token von Home Assistant eintragen'; return }
    $last = ''
    foreach ($u in $urls) {
        try {
            $r = Invoke-Ha $u '/api/states'
            if ($r.Status -eq 401) { Out-Line 'ERR|AUTH|Home-Assistant-Token ungültig'; return }
            if ($r.Status -ne 200) { throw "HTTP $($r.Status)" }
            $list = $r.Text | ConvertFrom-Json
            $n = 0
            foreach ($e in @($list)) {
                $id = [string]$e.entity_id
                if ($id.StartsWith('image.')) { Out-Line ('IMG|' + (Clean $id) + '|' + (Clean $e.attributes.friendly_name)); continue }
                if (-not $id.StartsWith('camera.')) { continue }
                $n++
                Out-Line ('CAM|' + (Clean $id) + '|' + (Clean $e.attributes.friendly_name))
            }
            Out-Line ('HAOK|' + $u + '|' + $n)
            Out-Line 'DONE'
            return
        } catch { $last = $_.Exception.Message }
    }
    Out-Err $last
}

# ================================================================== DRUCKOBJEKT (Cover)
# Bild speichern; alte Cover-Dateien vorher weg (BambuWatch liest die Datei sofort in den Speicher)
function Out-Cover([byte[]]$b, [string]$via, [string]$title) {
    Get-ChildItem -Path $OutDir -Filter 'bw_cover_*.img' -ErrorAction SilentlyContinue | ForEach-Object { try { Remove-Item $_.FullName -Force } catch {} }
    $path = Join-Path $OutDir ('bw_cover_' + [DateTime]::Now.Ticks + '.img')
    [IO.File]::WriteAllBytes($path, $b)
    Out-Line ('COVER|' + $path + '|' + $via + '|' + (Clean $title))
}

# Bild von einer Adresse ohne Anmeldung laden (Cover-URL der Bambu-Cloud ist vorsigniert)
function Get-Url([string]$url) {
    $req = [Net.HttpWebRequest]::Create($url)
    $req.Method = 'GET'
    $req.Timeout = 15000
    $req.ReadWriteTimeout = 15000
    $req.UserAgent = 'BambuWatch'
    if ($TestInsecure) { $req.ServerCertificateValidationCallback = $script:certCb }
    $resp = $null
    try { $resp = $req.GetResponse() } catch {
        $ex = $_.Exception
        while ($null -ne $ex -and -not ($ex -is [Net.WebException])) { $ex = $ex.InnerException }
        if ($null -ne $ex -and $null -ne $ex.Response) { $resp = $ex.Response } else { throw "Bild nicht ladbar ($($_.Exception.Message))" }
    }
    $ms = New-Object IO.MemoryStream
    $resp.GetResponseStream().CopyTo($ms)
    $status = [int]$resp.StatusCode
    $resp.Close()
    return @{ Status = $status; Bytes = $ms.ToArray() }
}

function Run-Cover {
    $msgs = New-Object Collections.Generic.List[string]
    $hadErr = $false; $tried = $false
    # 1. Home Assistant: Bild-Entitaet (ha-bambulab "Titelbild" / cover_image)
    $urls = @(Get-HaUrls)
    if ($HaEntity -ne '' -and $haToken -ne '' -and $urls.Count -gt 0) {
        $tried = $true
        $haMsg = ''; $haErr = $false
        foreach ($u in $urls) {
            # erste erreichbare Adresse zaehlt; nur deren Antwort entscheidet ueber Fehler / kein Bild
            try {
                $r = Invoke-Ha $u ('/api/image_proxy/' + $HaEntity)
                $haErr = $false
                if ($r.Status -eq 401) { $haMsg = 'HA: Token ungültig'; $haErr = $true; break }
                if ($r.Status -eq 404) { $haMsg = "HA: $HaEntity gibt es nicht"; break }
                if ($r.Status -eq 200 -and $r.Bytes.Length -ge 100) { Out-Cover $r.Bytes 'HA' ''; return }
                $haMsg = "HA: kein Titelbild (HTTP $($r.Status))"
                break
            } catch { $haMsg = 'HA: ' + $_.Exception.Message; $haErr = $true }
        }
        if ($haErr) { $hadErr = $true }
        $msgs.Add($haMsg)
    }
    # 2. Bambu-Cloud: letzter Auftrag dieses Druckers
    if ($token -ne '' -and $Serial -ne '') {
        $tried = $true
        try {
            $r = Invoke-Bambu 'GET' ($apiBase + '/v1/user-service/my/tasks?deviceId=' + [Uri]::EscapeDataString($Serial) + '&limit=1') $null $token
            if ($r.Status -eq 401 -or $r.Status -eq 403) { throw 'Anmeldung abgelaufen' }
            if ($r.Status -ne 200) { throw "HTTP $($r.Status)" }
            $hit = $null
            foreach ($h in @(Get-JsonProp $r.Json @('hits'))) {
                if ($null -eq $h) { continue }
                $dev = [string](Get-JsonProp $h @('deviceId'))
                if ($dev -ne '' -and $dev -ne $Serial) { continue }
                $hit = $h; break
            }
            $cov = $(if ($null -ne $hit) { [string](Get-JsonProp $hit @('cover')) } else { '' })
            if ($cov -eq '') { $msgs.Add('Cloud: kein Auftrag mit Bild') }
            else {
                $img = Get-Url $cov
                if ($img.Status -ne 200 -or $img.Bytes.Length -lt 100) { throw "Bild nicht ladbar (HTTP $($img.Status))" }
                Out-Cover $img.Bytes 'Cloud' ([string](Get-JsonProp $hit @('title', 'designTitle')))
                return
            }
        } catch {
            $m = $_.Exception.Message
            if ($m -eq 'CLOUDFLARE') { $m = 'vom Bambu-Schutz geblockt' }
            $msgs.Add('Cloud: ' + $m); $hadErr = $true
        }
    }
    if (-not $tried) { Out-Line 'NOCOVER|weder Home-Assistant-Titelbild noch Bambu-Konto eingerichtet'; return }
    $txt = Clean ($msgs -join ' · ')
    if ($hadErr) { Out-Line ('ERR|' + $txt) } else { Out-Line ('NOCOVER|' + $txt) }
}

# ================================================================== LICHT
function Get-LedCmd([string]$state) {
    return '{"system":{"sequence_id":"0","command":"ledctrl","led_node":"chamber_light","led_mode":"' + $state + '","led_on_time":500,"led_off_time":500,"loop_times":0,"interval_time":0}}'
}

# Heimnetz: kurz per MQTT anmelden, Befehl senden, abmelden
function Run-Light {
    if ($LightState -eq '') { Out-Err 'Licht: Zustand fehlt'; return }
    if ($Serial -eq '') { Out-Err 'Seriennummer fehlt'; return }
    $c = $null
    try {
        $c = Open-Tls $Ip 8883 6000
        Mqtt-Connect $c.Ssl 'bblp' $code
        $first = (Read-Exact $c.Ssl 1)[0]
        $pk = Mqtt-ReadRest $c.Ssl $first
        if ($pk.Type -ne 0x20 -or $pk.Body[1] -ne 0) { throw 'MQTT-Login abgelehnt - Access Code pruefen' }
        Mqtt-Publish $c.Ssl "device/$Serial/request" (Get-LedCmd $LightState)
        Start-Sleep -Milliseconds 300
        Send-Packet $c.Ssl ([byte[]](0xE0, 0x00)) # DISCONNECT
        Out-Line "LIGHT|$LightState"
    } catch { Out-Err ('Licht: ' + $_.Exception.Message) } finally { Close-Tls $c }
}

# POST auf die HA-REST-API (Dienst aufrufen)
function Invoke-HaPost([string]$base, [string]$path, [string]$json) {
    $req = [Net.HttpWebRequest]::Create($base + $path)
    $req.Method = 'POST'
    $req.Timeout = 8000
    $req.ReadWriteTimeout = 8000
    $req.UserAgent = 'BambuWatch'
    $req.ContentType = 'application/json'
    $req.Headers.Add('Authorization', "Bearer $haToken")
    if ($HaInsecure -or $TestInsecure) { $req.ServerCertificateValidationCallback = $script:certCb }
    $b = [Text.Encoding]::UTF8.GetBytes($json)
    $req.ContentLength = $b.Length
    $rs = $req.GetRequestStream(); $rs.Write($b, 0, $b.Length); $rs.Close()
    try { $resp = $req.GetResponse() } catch {
        $ex = $_.Exception
        while ($null -ne $ex -and -not ($ex -is [Net.WebException])) { $ex = $ex.InnerException }
        if ($null -ne $ex -and $null -ne $ex.Response) { $resp = $ex.Response } else { throw "$base nicht erreichbar" }
    }
    $status = [int]$resp.StatusCode
    $resp.Close()
    return $status
}

function Run-HaLight {
    $urls = @(Get-HaUrls)
    if ($urls.Count -eq 0 -or $haToken -eq '' -or $HaEntity -eq '' -or $LightState -eq '') { Out-Err 'Licht: Home Assistant nicht vollständig eingerichtet'; return }
    $svc = $(if ($LightState -eq 'on') { 'turn_on' } else { 'turn_off' })
    $last = ''
    foreach ($u in $urls) {
        try {
            $st = Invoke-HaPost $u "/api/services/light/$svc" ('{"entity_id":"' + $HaEntity + '"}')
            if ($st -eq 401) { Out-Line 'ERR|AUTH|Home-Assistant-Token ungültig'; return }
            if ($st -ne 200) { throw "HTTP $st" }
            Out-Line "LIGHT|$LightState"
            return
        } catch { $last = $_.Exception.Message }
    }
    Out-Err ('Licht: ' + $last)
}

# ================================================================== HOME ASSISTANT: STATUS (WebSocket)
$script:wsBuf = New-Object byte[] 65536
$script:wsTask = $null
$script:wsMs = $null

function Ws-Send($ws, [string]$txt) {
    $b = [Text.Encoding]::UTF8.GetBytes($txt)
    $t = $ws.SendAsync([ArraySegment[byte]]::new($b), [Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None)
    if (-not $t.Wait(10000)) { throw 'Home Assistant antwortet nicht (Senden)' }
}

# naechste Nachricht als Text; $null, wenn in $ms nichts angefangen hat
function Ws-Recv($ws, [int]$ms) {
    if ($null -eq $script:wsMs) { $script:wsMs = New-Object IO.MemoryStream }
    while ($true) {
        if ($null -eq $script:wsTask) {
            $script:wsTask = $ws.ReceiveAsync([ArraySegment[byte]]::new($script:wsBuf), [Threading.CancellationToken]::None)
        }
        # angefangene Nachricht: auf den Rest bis 30 s warten
        $wait = $(if ($script:wsMs.Length -gt 0) { 30000 } else { $ms })
        if (-not $script:wsTask.Wait($wait)) {
            if ($script:wsMs.Length -gt 0) { throw 'Home Assistant: Nachricht unvollständig' }
            return $null
        }
        $r = $script:wsTask.Result
        $script:wsTask = $null
        if ($r.MessageType -eq [Net.WebSockets.WebSocketMessageType]::Close) { throw 'Home Assistant hat die Verbindung geschlossen' }
        $script:wsMs.Write($script:wsBuf, 0, $r.Count)
        if ($r.EndOfMessage) {
            $txt = [Text.Encoding]::UTF8.GetString($script:wsMs.ToArray())
            $script:wsMs = New-Object IO.MemoryStream
            return $txt
        }
    }
}

# wartet auf die Antwort zu einer Befehls-ID (Events dazwischen werden verworfen)
function Ws-Result($ws, [int]$id, [int]$ms) {
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while ($sw.ElapsedMilliseconds -lt $ms) {
        $t = Ws-Recv $ws 1000
        if ($null -eq $t) { if (-not (Test-Parent)) { throw 'Ende' }; continue }
        $j = $t | ConvertFrom-Json
        if ($j.type -eq 'result' -and [int]$j.id -eq $id) {
            if (-not $j.success) { throw ('Home Assistant lehnt ab: ' + (Clean $j.error.message)) }
            return , $j.result
        }
    }
    throw 'Home Assistant antwortet nicht'
}

# Schluessel aus ha-bambulab, die BambuWatch braucht (unique_id = <Serie>_<Schluessel>)
$script:haWant = @('print_status', 'print_progress', 'remaining_time', 'current_layer', 'total_layers', 'nozzle_temp', 'target_nozzle_temp',
    'bed_temp', 'target_bed_temp', 'subtask_name', 'speed_profile', 'ip_address', 'left_nozzle_temp', 'left_target_nozzle_temp',
    'right_nozzle_temp', 'right_target_nozzle_temp', 'chamber_light', 'camera_light', 'camera', 'p1p_camera', 'cover_image')
$script:haMap = @{}    # entity_id -> Schluessel (bzw. "tray|<ams>|<slot>" / "ext|<n>")
$script:haVal = @{}    # Schluessel -> @{ s = Zustand; a = Attribute }

function Ha-Num($k, [string]$fmt = '0') {
    $v = $script:haVal[$k]
    if ($null -eq $v) { return '' }
    $s = [string]$v.s
    if ($s -eq '' -or $s -eq 'unknown' -or $s -eq 'unavailable') { return '' }
    try { $d = [double]::Parse($s, $inv) } catch { return '' }
    $u = ''
    if ($null -ne $v.a) { $u = [string]$v.a.unit_of_measurement }
    if ($u -eq (([string][char]0xB0) + 'F')) { $d = ($d - 32) * 5 / 9 }   # Fahrenheit -> Celsius
    return $d.ToString($fmt, $inv)
}

function Ha-Str($k) {
    $v = $script:haVal[$k]
    if ($null -eq $v) { return '' }
    $s = [string]$v.s
    if ($s -eq 'unknown' -or $s -eq 'unavailable') { return '' }
    return $s
}

# Restzeit in Minuten; HA kann die Einheit umstellen (ha-bambulab schlaegt Stunden vor)
function Ha-Minutes($k) {
    $v = $script:haVal[$k]
    $n = Ha-Num $k '0.###'
    if ($n -eq '') { return '' }
    $d = [double]::Parse($n, $inv)
    switch ([string]$v.a.unit_of_measurement) {
        'h' { $d *= 60 } 's' { $d /= 60 } 'd' { $d *= 1440 }
    }
    return ([Math]::Round($d)).ToString($inv)
}

function Out-HaStatus {
    $st = (Ha-Str 'print_status').ToUpper()
    $spdMap = @{ silent = '1'; standard = '2'; sport = '3'; ludicrous = '4' }
    $spd = $spdMap[(Ha-Str 'speed_profile')]
    # AMS-Slots und aktives Filament aus den Slot-Sensoren (Attribute type, color, active)
    $amsOrder = New-Object Collections.Generic.List[string]
    $list = New-Object Collections.Generic.List[string]
    $ext = New-Object Collections.Generic.List[string]
    $c = ''; $t = ''; $sl = ''; $am = ''
    foreach ($k in ($script:haVal.Keys | Where-Object { $_ -like 'tray|*' -or $_ -like 'ext|*' } | Sort-Object)) {
        $v = $script:haVal[$k]
        $a = $v.a
        $typ = ''; $col = ''; $act = $false
        if ($null -ne $a) {
            if ($a.empty -ne $true) { $typ = [string]$a.type }
            $col = ([string]$a.color).TrimStart('#'); if ($col.Length -ge 6) { $col = $col.Substring(0, 6) }
            $act = ($a.active -eq $true)
        }
        $p = $k.Split('|')
        if ($p[0] -eq 'tray') {
            if (-not $amsOrder.Contains($p[1])) { $amsOrder.Add($p[1]) }
            $un = [string]($amsOrder.IndexOf($p[1]) + 1)
            $list.Add($un + '.' + $p[2] + ',' + ((Clean $typ) -replace '[,;]', ' ') + ',' + $col + ',' + $(if ($act) { '1' } else { '0' }))
            if ($act) { $c = $col; $t = $typ; $sl = $p[2]; $am = $un }
        } else {
            if ($typ -ne '') { $ext.Add('E,' + ((Clean $typ) -replace '[,;]', ' ') + ',' + $col + ',' + $(if ($act) { '1' } else { '0' })) }
            if ($act) { $c = $col; $t = $typ; $sl = 'Ext'; $am = '' }
        }
    }
    foreach ($x in $ext) { $list.Add($x) } # externe Spule ans Ende
    $dual = ($script:haVal.ContainsKey('left_nozzle_temp'))
    $light = Ha-Str 'chamber_light'; if ($light -eq '') { $light = Ha-Str 'camera_light' }
    $f = [ordered]@{
        st = Clean $st; pct = Ha-Num 'print_progress'; rem = Ha-Minutes 'remaining_time'
        lay = Ha-Num 'current_layer'; tlay = Ha-Num 'total_layers'
        noz = Ha-Num 'nozzle_temp'; nozt = Ha-Num 'target_nozzle_temp'; bed = Ha-Num 'bed_temp'; bedt = Ha-Num 'target_bed_temp'
        job = Clean (Ha-Str 'subtask_name'); err = ''; spd = [string]$spd
        col = Clean $c; fil = Clean $t; slot = Clean $sl; ams = Clean $am
        ip = Clean (Ha-Str 'ip_address'); cam = ''; dual = $(if ($dual) { '1' } else { '' })
        nozR = Ha-Num 'right_nozzle_temp'; noztR = Ha-Num 'right_target_nozzle_temp'
        nozL = Ha-Num 'left_nozzle_temp'; noztL = Ha-Num 'left_target_nozzle_temp'; act = ''
        light = Clean $light; amsl = Clean ($list -join ';')
    }
    $line = 'STATUS'
    foreach ($k in $f.Keys) { $line += "|$k=" + $f[$k] }
    if ($line -ne $script:lastOut) { Out-Line $line; $script:lastOut = $line }
}

function Ha-Apply($eid, $state) {
    $k = $script:haMap[$eid]
    if ($null -eq $k) { return $false }
    if ($null -eq $state) { $script:haVal.Remove($k); return $true }
    $script:haVal[$k] = @{ s = [string]$state.state; a = $state.attributes }
    return $true
}

function Run-HaStatus {
    $urls = @(Get-HaUrls)
    if ($urls.Count -eq 0 -or $haToken -eq '') { Out-Line 'ERR|STOP|Home Assistant nicht eingerichtet'; return }
    if ($Serial -eq '') { Out-Line 'ERR|STOP|Seriennummer fehlt (Einstellungen)'; return }
    $ser = $Serial.ToUpper()
    $cur = 0
    while (Test-Parent) {
        $ws = $null; $last = ''
        $script:wsTask = $null; $script:wsMs = $null; $script:lastOut = ''
        try {
            Out-Line 'STATE|connecting'
            for ($k = 0; $k -lt $urls.Count -and $null -eq $ws; $k++) {
                $idx = ($cur + $k) % $urls.Count
                $wsUrl = ($urls[$idx] -replace '^http', 'ws') + '/api/websocket'
                $try = New-Object Net.WebSockets.ClientWebSocket
                if ($HaInsecure -or $TestInsecure) {
                    # .NET Framework: globale Pruefung; neueres .NET: eigene Eigenschaft
                    [Net.ServicePointManager]::ServerCertificateValidationCallback = $script:certCb
                    if ($null -ne $try.Options.PSObject.Properties['RemoteCertificateValidationCallback']) { $try.Options.RemoteCertificateValidationCallback = $script:certCb }
                }
                try {
                    $t = $try.ConnectAsync([Uri]$wsUrl, [Threading.CancellationToken]::None)
                    if (-not $t.Wait(8000)) { throw 'keine Antwort' }
                    $ws = $try; $cur = $idx
                } catch {
                    $m = $_.Exception; while ($null -ne $m.InnerException) { $m = $m.InnerException }
                    $last = $urls[$idx] + ': ' + $m.Message
                    try { $try.Dispose() } catch {}
                }
            }
            if ($null -eq $ws) { throw "Home Assistant nicht erreichbar ($last)" }
            # Anmeldung
            $j = (Ws-Recv $ws 8000) | ConvertFrom-Json
            if ($null -eq $j -or $j.type -ne 'auth_required') { throw 'Unerwartete Antwort von Home Assistant' }
            Ws-Send $ws ('{"type":"auth","access_token":"' + $haToken + '"}')
            $j = (Ws-Recv $ws 8000) | ConvertFrom-Json
            if ($null -ne $j -and $j.type -eq 'auth_invalid') { Out-Line 'ERR|AUTH|Home-Assistant-Token ungültig'; return }
            if ($null -eq $j -or $j.type -ne 'auth_ok') { throw 'Anmeldung bei Home Assistant fehlgeschlagen' }
            # Entitaeten des Druckers ueber die unique_id finden
            Ws-Send $ws '{"id":1,"type":"config/entity_registry/list"}'
            $reg = Ws-Result $ws 1 30000
            $script:haMap = @{}; $script:haVal = @{}
            $found = @{}
            foreach ($e in @($reg)) {
                if ($null -eq $e -or [string]$e.platform -ne 'bambu_lab') { continue }
                $uid = ([string]$e.unique_id).ToUpper()
                $eid = [string]$e.entity_id
                if ($uid.StartsWith($ser + '_')) {
                    $key = ([string]$e.unique_id).Substring($ser.Length + 1)
                    if ($script:haWant -contains $key) {
                        # Kamera gibt es als camera.* oder image.* (Option in ha-bambulab)
                        $script:haMap[$eid] = $key
                        $found[$key] = $eid
                    }
                } elseif ($uid -match ('_' + [regex]::Escape($ser) + '_AMS_(.+)_TRAY_(\d)$')) {
                    $script:haMap[$eid] = 'tray|' + $Matches[1] + '|' + $Matches[2]
                } elseif ($uid -match ('_' + [regex]::Escape($ser) + '_EXTERNALSPOOL(\d?)_EXTERNAL_SPOOL$')) {
                    $script:haMap[$eid] = 'ext|' + $Matches[1]
                }
            }
            if (-not $found.ContainsKey('print_status')) {
                Out-Line "ERR|STOP|Drucker $Serial ist in Home Assistant nicht eingebunden (Integration ha-bambulab)"
                return
            }
            if ($found.ContainsKey('camera')) { Out-Line ('HAENT|cam|' + $found['camera']) }
            elseif ($found.ContainsKey('p1p_camera')) { Out-Line ('HAENT|cam|' + $found['p1p_camera']) }
            if ($found.ContainsKey('cover_image')) { Out-Line ('HAENT|cover|' + $found['cover_image']) }
            if ($found.ContainsKey('chamber_light')) { Out-Line ('HAENT|light|' + $found['chamber_light']) }
            elseif ($found.ContainsKey('camera_light')) { Out-Line ('HAENT|light|' + $found['camera_light']) }
            # aktuelle Zustaende, dann Aenderungen abonnieren
            Ws-Send $ws '{"id":2,"type":"get_states"}'
            $states = Ws-Result $ws 2 30000 # erst zuweisen: @(Funktion) wuerde die Liste nochmal einpacken
            foreach ($s in @($states)) { if ($null -ne $s) { $null = Ha-Apply ([string]$s.entity_id) $s } }
            Ws-Send $ws '{"id":3,"type":"subscribe_events","event_type":"state_changed"}'
            $null = Ws-Result $ws 3 15000
            Out-Line 'STATE|connected'
            Out-Line ('VIA|' + $urls[$cur])
            Out-HaStatus
            $id = 10
            $ping = [Diagnostics.Stopwatch]::StartNew(); $silence = [Diagnostics.Stopwatch]::StartNew()
            $dirty = $false; $flush = [Diagnostics.Stopwatch]::StartNew()
            while (Test-Parent) {
                $t = Ws-Recv $ws 500
                if ($null -ne $t) {
                    $silence.Restart()
                    # schneller Vorfilter: nur Nachrichten, die eine unserer Entitaeten nennen
                    if ($t.Contains('"event"')) {
                        $m = [regex]::Match($t, '"entity_id"\s*:\s*"([^"]+)"')
                        if ($m.Success -and $script:haMap.ContainsKey($m.Groups[1].Value)) {
                            $j = $t | ConvertFrom-Json
                            if ($null -ne $j.event -and $null -ne $j.event.data) {
                                if (Ha-Apply ([string]$j.event.data.entity_id) $j.event.data.new_state) { $dirty = $true }
                            }
                        }
                    }
                }
                # Aenderungen kommen oft im Pulk -> hoechstens 2x pro Sekunde ausgeben
                if ($dirty -and $flush.ElapsedMilliseconds -ge 500) { Out-HaStatus; $dirty = $false; $flush.Restart() }
                if ($ping.Elapsed.TotalSeconds -ge 30) {
                    $id++; Ws-Send $ws ('{"id":' + $id + ',"type":"ping"}'); $ping.Restart()
                    # Lebenszeichen: Status auch ohne Aenderung erneut melden (sonst gilt er als veraltet)
                    $script:lastOut = ''; Out-HaStatus; $dirty = $false
                }
                if ($silence.Elapsed.TotalSeconds -ge 75) { throw 'Keine Antwort mehr von Home Assistant' }
            }
        } catch {
            $m = $_.Exception; while ($null -ne $m.InnerException) { $m = $m.InnerException }
            Out-Err $m.Message
        } finally {
            if ($null -ne $ws) { try { $ws.Abort(); $ws.Dispose() } catch {} }
        }
        Sleep-Parent 10
    }
}

# ================================================================== Start
try {
    if (@('Camera', 'Mqtt', 'Probe', 'Light') -contains $Mode -and $Ip -eq '') { Out-Err 'IP-Adresse fehlt (Einstellungen)'; exit 1 }
    switch ($Mode) {
        'Camera' {
            if ($code -eq '') { Out-Err 'Access Code fehlt (Einstellungen)'; exit 1 }
            Run-Camera
        }
        'Mqtt' {
            if ($code -eq '') { Out-Err 'Access Code fehlt (Einstellungen)'; exit 1 }
            Run-MqttLoop $Ip 8883 $false 'bblp' $code $false
        }
        'CloudMqtt' {
            if ($token -eq '' -or $cloudUser -eq '') { Out-Line 'ERR|AUTH|Nicht bei Bambu angemeldet'; exit 1 }
            Run-MqttLoop $cloudBroker $cloudPort $cloudVerify $cloudUser $token $true
        }
        'Probe' {
            if ($code -eq '') { Out-Err 'Access Code fehlt (Einstellungen)'; exit 1 }
            Run-Probe
        }
        'HaCamera' { Run-HaCamera }
        'HaList' { Run-HaList }
        'Cover' { Run-Cover }
        'HaStatus' { Run-HaStatus }
        'HaLight' { Run-HaLight }
        'Light' {
            if ($code -eq '') { Out-Err 'Access Code fehlt (Einstellungen)'; exit 1 }
            Run-Light
        }
        default { Run-Cloud }
    }
} catch {
    if ($_.Exception.Message -eq 'CLOUDFLARE') {
        Out-Err 'Vom Bambu-Schutz (Cloudflare) geblockt - bitte den Weg "Token aus dem Browser" nutzen'
    } else {
        Out-Err $_.Exception.Message
    }
    exit 1
}
