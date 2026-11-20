; =============================================================================
;  BambuWatch  v1.7
;  Kleines, frei skalierbares Fenster mit dem Live-Bild von Bambu-Lab-Druckern
;  (A1, A1 mini, P1 als Einzelbilder; H2D, X1, P2S als Videostream ueber ffmpeg),
;  mehrere Drucker umschaltbar (Klick auf den Namen oben links), mit optionalem
;  Druckstatus als Overlay: Fortschritt, Restzeit, Fertig-Uhrzeit, Schicht,
;  Temperaturen, aktives Filament. Meldung bei Druckende, Pause oder Abbruch.
;
;  Verbindung:
;   - Kamera:  nur im Heimnetz, TLS Port 6000, JPEG-Einzelbilder   (Community-Protokoll)
;   - Status:  im Heimnetz direkt vom Drucker (MQTT, Port 8883), unterwegs ueber
;              die Bambu-Cloud (us/cn.mqtt.bambulab.com) mit dem Bambu-Konto
;   - Bambu-Login liefert alle Drucker mit Seriennummer + Access Code, die IP kommt
;     aus dem Status (Ablauf wie ha-bambulab / BambuHelper, siehe Helfer).
;   - Bild alternativ ueber Home Assistant (REST-API /api/camera_proxy), z. B. unterwegs.
;   - Alles pro Drucker einstellbar: Kamera an/aus, Status an/aus, Status-Quelle,
;     Bildquelle, Kameratyp, HA-Kamera, Druckobjekt-Bild.
;   - Status auch ueber Home Assistant (ha-bambulab, WebSocket): Reihenfolge Heimnetz ->
;     Home Assistant -> Bambu-Cloud. Unterwegs werden Heimnetz-Versuche seltener.
;   - Fortschritt in der Taskleiste, Meldung mit Bild, Druckerlicht, AMS-Uebersicht.
;   - Kommt kein Kamerabild, zeigt BambuWatch das Bild des Druckobjekts: Titelbild aus
;     Home Assistant (ha-bambulab) oder vom letzten Auftrag aus der Bambu-Cloud.
;  AutoIt kann kein TLS ueber Sockets, deshalb erledigt das ein kleiner
;  PowerShell-Helfer (BambuWatch_helper.ps1, steckt nach dem Kompilieren in der EXE).
;
;  Dateien, die beim Kompilieren neben diesem Skript liegen muessen:
;     BambuWatch_helper.ps1   BambuWatch.ico
;
;  Benoetigt: AutoIt 3.3.16 oder neuer (Map-Datentyp), Windows 10/11
;  Einstellungen: BambuWatch.ini neben dem Skript (Access Code per Windows-DPAPI
;  verschluesselt, nur fuer dein Windows-Konto lesbar).
; =============================================================================

#AutoIt3Wrapper_Icon=BambuWatch.ico
; Version: hier UND in $APP_VERSION pflegen (Wrapper-Direktiven koennen keine Variablen lesen)
#AutoIt3Wrapper_Outfile=BambuWatch_v1.7.exe
#AutoIt3Wrapper_Res_Fileversion=1.7.0.0
#AutoIt3Wrapper_Res_ProductVersion=1.7
#AutoIt3Wrapper_Res_Description=BambuWatch - Live-Bild Bambu Lab
#AutoIt3Wrapper_Res_ProductName=BambuWatch

#include <AutoItConstants.au3>
#include <FileConstants.au3>
#include <StringConstants.au3>
#include <GUIConstantsEx.au3>
#include <WindowsConstants.au3>
#include <StaticConstants.au3>
#include <EditConstants.au3>
#include <TrayConstants.au3>
#include <GDIPlus.au3>
#include <Date.au3>
#include <Misc.au3>

Opt("GUIOnEventMode", 1)
Opt("MustDeclareVars", 1)
Opt("TrayMenuMode", 3)        ; kein Standard-Traymenue, kein Auto-Haken
Opt("TrayOnEventMode", 1)
Opt("GUICloseOnESC", 0)

; ---------------------------------------------------------------- Version
Global Const $APP_VERSION = "1.7"
Global Const $APP_NAME = "BambuWatch"

; ---------------------------------------------------------------- nur eine Instanz
If _Singleton("BambuWatch_Micha_SingleInstance", 1) = 0 Then
	Local $hOther = WinGetHandle("[REGEXPTITLE:^BambuWatch; CLASS:AutoIt v3 GUI]")
	If $hOther <> "" Then
		WinSetState($hOther, "", @SW_SHOW)
		WinSetState($hOther, "", @SW_RESTORE)
		WinActivate($hOther)
	EndIf
	Exit
EndIf

; ---------------------------------------------------------------- Dateien
Global Const $WORK = @TempDir & "\BambuWatch"
DirCreate($WORK)
Global Const $HELPER = $WORK & "\BambuWatch_helper.ps1"
Global Const $ICO = $WORK & "\BambuWatch.ico"
; FileInstall: kompiliert aus der EXE, unkompiliert einfache Kopie aus dem Skriptordner
FileInstall("BambuWatch_helper.ps1", $HELPER, 1)
FileInstall("BambuWatch.ico", $ICO, 1)
Global $INI = @ScriptDir & "\BambuWatch.ini"
If Not __DirWritable(@ScriptDir) Then ; z. B. unter "Programme" -> AppData
	DirCreate(@AppDataDir & "\BambuWatch")
	$INI = @AppDataDir & "\BambuWatch\BambuWatch.ini"
EndIf

; ---------------------------------------------------------------- Farben (GUI = RGB, GDI+ = ARGB)
Global Const $C_BG = 0x1B1D21       ; Fensterrahmen + Leiste
Global Const $C_PANEL = 0x24272D    ; Eingabefelder
Global Const $C_LINE = 0x363A42     ; Linien, Feldrahmen
Global Const $C_TXT = 0xE8EAED
Global Const $C_TXT2 = 0x9AA0A8
Global Const $C_GREEN = 0x00AE42    ; Bambu-Gruen
Global Const $C_AMBER = 0xF5A524
Global Const $C_RED = 0xE5484D
Global Const $C_BLUE = 0x3B9EFF
Global Const $C_OFF = 0x5A5F68
Global Const $BTN_GREEN = 0xFF00AE42, $BTN_DARK = 0xFF3A3F48
Global Const $GLYPH_FONT = "Segoe MDL2 Assets" ; Symbolschrift von Windows 10/11

; ---------------------------------------------------------------- Windows-Nachrichten (eigene Namen, keine Kollision mit UDFs)
Global Const $BW_WM_SIZE = 0x0005, $BW_WM_GETMINMAXINFO = 0x0024, $BW_WM_NCHITTEST = 0x0084
Global Const $BW_WM_NCLBUTTONDOWN = 0x00A1, $BW_WM_SIZING = 0x0214
Global Const $BW_STM_SETIMAGE = 0x0172, $BW_STM_GETIMAGE = 0x0173, $BW_IMAGE_BITMAP = 0
Global Const $BW_EM_SETPASSWORDCHAR = 0x00CC

; ---------------------------------------------------------------- Layout Hauptfenster
Global Const $BAR_H = 32   ; Titelleiste
Global Const $PAD = 6      ; Rahmen um das Bild (= Greifzone zum Groesse aendern)
Global Const $MIN_W = 430, $MIN_H = 260

; ---------------------------------------------------------------- Einstellungen
Global $g_sIp = "", $g_sCode = "", $g_sSerial = ""
Global $g_bCamOn = True, $g_bStatusOn = True ; aktiver Drucker (pro Profil gespeichert)
Global $g_iOverlay = 1          ; 0 aus, 1 kompakt, 2 ausfuehrlich
Global $g_bAspect = True        ; Seitenverhaeltnis beim Ziehen halten
Global $g_bOnTop = False
Global $g_bTray = True          ; Minimieren in den Infobereich
Global $g_iAlpha = 255
Global $g_bNotify = True, $g_bSound = True
Global $g_bStudioWatch = True     ; pausieren, solange Bambu Studio laeuft
Global $g_sSnapDir = @MyDocumentsDir & "\BambuWatch"
Global $g_iFontAdd = 1          ; Schrift groesser: 0..3 Pixel (gilt ueberall)
Global Const $FONT_NAMES[4] = ["Standard", "+1 px", "+2 px", "+3 px"]
Global $g_iX = -1, $g_iY = -1, $g_iW = 560, $g_iH = 0
; Bambu-Konto (fuer Status von unterwegs und automatische Einrichtung)
Global $g_sToken = "", $g_sUser = "", $g_sEmail = "", $g_sRegion = "Global", $g_sPrinterName = ""
Global $g_iConnMode = 0         ; 0 automatisch, 1 nur Heimnetz, 2 nur Bambu-Cloud, 3 nur Home Assistant (aktiver Drucker)
; Druckerprofile: Zeilen 1..$g_iPrnN, Spalten siehe $PF_*
Global Enum $PF_NAME, $PF_IP, $PF_CODE, $PF_SERIAL, $PF_MODEL, $PF_CAMT, $PF_IMG, $PF_HACAM, $PF_CONN, _
		$PF_CAMON, $PF_STON, $PF_COVER, $PF_HACOVER, $PF_HALIGHT, $PF_COUNT
Global $g_aPrn[1][$PF_COUNT], $g_iPrnN = 0, $g_iPrn = 1
; Werte des aktiven Druckers (zusaetzlich zu IP, Code, Seriennummer, Name, Status-Quelle)
Global $g_sModel = "", $g_iCamType = 0, $g_iImgSrc = 0, $g_sHaCam = ""
; Druckobjekt-Bild statt Kamerabild (an/aus) und HA-Bild-Entitaet dafuer (z. B. image.a1_..._titelbild)
Global $g_bCoverOn = True, $g_sHaCover = ""
; Licht-Entitaet in Home Assistant (findet der HA-Status selbst)
Global $g_sHaLight = ""
Global Const $CAMTYPE_KEYS[3] = ["auto", "jpeg", "rtsp"]
Global Const $CAMTYPE_NAMES[3] = ["Automatisch erkennen", "Einzelbilder (A1, P1)", "Videostream (H2D, X1, P2S)"]
Global Const $IMGSRC_NAMES[3] = ["Automatisch", "Nur Drucker (Heimnetz)", "Nur Home Assistant"]
; Home Assistant (fuer alle Drucker gemeinsam) und ffmpeg
Global $g_sHaUrlIn = "", $g_sHaUrlOut = "", $g_sHaToken = "", $g_bHaInsecure = False
Global $g_sFfmpeg = ""
Global Const $REGION_KEYS[2] = ["Global", "China"]
Global Const $REGION_NAMES[2] = ["Europa / USA", "China"]
Global Const $CONN_NAMES[4] = ["Automatisch", "Nur Heimnetz", "Nur Bambu-Cloud", "Nur Home Assistant"]
Global Const $LAN_GRACE_MS = 15000 ; so lange darf das Heimnetz nach dem Start brauchen, bevor die Cloud uebernimmt
Global Const $LAN_STALE_MS = 30000 ; so lange ohne Heimnetz-Daten -> Cloud uebernimmt
Global Const $OVL_NAMES[3] = ["Aus", "Kompakt", "Ausführlich"]
Global Const $ALPHA_STEPS[4] = [255, 230, 204, 178]
Global Const $ALPHA_NAMES[4] = ["100 %", "90 %", "80 %", "70 %"]
_SettingsLoad()

; ---------------------------------------------------------------- Laufzeit-Zustand
; Helfer-Prozesse
Global $g_iPidCam = 0, $g_iPidMq = 0, $g_iPidCl = 0, $g_iPidProbe = 0, $g_iPidAcc = 0
Global $g_sBufCam = "", $g_sBufMq = "", $g_sBufCl = "", $g_sBufProbe = "", $g_sBufAcc = ""
Global $g_tRestartCam = 0, $g_tRestartMq = 0, $g_tRestartCl = 0 ; Timer bis zum Neustart (0 = keiner)
; Quelle des Druckstatus: "LAN" (Heimnetz) oder "Cloud"
Global $g_sSrc = "", $g_tLanSt = 0, $g_tLanStart = 0
Global $g_iCl = 0, $g_sClMsg = "", $g_bClAuthBad = False
; Status ueber Home Assistant (WebSocket)
Global $g_iPidHs = 0, $g_sBufHs = "", $g_iHs = 0, $g_sHsMsg = "", $g_tRestartHs = 0, $g_bHsStop = False, $g_tHsSt = 0
; Laufzeit der Heimnetz-Helfer (unterwegs werden sie nach kurzer Zeit beendet und seltener gestartet)
Global $g_tCamRun = 0, $g_tMqRun = 0
; Licht schalten, Taskleiste, Meldungsfenster
Global $g_iPidLight = 0, $g_sBufLight = "", $g_tLight = 0
Global $g_oTaskbar = 0, $g_iTbState = -1, $g_iTbVal = -1
Global $g_hPop = 0, $g_idPopPic = 0, $g_tPop = 0
; Kamerabild: Drucker (Heimnetz) oder Home Assistant
Global $g_iPidHa = 0, $g_sBufHa = "", $g_iHa = 0, $g_sHaMsg = "", $g_tRestartHa = 0, $g_bHaStop = False
Global $g_tLanFrame = 0, $g_tCamStart = 0, $g_sImgVia = "", $g_bCamStop = False, $g_sCamTypeNow = ""
Global $g_iPidHaList = 0, $g_sBufHaList = "", $g_tHaList = 0
; Kamera: 0 aus, 1 verbinde, 2 verbunden (wartet auf Bild), 3 Bilder kommen, 4 Fehler
Global $g_iCam = 0, $g_sCamMsg = ""
Global $g_iMq = 0, $g_sMqMsg = ""
Global $g_hFrame = 0, $g_bFrame = Binary(""), $g_tFrame = 0, $g_fAspect = 16 / 9
; Druckobjekt-Bild (Ersatz, wenn kein Kamerabild kommt)
Global $g_iPidCov = 0, $g_sBufCov = "", $g_hCover = 0, $g_sCovKey = "", $g_sCovKeyLoaded = "", $g_tCovTry = 0
Global $g_sCovVia = "", $g_sCovTitle = "", $g_sCovMsg = ""
Global $g_bForceCover = False ; Umschalter: Druckobjekt statt Kamerabild
Global $g_bStudioPause = False ; Bambu Studio laeuft -> alle Verbindungen getrennt
Global $g_mSt[]                 ; Druckstatus (Schluessel siehe Helfer: st, pct, rem, ...)
Global $g_bHaveSt = False, $g_tSt = 0, $g_sPrevState = ""
Global $g_sToast = "", $g_tToast = 0
Global $g_bDirty = True
Global $g_bFirstTrayHint = True
Global $g_mDragLbl[]            ; reine Text-Labels in Dialogen = Greifflaechen zum Verschieben

; ---------------------------------------------------------------- GDI+
_GDIPlus_Startup()
; COM-Fehler (z. B. Taskleiste) abfangen, statt das Programm zu beenden
Global $g_oComErr = ObjEvent("AutoIt.Error", "__ComErr")
Global $g_hFamUI = _GDIPlus_FontFamilyCreate("Segoe UI")
Global $g_hFamSym = _GDIPlus_FontFamilyCreate($GLYPH_FONT)

; dunkle Kontextmenues (undokumentierte uxtheme-Funktion, ab Win10 1903; schlaegt sonst still fehl)
DllCall("uxtheme.dll", "int", 135, "int", 2)

; ---------------------------------------------------------------- Hauptfenster
__PosCheck()
Global $g_hGui = GUICreate($APP_NAME, $g_iW, $g_iH, $g_iX, $g_iY, BitOR($WS_POPUP, $WS_MINIMIZEBOX, $WS_SYSMENU), ($g_bOnTop ? $WS_EX_TOPMOST : 0))
GUISetIcon($ICO, -1, $g_hGui)
GUISetBkColor($C_BG, $g_hGui)
GUISetFont(__Pt(9), 400, 0, "Segoe UI", $g_hGui)
GUISetOnEvent($GUI_EVENT_CLOSE, "_Quit", $g_hGui)
GUISetOnEvent($GUI_EVENT_PRIMARYDOWN, "_DragAny", $g_hGui)
__WinStyleModern($g_hGui)

; Statuspunkte (links Kamera, rechts Druckstatus) + Titel
Global $g_idDotCam = __Lbl("●", 10, 0, 14, $BAR_H, BitOR($SS_CENTER, $SS_CENTERIMAGE), $C_OFF, 9, 400)
Global $g_idDotMq = __Lbl("●", 23, 0, 14, $BAR_H, BitOR($SS_CENTER, $SS_CENTERIMAGE), $C_OFF, 9, 400)
Global $g_idTitle = __Lbl($APP_NAME, 42, 0, 112, $BAR_H, BitOR($SS_CENTERIMAGE, 0x4000), $C_TXT, 9.5, 600) ; 0x4000 = SS_ENDELLIPSIS
GUICtrlSetOnEvent($g_idTitle, "_PrnMenuShow")
GUICtrlSetCursor($g_idTitle, 0)
GUICtrlSetTip($g_idTitle, "Drucker wechseln")
; Druckermenue (wird bei jedem Oeffnen neu aufgebaut)
Global $g_idPrnDummy = GUICtrlCreateDummy()
Global $g_idPrnCtx = GUICtrlCreateContextMenu($g_idPrnDummy)
Global $g_aPrnMenuIds[1] = [0], $g_mPrnMenu[]
GUICtrlSetResizing($g_idDotCam, BitOR($GUI_DOCKLEFT, $GUI_DOCKTOP, $GUI_DOCKSIZE))
GUICtrlSetResizing($g_idDotMq, BitOR($GUI_DOCKLEFT, $GUI_DOCKTOP, $GUI_DOCKSIZE))
GUICtrlSetResizing($g_idTitle, BitOR($GUI_DOCKLEFT, $GUI_DOCKTOP, $GUI_DOCKSIZE))
; Symbol-Knoepfe rechts (Reihenfolge von rechts nach links)
Global $g_idBtnClose = __BarBtn(0xE8BB, 1, "Beenden", "_Quit")
Global $g_idBtnMin = __BarBtn(0xE921, 2, "Minimieren", "_Minimize")
Global $g_idBtnHelp = __BarBtn(0xE897, 3, "Hilfe (F1)", "_HelpShow")
Global $g_idBtnSet = __BarBtn(0xE713, 4, "Einstellungen (F2)", "_SettingsShow")
Global $g_idBtnPin = __BarBtn(0xE718, 5, "Immer im Vordergrund (T)", "_ToggleOnTop")
Global $g_idBtnSnap = __BarBtn(0xE722, 6, "Snapshot speichern (S)", "_Snapshot")
Global $g_idBtnOvl = __BarBtn(0xE81E, 7, "Status-Overlay umschalten (O)", "_CycleOverlay")
Global $g_idBtnLight = __BarBtn(0xE82F, 8, "Druckerlicht an/aus (L)", "_LightToggle") ; Gluehbirne
Global $g_idBtnView = __BarBtn(0xE8B9, 9, "Kamerabild / Druckobjekt umschalten", "_ViewToggle") ; Bild
Global $g_aBarBtns[9] = [$g_idBtnClose, $g_idBtnMin, $g_idBtnHelp, $g_idBtnSet, $g_idBtnPin, $g_idBtnSnap, $g_idBtnOvl, $g_idBtnLight, $g_idBtnView]
Global $g_idHover = 0

; Bildflaeche
Global $g_idPic = GUICtrlCreatePic("", $PAD, $BAR_H, $g_iW - 2 * $PAD, $g_iH - $BAR_H - $PAD)
GUICtrlSetResizing($g_idPic, $GUI_DOCKBORDERS)

; Kontextmenue auf dem Bild
Global $g_idCtx = GUICtrlCreateContextMenu($g_idPic)
__Menu($g_idCtx, "Snapshot speichern" & @TAB & "S", "_Snapshot")
__Menu($g_idCtx, "Overlay umschalten" & @TAB & "O", "_CycleOverlay")
__Menu($g_idCtx, "Druckerlicht an/aus" & @TAB & "L", "_LightToggle")
__Menu($g_idCtx, "Kamerabild / Druckobjekt umschalten", "_ViewToggle")
Global $g_idMnuTop = __Menu($g_idCtx, "Immer im Vordergrund" & @TAB & "T", "_ToggleOnTop")
GUICtrlCreateMenuItem("", $g_idCtx)
Global $g_idMnuSize = GUICtrlCreateMenu("Größe", $g_idCtx)
__Menu($g_idMnuSize, "Klein" & @TAB & "1", "_SizeSmall")
__Menu($g_idMnuSize, "Mittel" & @TAB & "2", "_SizeMedium")
__Menu($g_idMnuSize, "Groß" & @TAB & "3", "_SizeLarge")
__Menu($g_idCtx, "Snapshot-Ordner öffnen", "_SnapFolderOpen")
__Menu($g_idCtx, "Verbindung neu aufbauen", "_HelpersStart")
GUICtrlCreateMenuItem("", $g_idCtx)
__Menu($g_idCtx, "Einstellungen …" & @TAB & "F2", "_SettingsShow")
__Menu($g_idCtx, "Hilfe" & @TAB & "F1", "_HelpShow")
GUICtrlCreateMenuItem("", $g_idCtx)
__Menu($g_idCtx, "Beenden", "_Quit")

; Tastenkuerzel (nur wenn BambuWatch aktiv ist)
Global $g_aAccelKeys[10] = ["s", "o", "t", "{F1}", "{F2}", "1", "2", "3", "{F5}", "l"]
Global $g_aAccelFunc[10] = ["_Snapshot", "_CycleOverlay", "_ToggleOnTop", "_HelpShow", "_SettingsShow", "_SizeSmall", "_SizeMedium", "_SizeLarge", "_HelpersStart", "_LightToggle"]
Global $g_aAccel[10][2]
For $__i = 0 To 9
	$g_aAccel[$__i][0] = $g_aAccelKeys[$__i]
	$g_aAccel[$__i][1] = __Dummy($g_aAccelFunc[$__i])
Next
GUISetAccelerators($g_aAccel, $g_hGui)

GUIRegisterMsg($BW_WM_NCHITTEST, "_WM_NCHITTEST")
GUIRegisterMsg($BW_WM_SIZING, "_WM_SIZING")
GUIRegisterMsg($BW_WM_GETMINMAXINFO, "_WM_GETMINMAXINFO")
GUIRegisterMsg($BW_WM_SIZE, "_WM_SIZE")

; Infobereich
TraySetIcon($ICO)
TraySetToolTip($APP_NAME)
TraySetOnEvent($TRAY_EVENT_PRIMARYDOWN, "_TrayShow")
TrayCreateItem("Anzeigen")
TrayItemSetOnEvent(-1, "_TrayShow")
TrayCreateItem("Snapshot speichern")
TrayItemSetOnEvent(-1, "_Snapshot")
TrayCreateItem("")
TrayCreateItem("Beenden")
TrayItemSetOnEvent(-1, "_Quit")
TraySetState($TRAY_ICONSTATE_SHOW)

; Fenster-Fenster: Einstellungen, Hilfe, Changelog
Global $g_hSet = 0, $g_hHelp = 0, $g_hLog = 0
Global $g_aHelpTab[2], $g_aHelpPage[2][2], $g_iHelpTab = 0, $g_idHelpTabLine = 0
Global $g_idSetIp, $g_idSetCode, $g_idSetEye, $g_idSetSerial, $g_idSetSnap, $g_idSetProbeCam, $g_idSetProbeMq, $g_idSetProbeBtn
Global $g_idTogCam, $g_idTogSt, $g_idTogAspect, $g_idTogTop, $g_idTogTray, $g_idTogNotify, $g_idTogSound, $g_idTogStudio
Global $g_idSetOvl, $g_idSetAlpha, $g_iSetOvl = 1, $g_iSetAlpha = 0, $g_bCodeVisible = False
Global $g_idSetFont, $g_iSetFont = 1
Global $g_mTogState[], $g_mTogLink[]
Global $g_aSetTab[3], $g_aSetPage[3][2], $g_iSetTab = 0, $g_idSetTabLine = 0, $g_iSetTabLineY = 0
; Drucker-Reiter: Arbeitskopie der Profile
Global $g_aSetPrn[1][$PF_COUNT], $g_iSetPrnN = 0, $g_iSetPrnSel = 1, $g_iSetActive = 1
Global $g_idSetPrnSel, $g_idSetName, $g_idSetImg, $g_idSetCamT, $g_idSetHaCam, $g_iSetImg = 0, $g_iSetCamT = 0, $g_sSetHaCam = ""
Global $g_idTogCover, $g_idSetHaCov, $g_sSetHaCov = ""
; Konten & Dienste
Global $g_idSetHaIn, $g_idSetHaOut, $g_idSetHaTok, $g_idTogHaIns, $g_idSetHaRes, $g_idSetFf, $g_idSetFfRes
Global $g_aSetHaCams[1][2] = [["", "– keine –"]], $g_iSetHaCamN = 0
Global $g_aSetHaImgs[1][2] = [["", "– keines –"]], $g_iSetHaImgN = 0
Global $g_idSetAcc, $g_idSetLogin, $g_idSetLogout, $g_idSetRegion, $g_idSetConn, $g_iSetRegion = 0, $g_iSetConn = 0
; Anmeldedialog
Global $g_hAcc = 0, $g_idAccEmail, $g_idAccPass, $g_idAccRegion, $g_iAccRegion = 0, $g_idAccLoginBtn
Global $g_idAccCodeLbl, $g_idAccCode, $g_idAccResend, $g_idAccStatus, $g_idAccDev, $g_idAccToken
Global $g_aAccCodeRange[2], $g_aAccDevRange[2]
Global $g_sAccStage = "", $g_sAccTfaKey = "", $g_sAccEmail = "", $g_sAccTok = "", $g_sAccUser = "", $g_bAccByToken = False
Global $g_aAccDev[1][5], $g_iAccDevN = 0, $g_iAccDevSel = 1, $g_tAcc = 0
Global $g_tProbe = 0

WinSetTrans($g_hGui, "", $g_iAlpha)
__PinStyle()
__TitleLabel()
GUISetState(@SW_SHOW, $g_hGui)
__TaskbarInit()
_Render()

$g_bStudioPause = ($g_bStudioWatch And ProcessExists("bambu-studio.exe") <> 0)
If Not __CanLan(False) And $g_sToken = "" And Not (__HaConfigured() And $g_sHaCam <> "") Then
	_SettingsShow()
Else
	_HelpersStart()
EndIf

; ---------------------------------------------------------------- Hauptschleife
Global $g_tTick = TimerInit(), $g_tHover = TimerInit(), $g_tRender = 0, $g_iWS = 0
While 1
	_PollHelpers()
	_ProbePoll()
	_AccPoll()
	_HaListPoll()
	If TimerDiff($g_tHover) > 50 Then
		_HoverCheck()
		$g_tHover = TimerInit()
	EndIf
	If TimerDiff($g_tTick) >= 1000 Then
		_EverySecond()
		$g_tTick = TimerInit()
	EndIf
	; hoechstens ca. 25 Bilder/s zeichnen, nicht wenn versteckt/minimiert
	If $g_bDirty And ($g_tRender = 0 Or TimerDiff($g_tRender) >= 40) Then
		$g_iWS = WinGetState($g_hGui)
		If BitAND($g_iWS, $WIN_STATE_VISIBLE) And Not BitAND($g_iWS, $WIN_STATE_MINIMIZED) Then _Render()
		$g_tRender = TimerInit()
	EndIf
	Sleep(15)
WEnd

Func _HelpersStart()
	_HelpersStop()
	$g_iCam = 0
	$g_iMq = 0
	$g_iCl = 0
	$g_sCamMsg = ""
	$g_sMqMsg = ""
	$g_sClMsg = ""
	$g_sSrc = ""
	$g_tLanSt = 0
	$g_tLanStart = TimerInit()
	; Kamerabild: im Heimnetz direkt vom Drucker, sonst ueber Home Assistant (__HaCheck)
	$g_iHa = 0
	$g_sHaMsg = ""
	$g_bCamStop = False
	$g_bHaStop = False
	$g_tLanFrame = 0
	$g_tCamStart = TimerInit()
	$g_sImgVia = ""
	$g_sCamTypeNow = ""
	; Bambu Studio laeuft: nichts verbinden (__StudioCheck startet neu, sobald es beendet ist)
	If $g_bStudioPause Then
		__DotsUpdate()
		$g_bDirty = True
		Return
	EndIf
	__CamStart()
	; Status: erst Heimnetz; Home Assistant (__HsCheck) und Cloud (__CloudCheck) kommen bei Bedarf dazu
	$g_iHs = 0
	$g_sHsMsg = ""
	$g_bHsStop = False
	$g_tHsSt = 0
	If $g_bStatusOn Then
		If __CanLan(True) And __LanAllowed() Then
			$g_iPidMq = __RunHelper("Mqtt", $g_sIp, $g_sCode, $g_sSerial)
			$g_iMq = ($g_iPidMq ? 1 : 4)
			$g_tMqRun = TimerInit()
			If Not $g_iPidMq Then $g_sMqMsg = "PowerShell konnte nicht gestartet werden"
		ElseIf $g_iConnMode = 1 Then
			$g_iMq = 4
			$g_sMqMsg = ($g_sSerial = "" ? "Seriennummer fehlt (Einstellungen)" : "IP-Adresse oder Access Code fehlt (Einstellungen)")
		EndIf
		__HsCheck()
		__CloudCheck()
	EndIf
	__LightStyle()
	__HaCheck()
	__DotsUpdate()
	$g_bDirty = True
EndFunc   ;==>_HelpersStart

; Heimnetz moeglich? Mit $bStatus = True muss auch die Seriennummer da sein
Func __CanLan($bStatus)
	If $g_sIp = "" Or $g_sCode = "" Then Return False
	If $bStatus And $g_sSerial = "" Then Return False
	Return True
EndFunc   ;==>__CanLan

Func __CanCloud()
	Return ($g_bStatusOn And $g_iConnMode <> 1 And $g_iConnMode <> 3 And $g_sToken <> "" And $g_sUser <> "" And $g_sSerial <> "" And Not $g_bClAuthBad)
EndFunc   ;==>__CanCloud

Func __LanFresh()
	Return ($g_tLanSt <> 0 And TimerDiff($g_tLanSt) < $LAN_STALE_MS)
EndFunc   ;==>__LanFresh

; Cloud zuschalten, wenn das Heimnetz keinen Status liefert (Automatik) oder "Nur Bambu-Cloud" gewaehlt ist
Func __CloudCheck()
	If $g_iPidCl <> 0 Or Not __CanCloud() Then Return
	If $g_iConnMode = 0 And __HsUsable() Then Return ; Home Assistant liefert -> Cloud nicht noetig
	If $g_tRestartCl <> 0 And TimerDiff($g_tRestartCl) < 10000 Then Return
	Local $bNeed
	If $g_iConnMode = 2 Or Not __CanLan(True) Then
		$bNeed = True
	ElseIf $g_tLanSt = 0 Then
		$bNeed = (TimerDiff($g_tLanStart) > $LAN_GRACE_MS)
	Else
		$bNeed = Not __LanFresh()
	EndIf
	If Not $bNeed Then Return
	$g_tRestartCl = 0
	$g_sBufCl = ""
	$g_iPidCl = __RunHelperEnv("-Mode CloudMqtt -Region " & $g_sRegion & ' -Serial "' & $g_sSerial & '"', "BW_TOKEN", $g_sToken, "BW_USER", $g_sUser)
	$g_iCl = ($g_iPidCl ? 1 : 4)
	$g_sClMsg = ($g_iPidCl ? "" : "PowerShell konnte nicht gestartet werden")
	$g_bDirty = True
EndFunc   ;==>__CloudCheck

Func __CloudStop()
	If $g_iPidCl Then ProcessClose($g_iPidCl)
	$g_iPidCl = 0
	$g_sBufCl = ""
	$g_iCl = 0
	$g_sClMsg = ""
	$g_tRestartCl = 0
EndFunc   ;==>__CloudStop

; ---------------------------------------------------------------- Status ueber Home Assistant
Func __LanAllowed()
	Return ($g_iConnMode = 0 Or $g_iConnMode = 1)
EndFunc   ;==>__LanAllowed

Func __HsFresh()
	Return ($g_tHsSt <> 0 And TimerDiff($g_tHsSt) < $LAN_STALE_MS)
EndFunc   ;==>__HsFresh

Func __CanHs()
	Return ($g_bStatusOn And ($g_iConnMode = 0 Or $g_iConnMode = 3) And __HaConfigured() And $g_sSerial <> "" And Not $g_bHsStop)
EndFunc   ;==>__CanHs

; laeuft der HA-Status (ohne Fehler) oder liefert er? Dann braucht es die Cloud nicht
Func __HsUsable()
	Return (__HsFresh() Or ($g_iPidHs <> 0 And $g_iHs <> 4))
EndFunc   ;==>__HsUsable

; Home Assistant zuschalten, wenn das Heimnetz keinen Status liefert (Automatik) oder "Nur Home Assistant"
Func __HsCheck()
	If $g_iPidHs <> 0 Or Not __CanHs() Then Return
	If $g_tRestartHs <> 0 And TimerDiff($g_tRestartHs) < 10000 Then Return
	Local $bNeed
	If $g_iConnMode = 3 Or Not __CanLan(True) Then
		$bNeed = True
	ElseIf $g_tLanSt = 0 Then
		$bNeed = (TimerDiff($g_tLanStart) > $LAN_GRACE_MS)
	Else
		$bNeed = Not __LanFresh()
	EndIf
	If Not $bNeed Then Return
	$g_tRestartHs = 0
	$g_sBufHs = ""
	$g_iPidHs = __RunHelperEnv('-Mode HaStatus -Serial "' & $g_sSerial & '"' & ($g_bHaInsecure ? " -HaInsecure" : ""), "BW_HATOKEN", $g_sHaToken, "BW_HAURLS", $g_sHaUrlIn & "|" & $g_sHaUrlOut)
	$g_iHs = ($g_iPidHs ? 1 : 4)
	$g_sHsMsg = ($g_iPidHs ? "" : "PowerShell konnte nicht gestartet werden")
	$g_bDirty = True
EndFunc   ;==>__HsCheck

Func __HsStop()
	If $g_iPidHs Then ProcessClose($g_iPidHs)
	$g_iPidHs = 0
	$g_sBufHs = ""
	$g_iHs = 0
	$g_sHsMsg = ""
	$g_tRestartHs = 0
EndFunc   ;==>__HsStop

; unterwegs: das Heimnetz liefert nicht, der Status kommt aber ueber Home Assistant oder die Cloud
Func __Away()
	Return (($g_sSrc = "Cloud" Or $g_sSrc = "HA") And Not __LanFresh())
EndFunc   ;==>__Away

; der HA-Status meldet die Entitaeten des Druckers: Kamera, Titelbild, Licht -> ins Profil
Func __HaEntFound($s)
	Local $p = StringSplit($s, "|", 2)
	If UBound($p) < 2 Or $p[1] = "" Then Return
	Local $bChg = False
	Switch $p[0]
		Case "cam"
			If $g_sHaCam = "" Then
				$g_sHaCam = $p[1]
				$bChg = True
				__ToastShow("Kamera in Home Assistant gefunden")
			EndIf
		Case "cover"
			If $g_sHaCover = "" Then
				$g_sHaCover = $p[1]
				$bChg = True
			EndIf
		Case "light"
			If $g_sHaLight <> $p[1] Then
				$g_sHaLight = $p[1]
				$bChg = True
			EndIf
	EndSwitch
	If Not $bChg Then Return
	__PrnStoreActive()
	__PrnSaveAll()
	; offene Einstellungen: Arbeitskopie mitziehen, damit "Speichern" nichts zuruecksetzt
	If $g_hSet <> 0 And $g_iPrn <= $g_iSetPrnN Then
		If $g_aSetPrn[$g_iPrn][$PF_HACAM] = "" Then $g_aSetPrn[$g_iPrn][$PF_HACAM] = $g_sHaCam
		If $g_aSetPrn[$g_iPrn][$PF_HACOVER] = "" Then $g_aSetPrn[$g_iPrn][$PF_HACOVER] = $g_sHaCover
		$g_aSetPrn[$g_iPrn][$PF_HALIGHT] = $g_sHaLight
	EndIf
EndFunc   ;==>__HaEntFound

; $bKeepView = True: nur Verbindungen trennen, Bild, Druckobjekt und letzten Status behalten (Pause)
Func _HelpersStop($bKeepView = False)
	If $g_iPidCam Then __KillTree($g_iPidCam) ; Kamera-Helfer kann ffmpeg gestartet haben
	If $g_iPidMq Then ProcessClose($g_iPidMq)
	$g_iPidCam = 0
	$g_iPidMq = 0
	$g_sBufCam = ""
	$g_sBufMq = ""
	$g_tRestartCam = 0
	$g_tRestartMq = 0
	__CloudStop()
	__HsStop()
	__HaStop()
	If $bKeepView Then
		If $g_iPidCov Then ProcessClose($g_iPidCov)
		$g_iPidCov = 0
		$g_sBufCov = ""
		If $g_iPidLight Then ProcessClose($g_iPidLight)
		$g_iPidLight = 0
		Return
	EndIf
	__CoverStop()
	$g_bHaveSt = False
	Local $mEmpty[]
	$g_mSt = $mEmpty
	$g_sPrevState = ""
EndFunc   ;==>_HelpersStop

; alle 2 s: laeuft Bambu Studio? Dann alle Verbindungen trennen, danach automatisch wieder verbinden.
; (Prozessliste lesen = ein Systemaufruf, kein Netzwerk)
Func __StudioCheck()
	Static $iTick = 0
	$iTick += 1
	If Mod($iTick, 2) <> 0 Then Return
	Local $bRun = ($g_bStudioWatch And ProcessExists("bambu-studio.exe") <> 0)
	If $bRun And Not $g_bStudioPause Then
		$g_bStudioPause = True
		_HelpersStop(True)
		__ToastShow("Bambu Studio läuft – BambuWatch pausiert")
		__DotsUpdate()
		$g_bDirty = True
	ElseIf Not $bRun And $g_bStudioPause Then
		$g_bStudioPause = False
		__ToastShow("Bambu Studio beendet – verbinde wieder")
		_HelpersStart()
	EndIf
EndFunc   ;==>__StudioCheck

; beendet einen Prozess samt Unterprozessen (ffmpeg)
Func __KillTree($iPid)
	If $iPid = 0 Then Return
	Run(@ComSpec & " /c taskkill /PID " & $iPid & " /T /F", "", @SW_HIDE)
	ProcessClose($iPid)
EndFunc   ;==>__KillTree

; Kamera-Helfer fuers Heimnetz starten (Typ: automatisch, Einzelbilder oder Videostream)
Func __CamStart()
	If $g_iPidCam Then __KillTree($g_iPidCam)
	$g_iPidCam = 0
	$g_sBufCam = ""
	$g_tRestartCam = 0
	If Not ($g_bCamOn And __CanLan(False) And $g_iImgSrc <> 2 And Not $g_bCamStop) Then Return
	Local $sArgs = '-Mode Camera -Ip "' & $g_sIp & '" -CamType ' & $CAMTYPE_KEYS[$g_iCamType]
	Local $sFf = __FfmpegPath()
	If $sFf <> "" Then $sArgs &= ' -Ffmpeg "' & $sFf & '"'
	$g_iPidCam = __RunHelperEnv($sArgs, "BW_CODE", $g_sCode)
	$g_tCamRun = TimerInit()
	$g_iCam = ($g_iPidCam ? 1 : 4)
	$g_sCamMsg = ($g_iPidCam ? "" : "PowerShell konnte nicht gestartet werden")
EndFunc   ;==>__CamStart

; ffmpeg.exe: Einstellung, sonst neben BambuWatch, sonst im PATH
Func __FfmpegPath($sSetting = Default)
	If $sSetting = Default Then $sSetting = $g_sFfmpeg
	If $sSetting <> "" And FileExists($sSetting) Then Return $sSetting
	If FileExists(@ScriptDir & "\ffmpeg.exe") Then Return @ScriptDir & "\ffmpeg.exe"
	Local $a = StringSplit(EnvGet("PATH"), ";")
	For $i = 1 To $a[0]
		Local $sDir = StringStripWS($a[$i], 3)
		If $sDir <> "" And FileExists($sDir & "\ffmpeg.exe") Then Return $sDir & "\ffmpeg.exe"
	Next
	Return ""
EndFunc   ;==>__FfmpegPath

Func __HaConfigured()
	Return ($g_sHaToken <> "" And ($g_sHaUrlIn <> "" Or $g_sHaUrlOut <> ""))
EndFunc   ;==>__HaConfigured

Func __LanCamFresh()
	Return ($g_tLanFrame <> 0 And TimerDiff($g_tLanFrame) < $LAN_STALE_MS)
EndFunc   ;==>__LanCamFresh

; Bild ueber Home Assistant holen, wenn der Drucker im Heimnetz kein Bild liefert (oder "Nur Home Assistant")
Func __HaCheck()
	If $g_iPidHa <> 0 Then Return
	If Not ($g_bCamOn And $g_iImgSrc <> 1 And __HaConfigured() And $g_sHaCam <> "" And Not $g_bHaStop) Then Return
	If $g_tRestartHa <> 0 And TimerDiff($g_tRestartHa) < 10000 Then Return
	Local $bNeed
	If $g_iImgSrc = 2 Or Not __CanLan(False) Or $g_bCamStop Then
		$bNeed = True
	ElseIf $g_tLanFrame = 0 Then
		$bNeed = (TimerDiff($g_tCamStart) > $LAN_GRACE_MS)
	Else
		$bNeed = Not __LanCamFresh()
	EndIf
	If Not $bNeed Then Return
	$g_tRestartHa = 0
	$g_sBufHa = ""
	Local $sArgs = '-Mode HaCamera -HaEntity "' & $g_sHaCam & '"' & ($g_bHaInsecure ? " -HaInsecure" : "")
	$g_iPidHa = __RunHelperEnv($sArgs, "BW_HATOKEN", $g_sHaToken, "BW_HAURLS", $g_sHaUrlIn & "|" & $g_sHaUrlOut)
	$g_iHa = ($g_iPidHa ? 1 : 4)
	$g_sHaMsg = ($g_iPidHa ? "" : "PowerShell konnte nicht gestartet werden")
	$g_bDirty = True
EndFunc   ;==>__HaCheck

Func __HaStop()
	If $g_iPidHa Then ProcessClose($g_iPidHa)
	$g_iPidHa = 0
	$g_sBufHa = ""
	$g_iHa = 0
	$g_sHaMsg = ""
	$g_tRestartHa = 0
EndFunc   ;==>__HaStop

; Startet den Helfer fuers Heimnetz. Der Access Code geht per Umgebungsvariable.
Func __RunHelper($sMode, $sIp, $sCode, $sSerial)
	Local $sArgs = "-Mode " & $sMode & ' -Ip "' & $sIp & '"'
	If $sSerial <> "" Then $sArgs &= ' -Serial "' & $sSerial & '"'
	Return __RunHelperEnv($sArgs, "BW_CODE", $sCode)
EndFunc   ;==>__RunHelper

; Startet den Helfer mit bis zu drei geheimen Werten als Umgebungsvariable
; (nicht in der Prozessliste sichtbar; der Helfer loescht sie sofort wieder).
; Leere Parameter nie als "" uebergeben: powershell.exe -File verschluckt sie.
Func __RunHelperEnv($sArgs, $n1 = "", $v1 = "", $n2 = "", $v2 = "", $n3 = "", $v3 = "")
	If Not FileExists($HELPER) Then Return 0
	Local $sCmd = 'powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' & $HELPER & '" ' & _
			$sArgs & ' -OutDir "' & $WORK & '" -ParentPid ' & @AutoItPID
	If $n1 <> "" Then EnvSet($n1, $v1)
	If $n2 <> "" Then EnvSet($n2, $v2)
	If $n3 <> "" Then EnvSet($n3, $v3)
	Local $iPid = Run($sCmd, $WORK, @SW_HIDE, $STDERR_MERGED)
	If $n1 <> "" Then EnvSet($n1)
	If $n2 <> "" Then EnvSet($n2)
	If $n3 <> "" Then EnvSet($n3)
	Return $iPid
EndFunc   ;==>__RunHelperEnv

; liest neue Zeilen; ist der Prozess beendet, wird $iPid 0 und @extended 1
Func __ReadLines(ByRef $iPid, ByRef $sBuf)
	Local $aOut[1] = [0]
	If $iPid = 0 Then Return SetExtended(0, $aOut)
	Local $b = StdoutRead($iPid, False, True)
	Local $bEnded = (@error <> 0)
	If BinaryLen($b) > 0 Then $sBuf &= BinaryToString($b, $SB_UTF8)
	If StringInStr($sBuf, @LF) Then
		Local $a = StringSplit($sBuf, @LF, 1)
		$sBuf = $a[$a[0]] ; unvollstaendige Restzeile bleibt im Puffer
		ReDim $aOut[$a[0]]
		$aOut[0] = $a[0] - 1
		For $i = 1 To $a[0] - 1
			$aOut[$i] = StringStripWS($a[$i], 3)
		Next
	EndIf
	If $bEnded Then
		$iPid = 0
		Return SetExtended(1, $aOut)
	EndIf
	Return SetExtended(0, $aOut)
EndFunc   ;==>__ReadLines

Func _PollHelpers()
	Local $a, $sFrame = "", $bEnd, $s
	; --- Kamerabild: Drucker (Heimnetz) und Home Assistant
	If $g_iPidCam Then __PollCam(False)
	If $g_iPidHa Then __PollCam(True)

	; --- Druckstatus: Heimnetz, dann Cloud
	If $g_iPidMq Then __PollStatus(0)
	If $g_iPidCl Then __PollStatus(1)
	If $g_iPidHs Then __PollStatus(2)
	If $g_iPidLight Then __PollLight()

	; --- Druckobjekt-Bild
	If $g_iPidCov Then __PollCover()
EndFunc   ;==>_PollHelpers

; liest den Kamera-Helfer (Drucker) oder den HA-Helfer ($bHa = True)
Func __PollCam($bHa)
	Local $a, $bEnd, $s, $sFrame = "", $iState, $sMsg, $bStop = False
	If $bHa Then
		$a = __ReadLines($g_iPidHa, $g_sBufHa)
	Else
		$a = __ReadLines($g_iPidCam, $g_sBufCam)
	EndIf
	$bEnd = (@extended = 1)
	$iState = ($bHa ? $g_iHa : $g_iCam)
	$sMsg = ($bHa ? $g_sHaMsg : $g_sCamMsg)
	For $i = 1 To $a[0]
		$s = $a[$i]
		If $s = "" Then ContinueLoop
		If StringLeft($s, 6) = "FRAME|" Then
			$sFrame = StringTrimLeft($s, 6)
		ElseIf $s = "STATE|connecting" Then
			If $iState <> 3 Then $iState = 1
		ElseIf $s = "STATE|connected" Then
			$iState = 2
			$sMsg = ""
		ElseIf StringLeft($s, 8) = "CAMTYPE|" Then
			$g_sCamTypeNow = StringTrimLeft($s, 8)
		ElseIf StringLeft($s, 4) = "VIA|" Then
			; Adresse, ueber die Home Assistant erreichbar war (nur Info)
		ElseIf StringLeft($s, 9) = "ERR|STOP|" Or StringLeft($s, 9) = "ERR|AUTH|" Then
			$iState = 4
			$sMsg = StringTrimLeft($s, 9)
			$bStop = True
		ElseIf StringLeft($s, 4) = "ERR|" Then
			$iState = 4
			$sMsg = StringTrimLeft($s, 4)
		Else ; sonstige Ausgabe von PowerShell (z. B. Startfehler)
			$sMsg = StringLeft($s, 160)
		EndIf
		$g_bDirty = True
	Next
	If $sFrame <> "" Then
		$iState = 3
		$sMsg = ""
		If $bHa Then
			If Not __LanCamFresh() Then __FrameLoad($sFrame, "HA")
		Else
			$g_tLanFrame = TimerInit()
			__FrameLoad($sFrame, "LAN")
			; Drucker liefert wieder selbst -> Home Assistant nicht mehr noetig
			If $g_iPidHa And $g_iImgSrc <> 2 Then __HaStop()
		EndIf
	EndIf
	If $bEnd Then
		$iState = 4
		If $sMsg = "" Then $sMsg = ($bHa ? "Home-Assistant-Helfer beendet" : "Kamera-Helfer beendet")
		$g_bDirty = True
	EndIf
	If $bHa Then
		$g_iHa = $iState
		$g_sHaMsg = $sMsg
		If $bStop Then $g_bHaStop = True
		If $bEnd And Not $g_bHaStop Then $g_tRestartHa = TimerInit()
	Else
		$g_iCam = $iState
		$g_sCamMsg = $sMsg
		If $bStop Then $g_bCamStop = True
		If $bEnd And Not $g_bCamStop Then $g_tRestartCam = TimerInit()
	EndIf
	__DotsUpdate()
EndFunc   ;==>__PollCam

; liest einen Status-Helfer: 0 Heimnetz, 1 Bambu-Cloud, 2 Home Assistant
Func __PollStatus($iKind)
	Local $a, $bEnd, $s, $iState, $sMsg, $bStop = False
	Switch $iKind
		Case 1
			$a = __ReadLines($g_iPidCl, $g_sBufCl)
			$bEnd = (@extended = 1)
			$iState = $g_iCl
			$sMsg = $g_sClMsg
		Case 2
			$a = __ReadLines($g_iPidHs, $g_sBufHs)
			$bEnd = (@extended = 1)
			$iState = $g_iHs
			$sMsg = $g_sHsMsg
		Case Else
			$a = __ReadLines($g_iPidMq, $g_sBufMq)
			$bEnd = (@extended = 1)
			$iState = $g_iMq
			$sMsg = $g_sMqMsg
	EndSwitch
	For $i = 1 To $a[0]
		$s = $a[$i]
		If $s = "" Then ContinueLoop
		If StringLeft($s, 7) = "STATUS|" Then
			$iState = 3
			$sMsg = ""
			__StatusFrom($s, $iKind)
		ElseIf $s = "STATE|connecting" Then
			$iState = 1
		ElseIf $s = "STATE|connected" Then
			$iState = 2
			$sMsg = ""
		ElseIf StringLeft($s, 6) = "HAENT|" Then
			__HaEntFound(StringTrimLeft($s, 6))
		ElseIf StringLeft($s, 4) = "VIA|" Then
			; Adresse, ueber die Home Assistant erreichbar war (nur Info)
		ElseIf StringLeft($s, 9) = "ERR|AUTH|" Then
			$iState = 4
			$sMsg = StringTrimLeft($s, 9)
			If $iKind = 1 Then __CloudAuthFailed()
			If $iKind = 2 Then $bStop = True
		ElseIf StringLeft($s, 9) = "ERR|STOP|" Then
			$iState = 4
			$sMsg = StringTrimLeft($s, 9)
			$bStop = True
		ElseIf StringLeft($s, 4) = "ERR|" Then
			$iState = 4
			$sMsg = StringTrimLeft($s, 4)
		Else ; sonstige Ausgabe von PowerShell (z. B. Startfehler)
			$sMsg = StringLeft($s, 160)
		EndIf
		$g_bDirty = True
	Next
	If $bEnd Then
		$iState = 4
		If $sMsg = "" Then $sMsg = "Status-Helfer beendet"
		$g_bDirty = True
	EndIf
	Switch $iKind
		Case 1
			$g_iCl = $iState
			$g_sClMsg = $sMsg
			If $bEnd And Not $g_bClAuthBad Then $g_tRestartCl = TimerInit()
		Case 2
			$g_iHs = $iState
			$g_sHsMsg = $sMsg
			If $bStop Then $g_bHsStop = True
			If $bEnd And Not $g_bHsStop Then $g_tRestartHs = TimerInit()
		Case Else
			$g_iMq = $iState
			$g_sMqMsg = $sMsg
			If $bEnd Then $g_tRestartMq = TimerInit()
	EndSwitch
	__DotsUpdate()
EndFunc   ;==>__PollStatus

; Status uebernehmen: Heimnetz vor Home Assistant vor Cloud, jeweils solange frisch
Func __StatusFrom($sLine, $iKind)
	Switch $iKind
		Case 0
			$g_tLanSt = TimerInit()
			$g_sSrc = "LAN"
			; Heimnetz liefert -> Cloud und HA nicht mehr noetig (ausser fest eingestellt)
			If $g_iPidCl And $g_iConnMode <> 2 Then __CloudStop()
			If $g_iPidHs And $g_iConnMode <> 3 Then __HsStop()
			__StatusParse($sLine)
		Case 2
			If __LanFresh() Then Return
			$g_tHsSt = TimerInit()
			$g_sSrc = "HA"
			If $g_iPidCl And $g_iConnMode = 0 Then __CloudStop()
			__StatusParse($sLine)
			__IpCheck()
		Case Else
			If __LanFresh() Or __HsFresh() Then Return
			$g_sSrc = "Cloud"
			__StatusParse($sLine)
			__IpCheck()
	EndSwitch
EndFunc   ;==>__StatusFrom

; Drucker-IP aus dem Cloud-Status uebernehmen (print.net.info) -> Kamerabild im Heimnetz ohne Abtippen
Func __IpCheck()
	If $g_iConnMode = 1 Then Return
	Local $sNew = __St("ip")
	If $sNew = "" Or $sNew = $g_sIp Or $sNew = "0.0.0.0" Then Return
	If Not StringRegExp($sNew, "^\d{1,3}(\.\d{1,3}){3}$") Then Return
	Local $bHad = ($g_sIp <> "")
	$g_sIp = $sNew
	__PrnStoreActive()
	__PrnSaveAll()
	If $g_hSet <> 0 Then GUICtrlSetData($g_idSetIp, $g_sIp)
	__ToastShow(($bHad ? "Neue Drucker-IP übernommen: " : "Drucker-IP übernommen: ") & $g_sIp)
	; Kamera und Heimnetz-Status mit der neuen IP neu starten (Cloud laeuft weiter)
	$g_bCamStop = False
	$g_tCamStart = TimerInit()
	__CamStart()
	If $g_iPidMq Then ProcessClose($g_iPidMq)
	$g_iPidMq = 0
	$g_sBufMq = ""
	$g_tRestartMq = 0
	If $g_bStatusOn And __CanLan(True) And __LanAllowed() Then
		$g_iPidMq = __RunHelper("Mqtt", $g_sIp, $g_sCode, $g_sSerial)
		$g_iMq = ($g_iPidMq ? 1 : 4)
		$g_tMqRun = TimerInit()
		$g_tLanStart = TimerInit()
	EndIf
EndFunc   ;==>__IpCheck

; Cloud meldet: Token abgelaufen/ungueltig -> nicht weiter probieren, einmal Bescheid geben
Func __CloudAuthFailed()
	If $g_bClAuthBad Then Return
	$g_bClAuthBad = True
	__ToastShow("Bambu-Anmeldung abgelaufen")
	If $g_bNotify Then TrayTip($APP_NAME, "Bambu-Anmeldung abgelaufen – für den Status von unterwegs in den Einstellungen neu anmelden.", 10, $TIP_ICONEXCLAMATION)
	__AccountLabelUpdate()
EndFunc   ;==>__CloudAuthFailed

; Bild aus Datei in den Speicher holen (ohne die Datei zu sperren)
Func __FrameLoad($sPath, $sVia = "LAN")
	Local $hF = FileOpen($sPath, $FO_BINARY)
	If $hF = -1 Then Return
	Local $b = FileRead($hF)
	FileClose($hF)
	If BinaryLen($b) < 100 Then Return
	Local $hImg = _GDIPlus_BitmapCreateFromMemory($b)
	If @error Or $hImg = 0 Then Return
	If $g_hFrame Then _GDIPlus_ImageDispose($g_hFrame)
	$g_hFrame = $hImg
	$g_bFrame = $b
	$g_tFrame = TimerInit()
	Local $iW = _GDIPlus_ImageGetWidth($hImg), $iH = _GDIPlus_ImageGetHeight($hImg)
	If $iW > 0 And $iH > 0 Then $g_fAspect = $iW / $iH
	$g_sImgVia = $sVia
	$g_bDirty = True
EndFunc   ;==>__FrameLoad

Func __StatusParse($sLine)
	Local $a = StringSplit($sLine, "|", 2)
	For $i = 1 To UBound($a) - 1
		Local $p = StringInStr($a[$i], "=")
		If $p = 0 Then ContinueLoop
		$g_mSt[StringLeft($a[$i], $p - 1)] = StringMid($a[$i], $p + 1)
	Next
	$g_bHaveSt = True
	$g_tSt = TimerInit()
	; Zustandswechsel melden
	Local $sNow = __St("st")
	If $g_sPrevState <> "" And $sNow <> $g_sPrevState Then
		Local $bWasActive = __IsActive($g_sPrevState)
		If $bWasActive And $sNow = "FINISH" Then __Notify("Druck fertig", __St("job"), 0xFF00AE42)
		If $bWasActive And $sNow = "FAILED" Then __Notify("Druck abgebrochen", __St("job"), 0xFFE5484D)
		If $g_sPrevState = "RUNNING" And $sNow = "PAUSE" Then __Notify("Druck pausiert", "Am Drucker nachsehen (z. B. Filament)", 0xFFF5A524)
	EndIf
	If $sNow <> "" Then $g_sPrevState = $sNow
	__TitleUpdate()
	__LightStyle()
EndFunc   ;==>__StatusParse

Func __St($sKey)
	If MapExists($g_mSt, $sKey) Then Return $g_mSt[$sKey]
	Return ""
EndFunc   ;==>__St

Func __IsActive($sState)
	Return ($sState = "RUNNING" Or $sState = "PAUSE" Or $sState = "PREPARE" Or $sState = "SLICING")
EndFunc   ;==>__IsActive

; einmal pro Sekunde: Neustarts, Countdown, Tooltip
Func _EverySecond()
	__StudioCheck()
	If $g_bStudioPause Then
		__TitleUpdate()
		If $g_hPop <> 0 And TimerDiff($g_tPop) > 15000 Then __PopupClose()
		$g_bDirty = True
		Return
	EndIf
	; unterwegs: Kamera und Heimnetz-Status nur einmal pro Minute fuer 20 s probieren
	If __Away() Then
		If $g_iPidCam And Not __LanCamFresh() And TimerDiff($g_tCamRun) > 20000 Then
			__KillTree($g_iPidCam)
			$g_iPidCam = 0
			$g_sBufCam = ""
			$g_iCam = 0
			$g_sCamMsg = "unterwegs – nächster Versuch in 1 min"
			$g_tRestartCam = TimerInit()
		EndIf
		If $g_iPidMq And TimerDiff($g_tMqRun) > 20000 Then
			ProcessClose($g_iPidMq)
			$g_iPidMq = 0
			$g_sBufMq = ""
			$g_iMq = 0
			$g_sMqMsg = "unterwegs – nächster Versuch in 1 min"
			$g_tRestartMq = TimerInit()
		EndIf
	EndIf
	Local $iRetry = (__Away() ? 60000 : 10000)
	If $g_tRestartCam <> 0 And TimerDiff($g_tRestartCam) > $iRetry Then __CamStart()
	If $g_tRestartMq <> 0 And TimerDiff($g_tRestartMq) > $iRetry Then
		$g_tRestartMq = 0
		If $g_bStatusOn And __CanLan(True) And __LanAllowed() Then
			$g_iPidMq = __RunHelper("Mqtt", $g_sIp, $g_sCode, $g_sSerial)
			If $g_iPidMq Then $g_iMq = 1
			$g_tMqRun = TimerInit()
		EndIf
	EndIf
	__HsCheck()
	__CloudCheck()
	__HaCheck()
	__CoverCheck()
	__DotsUpdate()
	__TitleUpdate()
	If $g_hPop <> 0 And TimerDiff($g_tPop) > 15000 Then __PopupClose()
	$g_bDirty = True ; Restzeit, "kein Bild seit", Hinweise
EndFunc   ;==>_EverySecond

; Fenstertitel (Taskleiste) und Tray-Tooltip mit Fortschritt
Func __TitleUpdate()
	Local $sTitle = $APP_NAME & " – " & $g_sPrinterName, $sTip = $APP_NAME & " · " & $g_sPrinterName
	If $g_bHaveSt Then
		Local $sState = __St("st"), $sTxt, $iCol
		__StateInfo($sState, $sTxt, $iCol)
		If __IsActive($sState) Then
			$sTitle &= " – " & __St("pct") & " %"
			$sTip &= @CRLF & $sTxt & " " & __St("pct") & " %"
			If __St("rem") <> "" And $sState = "RUNNING" Then $sTip &= @CRLF & "fertig " & __EtaText(Int(__St("rem")))
		Else
			$sTip &= @CRLF & $sTxt
		EndIf
	EndIf
	If WinGetTitle($g_hGui) <> $sTitle Then WinSetTitle($g_hGui, "", $sTitle)
	TraySetToolTip($sTip)
	__TaskbarUpdate()
EndFunc   ;==>__TitleUpdate

; Statuspunkte: grau aus · gelb verbindet · gruen laeuft · rot Fehler
Func __DotsUpdate()
	Static $iLastCam = -1, $iLastMq = -1, $sLastCamTip = "", $sLastMqTip = ""
	If $g_bStudioPause Then
		GUICtrlSetColor($g_idDotCam, $C_OFF)
		GUICtrlSetColor($g_idDotMq, $C_OFF)
		If $sLastCamTip <> "pause" Then
			GUICtrlSetTip($g_idDotCam, "Pausiert – Bambu Studio läuft")
			GUICtrlSetTip($g_idDotMq, "Pausiert – Bambu Studio läuft")
		EndIf
		$iLastCam = -1
		$iLastMq = -1
		$sLastCamTip = "pause"
		$sLastMqTip = "pause"
		Return
	EndIf
	Local $sMqMsg, $sVia, $sCamMsg, $sCamVia
	Local $iCam = ($g_bCamOn ? __CamEff($sCamMsg, $sCamVia) : 0), $iMq = ($g_bStatusOn ? __StatusEff($sMqMsg, $sVia) : 0)
	; unterwegs ohne Home Assistant ist "kein Bild" kein Fehler -> grau statt rot
	Local $bAway = ($g_bCamOn And $sCamVia = "Drucker" And __Away() And $iCam <> 3)
	If $bAway Then $iCam = 0
	If $iCam = 3 And $g_tFrame <> 0 And TimerDiff($g_tFrame) > 10000 Then $iCam = 1
	If $iCam <> $iLastCam Then
		GUICtrlSetColor($g_idDotCam, __StateColor($iCam))
		$iLastCam = $iCam
	EndIf
	If $iMq <> $iLastMq Then
		GUICtrlSetColor($g_idDotMq, __StateColor($iMq))
		$iLastMq = $iMq
	EndIf
	Local $sCamTip = "Kamera" & ($g_bCamOn ? " über " & $sCamVia : "") & ": " & _
			($bAway ? "nur im Heimnetz (gerade unterwegs)" : __StateWord($iCam, $g_bCamOn) & ($sCamMsg <> "" ? @CRLF & $sCamMsg : ""))
	If __CoverShown() Then
		$sCamTip &= @CRLF & "Ersatzbild: Druckobjekt über " & ($g_sCovVia = "HA" ? "Home Assistant" : "Bambu-Cloud")
	ElseIf $g_bCoverOn And $g_sCovMsg <> "" And Not __LiveFrameOk() Then
		$sCamTip &= @CRLF & "Druckobjekt: " & $g_sCovMsg
	EndIf
	Local $sMqTip = "Druckstatus" & ($g_bStatusOn ? " über " & $sVia : "") & ": " & __StateWord($iMq, $g_bStatusOn) & ($sMqMsg <> "" ? @CRLF & $sMqMsg : "")
	If $sCamTip <> $sLastCamTip Then
		GUICtrlSetTip($g_idDotCam, $sCamTip)
		$sLastCamTip = $sCamTip
	EndIf
	If $sMqTip <> $sLastMqTip Then
		GUICtrlSetTip($g_idDotMq, $sMqTip)
		$sLastMqTip = $sMqTip
	EndIf
EndFunc   ;==>__DotsUpdate

; wirksame Bildquelle: Home Assistant, wenn der Drucker gerade kein Bild liefert und HA im Spiel ist
Func __CamEff(ByRef $sMsg, ByRef $sVia)
	If Not __LanCamFresh() And ($g_iImgSrc = 2 Or $g_iPidHa <> 0 Or ($g_bHaStop And __HaConfigured() And $g_sHaCam <> "")) Then
		$sMsg = $g_sHaMsg
		$sVia = "Home Assistant"
		Return $g_iHa
	EndIf
	$sMsg = $g_sCamMsg
	$sVia = "Drucker"
	Return $g_iCam
EndFunc   ;==>__CamEff

; wirksamer Status-Zustand: Heimnetz, sonst Home Assistant, sonst Cloud - je nachdem, was im Spiel ist
Func __StatusEff(ByRef $sMsg, ByRef $sVia)
	If Not __LanFresh() And $g_iConnMode <> 1 Then
		Local $bHs = ($g_iConnMode = 3 Or $g_iPidHs <> 0 Or $g_sSrc = "HA" Or $g_bHsStop)
		Local $bCl = ($g_iConnMode = 2 Or $g_iPidCl <> 0 Or $g_sSrc = "Cloud" Or ($g_bClAuthBad And $g_sToken <> ""))
		If $bHs And ($g_sSrc = "HA" Or $g_iConnMode = 3 Or Not $bCl) Then
			$sMsg = $g_sHsMsg
			$sVia = "Home Assistant"
			Return $g_iHs
		EndIf
		If $bCl Then
			$sMsg = $g_sClMsg
			$sVia = "Bambu-Cloud"
			Return $g_iCl
		EndIf
	EndIf
	$sMsg = $g_sMqMsg
	$sVia = "Heimnetz"
	Return $g_iMq
EndFunc   ;==>__StatusEff

Func __StateColor($i)
	Switch $i
		Case 1, 2
			Return $C_AMBER
		Case 3
			Return $C_GREEN
		Case 4
			Return $C_RED
	EndSwitch
	Return $C_OFF
EndFunc   ;==>__StateColor

Func __StateWord($i, $bOn)
	If Not $bOn Then Return "ausgeschaltet"
	Switch $i
		Case 1
			Return "verbinde …"
		Case 2
			Return "verbunden, warte auf Daten"
		Case 3
			Return "läuft"
		Case 4
			Return "Fehler"
	EndSwitch
	Return "nicht verbunden"
EndFunc   ;==>__StateWord

; =============================================================================
;  Zeichnen: Bild + Overlay in ein Bitmap, das dann ins Bild-Steuerelement kommt
; =============================================================================
Func _Render()
	$g_bDirty = False
	Local $aCli = WinGetClientSize($g_hGui)
	If Not IsArray($aCli) Then Return
	Local $w = $aCli[0] - 2 * $PAD, $h = $aCli[1] - $BAR_H - $PAD
	If $w < 20 Or $h < 20 Then Return
	Local $hBmp = _GDIPlus_BitmapCreateFromScan0($w, $h)
	Local $hG = _GDIPlus_ImageGetGraphicsContext($hBmp)
	_GDIPlus_GraphicsSetSmoothingMode($hG, 4)        ; AntiAlias
	_GDIPlus_GraphicsSetTextRenderingHint($hG, 4)    ; AntiAliasGridFit
	_GDIPlus_GraphicsClear($hG, 0xFF0E0F12)
	Local $fS = __Scale($w)

	Local $bCover = __CoverShown()
	If $bCover Then
		; kein (aktuelles) Kamerabild -> Bild des Druckobjekts
		__DrawCover($hG, $w, $h, $fS)
	ElseIf $g_hFrame And $g_bCamOn Then
		; Bild einpassen (Seitenverhaeltnis bleibt, Rest schwarz)
		Local $iw = _GDIPlus_ImageGetWidth($g_hFrame), $ih = _GDIPlus_ImageGetHeight($g_hFrame)
		Local $f = ($w / $iw < $h / $ih ? $w / $iw : $h / $ih)
		Local $dw = $iw * $f, $dh = $ih * $f
		_GDIPlus_GraphicsSetInterpolationMode($hG, 7) ; HighQualityBicubic
		_GDIPlus_GraphicsDrawImageRect($hG, $g_hFrame, ($w - $dw) / 2, ($h - $dh) / 2, $dw, $dh)
		; Bild veraltet -> abdunkeln + Hinweis
		Local $iAge = Int(TimerDiff($g_tFrame) / 1000)
		If $iAge >= 10 And Not $g_bStudioPause Then
			__Fill($hG, 0, 0, $w, $h, 0x99000000)
			Local $sEffMsg, $sEffVia
			Local $iEff = __CamEff($sEffMsg, $sEffVia)
			Local $sWhy = ($iEff = 4 And $sEffMsg <> "" ? $sEffMsg : (__Away() And $sEffVia = "Drucker" ? "unterwegs – Bild nur im Heimnetz" : "verbinde neu …"))
			__Badge($hG, "Kein neues Bild seit " & __AgeText($iAge) & " · " & $sWhy, $w / 2, 14 * $fS, $fS, 0xE6303338, 0xFFE8EAED, 1)
		EndIf
	Else
		__DrawPlaceholder($hG, $w, $h, $fS)
	EndIf

	If $g_iOverlay > 0 And $g_bStatusOn Then __DrawOverlay($hG, $w, $h, $fS)
	; Herkunft oben links: Druckobjekt-Bild, Bild ueber Home Assistant, Status ueber die Cloud
	If $g_iOverlay > 0 Or $bCover Then
		Local $sOrig = ""
		If $bCover Then
			$sOrig = __CoverLabel() & "  ·  " & __NoCamWhy()
		ElseIf $g_hFrame And $g_bCamOn And $g_sImgVia = "HA" Then
			$sOrig = "Bild: Home Assistant"
		EndIf
		If $g_iOverlay > 0 And $g_bStatusOn And __Away() Then $sOrig &= ($sOrig = "" ? "" : "  ·  ") & "Status: " & ($g_sSrc = "HA" ? "Home Assistant" : "Cloud")
		If $sOrig <> "" Then __Badge($hG, $sOrig, 8 * $fS, 8 * $fS, $fS, 0xD0303338, 0xFF9AA0A8, 0)
	EndIf

	; Pause: deutlich anzeigen
	If $g_bStudioPause And ($g_hFrame Or $bCover) Then __Badge($hG, "Pausiert – Bambu Studio läuft", $w / 2, 40 * $fS, $fS, 0xE6303338, 0xFFF5A524, 1)
	; kurzer Hinweis oben (Snapshot gespeichert, Overlay-Modus …)
	If $g_sToast <> "" Then
		If TimerDiff($g_tToast) < 1800 Then
			__Badge($hG, $g_sToast, $w / 2, (($bCover Or ($g_hFrame And TimerDiff($g_tFrame) >= 10000)) ? 44 : 14) * $fS, $fS, 0xE600AE42, 0xFFFFFFFF, 1)
			$g_bDirty = True ; bis der Hinweis weg ist weiterzeichnen
		Else
			$g_sToast = ""
		EndIf
	EndIf

	Local $hH = _GDIPlus_BitmapCreateHBITMAPFromBitmap($hBmp)
	_GDIPlus_GraphicsDispose($hG)
	_GDIPlus_BitmapDispose($hBmp)
	; Bild setzen und altes freigeben (Muster aus _GUICtrlStatic_SetBitmap)
	Local $hOld = GUICtrlSendMsg($g_idPic, $BW_STM_SETIMAGE, $BW_IMAGE_BITMAP, $hH)
	If $hOld Then DllCall("gdi32.dll", "bool", "DeleteObject", "handle", $hOld)
	Local $hCur = GUICtrlSendMsg($g_idPic, $BW_STM_GETIMAGE, $BW_IMAGE_BITMAP, 0)
	If $hCur <> $hH Then DllCall("gdi32.dll", "bool", "DeleteObject", "handle", $hH)
EndFunc   ;==>_Render

Func __Scale($w)
	Local $f = $w / 560
	If $f < 0.8 Then $f = 0.8
	If $f > 1.4 Then $f = 1.4
	Return $f
EndFunc   ;==>__Scale

; Platzhalter, solange kein Bild da ist
Func __DrawPlaceholder($hG, $w, $h, $fS)
	Local $sHead, $sSub = "", $iCol = 0xFF5A5F68
	Local $bHaCam = (__HaConfigured() And $g_sHaCam <> "" And $g_iImgSrc <> 1)
	If $g_bStudioPause Then
		$sHead = "Pausiert – Bambu Studio läuft"
		$sSub = "BambuWatch verbindet sich wieder, sobald Bambu Studio beendet ist"
		$iCol = 0xFFF5A524
	ElseIf Not __CanLan(False) And $g_sToken = "" And Not $bHaCam Then
		$sHead = "Noch nicht eingerichtet"
		$sSub = "Einstellungen öffnen: Zahnrad oben oder F2"
		$iCol = 0xFF00AE42
	ElseIf Not $g_bCamOn Then
		$sHead = "Kamera ausgeschaltet"
		$sSub = "für diesen Drucker – Einstellungen › Drucker"
	ElseIf $g_iPidHa <> 0 Or $g_iImgSrc = 2 Then
		If $g_iImgSrc = 2 And Not $bHaCam Then
			$sHead = "Home Assistant nicht eingerichtet"
			$sSub = "Adresse, Token und Kamera in den Einstellungen eintragen"
			$iCol = 0xFFF5A524
		ElseIf $g_iHa = 4 Then
			$sHead = "Kein Bild von Home Assistant"
			$sSub = $g_sHaMsg
			$iCol = 0xFFE5484D
		Else
			$sHead = "Hole Bild über Home Assistant …"
			$sSub = $g_sHaCam
			$iCol = 0xFFF5A524
		EndIf
	ElseIf __St("cam") = "off" Then
		$sHead = "LAN Mode Liveview ist aus"
		$sSub = "Am Drucker einschalten, dann kommt das Bild"
		$iCol = 0xFFF5A524
	ElseIf $g_bCamStop Then
		$sHead = "Kein Kamerabild"
		$sSub = $g_sCamMsg
		$iCol = 0xFFF5A524
	ElseIf Not __CanLan(False) Then
		$sHead = "Noch kein Kamerabild"
		$sSub = ($g_sIp = "" ? "Warte auf die Drucker-IP aus der Bambu-Cloud …" : "Access Code fehlt (Einstellungen)")
		$iCol = 0xFFF5A524
	ElseIf __Away() Then
		$sHead = "Unterwegs – kein Kamerabild"
		$sSub = ($bHaCam ? "Home Assistant liefert gerade kein Bild" : "Bild von unterwegs: Home Assistant in den Einstellungen eintragen")
		$iCol = 0xFF3B9EFF
	ElseIf $g_iCam = 4 Then
		$sHead = "Keine Verbindung zur Kamera"
		$sSub = $g_sCamMsg & " · neuer Versuch läuft"
		$iCol = 0xFFE5484D
	ElseIf $g_iCam = 2 Then
		$sHead = "Verbunden – warte auf Bild …"
		$sSub = ($g_sCamTypeNow = "rtsp" ? "Videostream startet (ffmpeg) …" : "Der Drucker liefert nicht immer sofort ein Bild")
		$iCol = 0xFFF5A524
	Else
		$sHead = "Verbinde mit " & $g_sIp & " …"
		$iCol = 0xFFF5A524
	EndIf
	Local $fG = 54 * $fS
	Local $y0 = $h / 2 - $fG * 0.9 - (($g_iOverlay > 0 And $g_bHaveSt) ? 24 * $fS : 0)
	If $y0 < 4 Then $y0 = 4
	If $g_hFamSym Then __Txt($hG, ChrW(0xE714), $fG, 0, $iCol, 0, $y0, $w, $fG * 1.25, 1, 1, $g_hFamSym)
	__Txt($hG, $sHead, 15 * $fS, 1, 0xFFE8EAED, 10, $y0 + $fG * 1.3, $w - 20, 24 * $fS, 1, 1)
	If $sSub <> "" Then __Txt($hG, $sSub, 11.5 * $fS, 0, 0xFF9AA0A8, 10, $y0 + $fG * 1.3 + 24 * $fS, $w - 20, 20 * $fS, 1, 1)
EndFunc   ;==>__DrawPlaceholder

; Bild des Druckobjekts einpassen; oben Platz fuer die Herkunfts-Plakette, unten fuers Overlay
Func __DrawCover($hG, $w, $h, $fS)
	Local $iw = _GDIPlus_ImageGetWidth($g_hCover), $ih = _GDIPlus_ImageGetHeight($g_hCover)
	If $iw < 1 Or $ih < 1 Then Return
	_GDIPlus_GraphicsClear($hG, 0xFF15171B)
	Local $fTop = 36 * $fS, $fBot = 12 * $fS
	If $g_iOverlay > 0 And $g_bStatusOn And $g_bHaveSt Then $fBot = ($g_iOverlay = 2 ? (__St("amsl") <> "" ? 99 : 79) : 34) * $fS
	Local $aw = $w - 24 * $fS, $ah = $h - $fTop - $fBot
	If $ah < 60 * $fS Then ; Fenster sehr flach: Bild darf unters Overlay
		$fTop = 8 * $fS
		$ah = $h - 16 * $fS
	EndIf
	If $aw < 20 Or $ah < 20 Then Return
	Local $f = ($aw / $iw < $ah / $ih ? $aw / $iw : $ah / $ih)
	If $f > 3 Then $f = 3 ; kleine Vorschaubilder nicht ins Unscharfe ziehen
	Local $dw = $iw * $f, $dh = $ih * $f
	_GDIPlus_GraphicsSetInterpolationMode($hG, 7) ; HighQualityBicubic
	_GDIPlus_GraphicsDrawImageRect($hG, $g_hCover, ($w - $dw) / 2, $fTop + ($ah - $dh) / 2, $dw, $dh)
EndFunc   ;==>__DrawCover

; =============================================================================
;  Druckobjekt-Bild: Ersatz, wenn kein Kamerabild kommt (Helfer-Modus "Cover")
; =============================================================================
Func __LiveFrameOk()
	Return ($g_hFrame <> 0 And $g_bCamOn And $g_tFrame <> 0 And TimerDiff($g_tFrame) < 10000)
EndFunc   ;==>__LiveFrameOk

; gibt es eine Quelle? Home-Assistant-Bild oder Bambu-Konto (Cloud-Auftraege)
Func __CoverSrc()
	If __HaConfigured() And $g_sHaCover <> "" Then Return True
	Return ($g_sToken <> "" And $g_sSerial <> "" And Not $g_bClAuthBad)
EndFunc   ;==>__CoverSrc

Func __CoverCan()
	If Not ($g_bCoverOn Or $g_bForceCover) Then Return False
	Return __CoverSrc()
EndFunc   ;==>__CoverCan

Func __CoverShown()
	Return ($g_hCover <> 0 And ($g_bForceCover Or ($g_bCoverOn And Not __LiveFrameOk())))
EndFunc   ;==>__CoverShown

; einmal pro Sekunde: Bild holen, wenn noetig. Neu bei anderem Auftrag, sonst alle 10 min
; (nach Fehler jede Minute). Solange die Kamera liefert, wird nichts abgefragt.
Func __CoverCheck()
	If $g_iPidCov <> 0 Then
		If TimerDiff($g_tCovTry) > 60000 Then ; Notbremse, falls der Helfer haengt
			ProcessClose($g_iPidCov)
			$g_iPidCov = 0
			$g_sBufCov = ""
		EndIf
		Return
	EndIf
	If Not __CoverCan() Or (__LiveFrameOk() And Not $g_bForceCover) Then Return
	; der Kamera nach dem (Neu-)Start Zeit lassen - ausser sie ist aus oder es ist umgeschaltet
	If $g_bCamOn And $g_tFrame = 0 And TimerDiff($g_tCamStart) < $LAN_GRACE_MS And Not $g_bForceCover Then Return
	Local $sKey = $g_sSerial & "|" & $g_sHaCover & "|" & __St("job")
	Local $iWait = 5000
	If $sKey = $g_sCovKey Then $iWait = ($g_hCover <> 0 ? 600000 : 60000)
	If $g_tCovTry <> 0 And TimerDiff($g_tCovTry) < $iWait Then Return
	$g_sCovKey = $sKey
	$g_tCovTry = TimerInit()
	$g_sBufCov = ""
	Local $sArgs = "-Mode Cover -Region " & $g_sRegion
	If $g_sSerial <> "" Then $sArgs &= ' -Serial "' & $g_sSerial & '"'
	Local $bHa = (__HaConfigured() And $g_sHaCover <> "")
	If $bHa Then $sArgs &= ' -HaEntity "' & $g_sHaCover & '"' & ($g_bHaInsecure ? " -HaInsecure" : "")
	Local $sTok = (($g_sToken <> "" And Not $g_bClAuthBad) ? $g_sToken : "")
	$g_iPidCov = __RunHelperEnv($sArgs, "BW_TOKEN", $sTok, "BW_HATOKEN", ($bHa ? $g_sHaToken : ""), "BW_HAURLS", ($bHa ? $g_sHaUrlIn & "|" & $g_sHaUrlOut : ""))
	If Not $g_iPidCov Then $g_sCovMsg = "PowerShell konnte nicht gestartet werden"
EndFunc   ;==>__CoverCheck

Func __PollCover()
	Local $a = __ReadLines($g_iPidCov, $g_sBufCov)
	Local $p, $s
	For $i = 1 To $a[0]
		$s = $a[$i]
		If StringLeft($s, 6) = "COVER|" Then
			$p = StringSplit($s, "|", 2)
			If UBound($p) >= 4 Then __CoverLoad($p[1], $p[2], $p[3])
		ElseIf StringLeft($s, 8) = "NOCOVER|" Then
			__CoverDrop()
			$g_sCovMsg = StringTrimLeft($s, 8)
			If $g_bForceCover Then ; umgeschaltet, aber es gibt kein Bild -> zurueck zur Kamera
				$g_bForceCover = False
				__ViewStyle()
				__ToastShow("Kein Druckobjekt-Bild gefunden")
			EndIf
		ElseIf StringLeft($s, 4) = "ERR|" Then
			; Fehler: altes Bild nur behalten, wenn es noch zum selben Auftrag gehoert
			If $g_sCovKeyLoaded <> $g_sCovKey Then __CoverDrop()
			$g_sCovMsg = StringTrimLeft($s, 4)
		EndIf
	Next
EndFunc   ;==>__PollCover

; Bild aus Datei in den Speicher holen, Datei danach loeschen
Func __CoverLoad($sPath, $sVia, $sTitle)
	Local $hF = FileOpen($sPath, $FO_BINARY)
	If $hF = -1 Then Return
	Local $b = FileRead($hF)
	FileClose($hF)
	FileDelete($sPath)
	If BinaryLen($b) < 100 Then Return
	Local $hImg = _GDIPlus_BitmapCreateFromMemory($b)
	If @error Or $hImg = 0 Then
		$g_sCovMsg = "Bild nicht lesbar"
		Return
	EndIf
	__CoverDrop()
	$g_hCover = $hImg
	$g_sCovVia = $sVia
	$g_sCovTitle = $sTitle
	$g_sCovKeyLoaded = $g_sCovKey
	$g_sCovMsg = ""
	$g_bDirty = True
EndFunc   ;==>__CoverLoad

Func __CoverDrop()
	If $g_hCover Then _GDIPlus_ImageDispose($g_hCover)
	$g_hCover = 0
	$g_sCovVia = ""
	$g_sCovTitle = ""
	$g_sCovKeyLoaded = ""
	$g_bDirty = True
EndFunc   ;==>__CoverDrop

; beim Neuverbinden / Druckerwechsel: alles zuruecksetzen
Func __CoverStop()
	If $g_iPidCov Then ProcessClose($g_iPidCov)
	$g_iPidCov = 0
	$g_sBufCov = ""
	$g_sCovKey = ""
	$g_tCovTry = 0
	$g_sCovMsg = ""
	__CoverDrop()
EndFunc   ;==>__CoverStop

; Herkunft fuer die Plakette. Cloud: passt der Auftragsname nicht zum laufenden Druck,
; ist es nur der letzte Cloud-Auftrag (z. B. Druck von SD-Karte oder im LAN-Modus gestartet).
Func __CoverLabel()
	If $g_sCovVia <> "Cloud" Then Return "Druckobjekt (HA)"
	Local $sJob = __St("job")
	If $sJob <> "" And $g_sCovTitle <> "" And Not StringInStr($sJob, $g_sCovTitle) And Not StringInStr($g_sCovTitle, $sJob) Then Return "Letzter Cloud-Auftrag"
	Return "Druckobjekt (Cloud)"
EndFunc   ;==>__CoverLabel

Func __NoCamWhy()
	If $g_bForceCover And __LiveFrameOk() Then Return "umgeschaltet"
	If Not $g_bCamOn Then Return "Kamera aus"
	If $g_hFrame <> 0 And $g_tFrame <> 0 Then Return "Kamerabild seit " & __AgeText(Int(TimerDiff($g_tFrame) / 1000)) & " weg"
	If __Away() Then Return "unterwegs"
	Return "kein Kamerabild"
EndFunc   ;==>__NoCamWhy

; Knopf "Bild" oben / Rechtsklick: Kamerabild <-> Druckobjekt
Func _ViewToggle()
	$g_bForceCover = Not $g_bForceCover
	If $g_bForceCover And $g_hCover = 0 Then
		If Not __CoverSrc() Then
			$g_bForceCover = False
			__ToastShow("Druckobjekt: Home Assistant oder Bambu-Konto einrichten")
			Return
		EndIf
		; sofort holen
		$g_sCovKey = ""
		$g_tCovTry = 0
		__CoverCheck()
		__ToastShow("Druckobjekt wird geladen …")
	Else
		__ToastShow($g_bForceCover ? "Druckobjekt" : "Kamerabild")
	EndIf
	__ViewStyle()
	$g_bDirty = True
EndFunc   ;==>_ViewToggle

Func __ViewStyle()
	GUICtrlSetColor($g_idBtnView, $g_bForceCover ? $C_GREEN : $C_TXT2)
EndFunc   ;==>__ViewStyle

; Status-Overlay unten im Bild
Func __DrawOverlay($hG, $w, $h, $fS)
	If Not $g_bHaveSt Then
		Local $sEffMsg, $sVia
		If __StatusEff($sEffMsg, $sVia) = 4 And $sEffMsg <> "" Then __Badge($hG, "Status: " & $sEffMsg, $w / 2, $h - 30 * $fS, $fS, 0xD9303338, 0xFFE5484D, 1)
		Return
	EndIf
	Local $sState = __St("st"), $sStateTxt, $iStateCol
	__StateInfo($sState, $sStateTxt, $iStateCol)
	Local $bActive = __IsActive($sState)
	Local $iPct = Int(__St("pct")), $iRem = Int(__St("rem"))
	Local $sJob = __St("job")
	Local $bOld = (TimerDiff($g_tSt) > 120000) ; seit 2 Min. keine Daten
	If $bOld Then $iStateCol = 0xFF5A5F68
	Local $fF = 12 * $fS          ; Schriftgroesse in Pixel
	Local $fPad = 10 * $fS

	; Zeile "Fortschritt": 42 %  ·  Rest 1 h 13 min  ·  fertig 14:27
	Local $sProg = ""
	If $bActive Then
		$sProg = $iPct & " %"
		If $sState = "RUNNING" And __St("rem") <> "" Then $sProg &= "   ·   Rest " & __DurText($iRem) & "   ·   fertig " & __EtaText($iRem)
	ElseIf $g_iOverlay = 1 Then ; kompakt: rechts steht schon der Zustand, links der Auftrag
		$sProg = ($sJob <> "" ? $sJob : "Kein Druck aktiv")
	ElseIf $sState = "FINISH" Then
		$sProg = "Druck beendet"
	ElseIf $sState = "FAILED" Then
		$sProg = "Druck abgebrochen"
	Else
		$sProg = "Kein Druck aktiv"
	EndIf
	If __St("err") <> "" And __St("err") <> "0" Then $sProg &= "   ·   Fehlercode 0x" & Hex(Int(__St("err")), 8)
	If $bOld Then $sProg &= "   ·   (veraltet)"

	If $g_iOverlay = 1 Then
		; --- kompakt: eine Zeile + Fortschrittsbalken
		Local $hB = 26 * $fS
		__Fill($hG, 0, $h - $hB, $w, $hB, 0xB4101214)
		__Txt($hG, $sProg, $fF, 0, 0xFFE8EAED, $fPad, $h - $hB, $w - 2 * $fPad - 90 * $fS, $hB - 3 * $fS, 0, 1)
		__Txt($hG, $sStateTxt, $fF, 1, $iStateCol, $w - $fPad - 100 * $fS, $h - $hB, 100 * $fS, $hB - 3 * $fS, 2, 1)
		__ProgressBar($hG, 0, $h - 3 * $fS, $w, 3 * $fS, ($bActive Or $sState = "FINISH" ? $iPct : 0), $iStateCol)
		Return
	EndIf

	; --- ausfuehrlich: drei Zeilen
	Local $sAms = __St("amsl")
	Local $hL = 20 * $fS, $hP = 3 * $fS + 8 * $fS + ($sAms <> "" ? 4 : 3) * $hL
	Local $y = $h - $hP
	__Fill($hG, 0, $y, $w, $hP, 0xC4101214)
	$y += 6 * $fS
	; Zeile 1: Auftrag + Zustands-Plakette
	Local $fBadgeW = __Badge($hG, $sStateTxt, $w - $fPad, $y + $hL / 2 - 9 * $fS, $fS, $iStateCol, 0xFFFFFFFF, 2)
	__Txt($hG, ($sJob <> "" ? $sJob : "–"), $fF + 1, 1, 0xFFFFFFFF, $fPad, $y, $w - 2 * $fPad - $fBadgeW - 8 * $fS, $hL, 0, 1)
	$y += $hL
	; Zeile 2: Fortschritt, Schicht, Restzeit
	Local $sL2 = $sProg
	If $bActive And __St("tlay") <> "" And __St("tlay") <> "0" Then $sL2 = $iPct & " %   ·   Schicht " & __St("lay") & "/" & __St("tlay") & StringTrimLeft($sProg, StringLen($iPct & " %"))
	__Txt($hG, $sL2, $fF, 0, 0xFFE8EAED, $fPad, $y, $w - 2 * $fPad, $hL, 0, 1)
	$y += $hL
	; Zeile 3: Temperaturen + Filament
	Local $sT
	If __St("dual") = "1" Then ; H2D: linke und rechte Duese
		$sT = "Düse L " & __St("nozL") & "/" & __St("noztL") & " · R " & __St("nozR") & "/" & __St("noztR") & " °C     Bett " & __St("bed") & "/" & __St("bedt") & " °C"
	Else
		$sT = "Düse " & __St("noz") & "/" & __St("nozt") & " °C     Bett " & __St("bed") & "/" & __St("bedt") & " °C"
		If __St("spd") <> "" Then $sT &= "     " & __SpeedText(__St("spd"))
	EndIf
	__Txt($hG, $sT, $fF - 1, 0, 0xFF9AA0A8, $fPad, $y, $w - 2 * $fPad - 140 * $fS, $hL, 0, 1)
	If __St("fil") <> "" Or __St("col") <> "" Then
		Local $sSlot = (__St("ams") <> "" And __St("ams") <> "1" ? " · AMS " & __St("ams") & "/" & __St("slot") : " · Slot " & __St("slot"))
		Local $sFil = __St("fil") & (__St("slot") <> "" ? (__St("slot") = "Ext" ? " · extern" : $sSlot) : "")
		__Txt($hG, $sFil, $fF - 1, 0, 0xFF9AA0A8, $w - $fPad - 120 * $fS, $y, 120 * $fS, $hL, 2, 1)
		Local $iCol = __ColorFromBambu(__St("col"))
		If $iCol <> 0 Then
			Local $fD = 10 * $fS
			Local $fTw = __TxtWidth($hG, $sFil, $fF - 1, 0)
			Local $xDot = $w - $fPad - $fTw - $fD - 6 * $fS
			Local $hBr = _GDIPlus_BrushCreateSolid($iCol)
			_GDIPlus_GraphicsFillEllipse($hG, $xDot, $y + ($hL - $fD) / 2, $fD, $fD, $hBr)
			_GDIPlus_BrushDispose($hBr)
			Local $hPen = _GDIPlus_PenCreate(0x66FFFFFF, 1)
			_GDIPlus_GraphicsDrawEllipse($hG, $xDot, $y + ($hL - $fD) / 2, $fD, $fD, $hPen)
			_GDIPlus_PenDispose($hPen)
		EndIf
	EndIf
	; Zeile 4: alle AMS-Slots (aktiver Slot hervorgehoben)
	If $sAms <> "" Then __DrawAmsRow($hG, $sAms, $fPad, $y + $hL, $w - 2 * $fPad, $hL, $fF - 1, $fS)
	__ProgressBar($hG, 0, $h - 3 * $fS, $w, 3 * $fS, ($bActive Or $sState = "FINISH" ? $iPct : 0), $iStateCol)
EndFunc   ;==>__DrawOverlay

; AMS-Uebersicht: "Einheit.Slot,Typ,RRGGBB,aktiv;..." -> Farbpunkt + Slot + Material
Func __DrawAmsRow($hG, $sAms, $x, $y, $wMax, $hL, $fPx, $fS)
	Local $a = StringSplit($sAms, ";", 2)
	Local $bMulti = False, $p
	For $i = 0 To UBound($a) - 1
		If StringRegExp($a[$i], "^([2-9]|HT)") Then $bMulti = True
	Next
	Local $fD = 9 * $fS, $xC = $x, $sLbl, $iCol, $bAct, $fTw, $fYd, $hBr, $hPen
	For $i = 0 To UBound($a) - 1
		$p = StringSplit($a[$i], ",", 2)
		If UBound($p) < 4 Then ContinueLoop
		$bAct = ($p[3] = "1")
		$sLbl = ($p[0] = "E" ? "Ext" : ($bMulti ? $p[0] : StringRegExpReplace($p[0], "^\d+\.", ""))) & " " & ($p[1] = "" ? "–" : $p[1])
		$fTw = __TxtWidth($hG, $sLbl, $fPx, ($bAct ? 1 : 0))
		If $xC + $fD + 4 * $fS + $fTw > $x + $wMax Then ExitLoop
		$fYd = $y + ($hL - $fD) / 2
		$iCol = __ColorFromBambu($p[2])
		If $p[1] <> "" And $iCol <> 0 Then
			$hBr = _GDIPlus_BrushCreateSolid($iCol)
			_GDIPlus_GraphicsFillEllipse($hG, $xC, $fYd, $fD, $fD, $hBr)
			_GDIPlus_BrushDispose($hBr)
		EndIf
		$hPen = _GDIPlus_PenCreate(($bAct ? 0xFF00AE42 : 0x66FFFFFF), ($bAct ? 2 : 1))
		_GDIPlus_GraphicsDrawEllipse($hG, $xC, $fYd, $fD, $fD, $hPen)
		_GDIPlus_PenDispose($hPen)
		__Txt($hG, $sLbl, $fPx, ($bAct ? 1 : 0), ($bAct ? 0xFFFFFFFF : 0xFF9AA0A8), $xC + $fD + 4 * $fS, $y, $fTw + 8, $hL, 0, 1)
		$xC += $fD + 4 * $fS + $fTw + 14 * $fS
	Next
EndFunc   ;==>__DrawAmsRow

Func __StateInfo($sState, ByRef $sTxt, ByRef $iCol)
	Switch $sState
		Case "RUNNING"
			$sTxt = "Druckt"
			$iCol = 0xFF00AE42
		Case "PAUSE"
			$sTxt = "Pausiert"
			$iCol = 0xFFF5A524
		Case "PREPARE"
			$sTxt = "Vorbereitung"
			$iCol = 0xFFF5A524
		Case "SLICING"
			$sTxt = "Slicing"
			$iCol = 0xFFF5A524
		Case "FINISH"
			$sTxt = "Fertig"
			$iCol = 0xFF3B9EFF
		Case "FAILED"
			$sTxt = "Abgebrochen"
			$iCol = 0xFFE5484D
		Case "INIT"
			$sTxt = "Startet"
			$iCol = 0xFFF5A524
		Case "OFFLINE"
			$sTxt = "Offline"
			$iCol = 0xFF5A5F68
		Case "IDLE"
			$sTxt = "Bereit"
			$iCol = 0xFF9AA0A8
		Case ""
			$sTxt = "–"
			$iCol = 0xFF9AA0A8
		Case Else
			$sTxt = $sState
			$iCol = 0xFF9AA0A8
	EndSwitch
EndFunc   ;==>__StateInfo

; spd_lvl: 1 leise, 2 Standard, 3 Sport, 4 Ludicrous (Stufen wie in Bambu Studio)
Func __SpeedText($s)
	Switch $s
		Case "1"
			Return "Leise"
		Case "2"
			Return "Standard"
		Case "3"
			Return "Sport"
		Case "4"
			Return "Ludicrous"
	EndSwitch
	Return ""
EndFunc   ;==>__SpeedText

; Bambu liefert Farben als RRGGBBAA -> ARGB (Alpha immer voll)
Func __ColorFromBambu($s)
	If Not StringRegExp($s, "^[0-9A-Fa-f]{6}") Then Return 0
	Return BitOR(0xFF000000, Dec(StringLeft($s, 6)))
EndFunc   ;==>__ColorFromBambu

Func __DurText($iMin)
	If $iMin < 60 Then Return $iMin & " min"
	Return Int($iMin / 60) & " h " & StringFormat("%02d", Mod($iMin, 60)) & " min"
EndFunc   ;==>__DurText

; Fertig-Uhrzeit: "14:27", "morgen 06:10" oder "24.10. 09:00"
Func __EtaText($iMin)
	Local $s = _DateAdd("n", $iMin, _NowCalc())
	Local $sHM = StringMid($s, 12, 5)
	Local $sDay = StringLeft($s, 10)
	If $sDay = StringLeft(_NowCalc(), 10) Then Return $sHM
	If $sDay = StringLeft(_DateAdd("D", 1, _NowCalc()), 10) Then Return "morgen " & $sHM
	Return StringMid($s, 9, 2) & "." & StringMid($s, 6, 2) & ". " & $sHM
EndFunc   ;==>__EtaText

Func __AgeText($iSec)
	If $iSec < 60 Then Return $iSec & " s"
	If $iSec < 3600 Then Return Int($iSec / 60) & " min"
	Return Int($iSec / 3600) & " h"
EndFunc   ;==>__AgeText

; ---------------------------------------------------------------- Zeichenhelfer
Func __Fill($hG, $x, $y, $w, $h, $iArgb)
	Local $hBr = _GDIPlus_BrushCreateSolid($iArgb)
	_GDIPlus_GraphicsFillRect($hG, $x, $y, $w, $h, $hBr)
	_GDIPlus_BrushDispose($hBr)
EndFunc   ;==>__Fill

Func __RRect($hG, $x, $y, $w, $h, $r, $iArgb)
	If $r * 2 > $h Then $r = $h / 2
	If $r * 2 > $w Then $r = $w / 2
	Local $d = $r * 2
	Local $hP = _GDIPlus_PathCreate()
	_GDIPlus_PathAddArc($hP, $x, $y, $d, $d, 180, 90)
	_GDIPlus_PathAddArc($hP, $x + $w - $d, $y, $d, $d, 270, 90)
	_GDIPlus_PathAddArc($hP, $x + $w - $d, $y + $h - $d, $d, $d, 0, 90)
	_GDIPlus_PathAddArc($hP, $x, $y + $h - $d, $d, $d, 90, 90)
	_GDIPlus_PathCloseFigure($hP)
	Local $hBr = _GDIPlus_BrushCreateSolid($iArgb)
	_GDIPlus_GraphicsFillPath($hG, $hP, $hBr)
	_GDIPlus_BrushDispose($hBr)
	_GDIPlus_PathDispose($hP)
EndFunc   ;==>__RRect

Func __ProgressBar($hG, $x, $y, $w, $h, $iPct, $iCol)
	__Fill($hG, $x, $y, $w, $h, 0x40FFFFFF)
	If $iPct > 100 Then $iPct = 100
	If $iPct > 0 Then __Fill($hG, $x, $y, $w * $iPct / 100, $h, $iCol)
EndFunc   ;==>__ProgressBar

; Text in einem Rechteck; $iAlign 0 links, 1 Mitte, 2 rechts; zu lang -> "…"
Func __Txt($hG, $s, $fPx, $iStyle, $iArgb, $x, $y, $w, $h, $iAlign = 0, $iLAlign = 1, $hFam = 0)
	If $hFam = 0 Then $hFam = $g_hFamUI
	If $w < 4 Or $h < 4 Then Return
	Local $hFont = _GDIPlus_FontCreate($hFam, $fPx + $g_iFontAdd, $iStyle, 2) ; 2 = Pixel
	Local $hFmt = _GDIPlus_StringFormatCreate(0x1000)           ; kein Umbruch
	_GDIPlus_StringFormatSetAlign($hFmt, $iAlign)
	_GDIPlus_StringFormatSetLineAlign($hFmt, $iLAlign)
	DllCall($__g_hGDIPDll, "int", "GdipSetStringFormatTrimming", "handle", $hFmt, "int", 3) ; EllipsisCharacter
	Local $hBr = _GDIPlus_BrushCreateSolid($iArgb)
	Local $tR = _GDIPlus_RectFCreate($x, $y, $w, $h)
	_GDIPlus_GraphicsDrawStringEx($hG, $s, $hFont, $tR, $hFmt, $hBr)
	_GDIPlus_BrushDispose($hBr)
	_GDIPlus_StringFormatDispose($hFmt)
	_GDIPlus_FontDispose($hFont)
EndFunc   ;==>__Txt

Func __TxtWidth($hG, $s, $fPx, $iStyle)
	Local $hFont = _GDIPlus_FontCreate($g_hFamUI, $fPx + $g_iFontAdd, $iStyle, 2)
	Local $hFmt = _GDIPlus_StringFormatCreate(0x1000)
	Local $tL = _GDIPlus_RectFCreate(0, 0, 4000, 200)
	Local $a = _GDIPlus_GraphicsMeasureString($hG, $s, $hFont, $tL, $hFmt)
	Local $fW = 0
	If IsArray($a) Then $fW = DllStructGetData($a[0], "Width")
	_GDIPlus_StringFormatDispose($hFmt)
	_GDIPlus_FontDispose($hFont)
	Return $fW
EndFunc   ;==>__TxtWidth

; abgerundete Plakette mit Text; $iAnchor 0 = $x ist linker Rand, 1 = Mitte, 2 = rechter Rand. Liefert die Breite.
Func __Badge($hG, $s, $x, $y, $fS, $iBg, $iFg, $iAnchor)
	Local $fPx = 11.5 * $fS, $fH = 20 * $fS, $fP = 9 * $fS
	Local $fW = __TxtWidth($hG, $s, $fPx, 1) + 2 * $fP
	Local $aCli = WinGetClientSize($g_hGui)
	Local $fMax = (IsArray($aCli) ? $aCli[0] - 2 * $PAD - 16 : 600)
	If $fW > $fMax Then $fW = $fMax
	Local $x0 = ($iAnchor = 1 ? $x - $fW / 2 : ($iAnchor = 2 ? $x - $fW : $x))
	__RRect($hG, $x0, $y, $fW, $fH, $fH / 2, $iBg)
	__Txt($hG, $s, $fPx, 1, $iFg, $x0 + $fP / 2, $y, $fW - $fP, $fH, 1, 1)
	Return $fW
EndFunc   ;==>__Badge

; =============================================================================
;  Fenster: ziehen, Groesse, Seitenverhaeltnis, Hover
; =============================================================================
; Ziehen an Leiste, Titel oder Bild (Fenster ohne Windows-Titelleiste)
Func _DragAny()
	Local $hWin = @GUI_WinHandle
	Local $a = GUIGetCursorInfo($hWin)
	If @error Then Return
	Local $id = $a[4]
	If $hWin = $g_hGui Then
		If $id <> 0 And $id <> $g_idPic And $id <> $g_idDotCam And $id <> $g_idDotMq Then Return
	Else
		If $id <> 0 And Not __IsDragLabel($id) Then Return
	EndIf
	DllCall("user32.dll", "bool", "ReleaseCapture")
	DllCall("user32.dll", "lresult", "SendMessageW", "hwnd", $hWin, "uint", $BW_WM_NCLBUTTONDOWN, "wparam", 2, "lparam", 0) ; HTCAPTION
EndFunc   ;==>_DragAny

; In Dialogen sind reine Text-Labels (ohne Klickfunktion) ebenfalls Greifflaechen
Func __IsDragLabel($id)
	Return MapExists($g_mDragLbl, $id)
EndFunc   ;==>__IsDragLabel

; Raender als Groessen-Griffe melden
Func _WM_NCHITTEST($hWnd, $iMsg, $wParam, $lParam)
	#forceref $iMsg, $wParam
	If $hWnd <> $g_hGui Then Return $GUI_RUNDEFMSG
	Local $x = BitAND($lParam, 0xFFFF), $y = BitAND(BitShift($lParam, 16), 0xFFFF)
	If $x > 32767 Then $x -= 65536
	If $y > 32767 Then $y -= 65536
	Local $p = WinGetPos($g_hGui)
	If Not IsArray($p) Then Return $GUI_RUNDEFMSG
	Local $cx = $x - $p[0], $cy = $y - $p[1], $B = $PAD + 1
	Local $bL = ($cx < $B), $bR = ($cx >= $p[2] - $B), $bT = ($cy < 4), $bB = ($cy >= $p[3] - $B)
	If $bT And $bL Then Return 13
	If $bT And $bR Then Return 14
	If $bB And $bL Then Return 16
	If $bB And $bR Then Return 17
	If $bL Then Return 10
	If $bR Then Return 11
	If $bT Then Return 12
	If $bB Then Return 15
	Return $GUI_RUNDEFMSG
EndFunc   ;==>_WM_NCHITTEST

Func _WM_GETMINMAXINFO($hWnd, $iMsg, $wParam, $lParam)
	#forceref $iMsg, $wParam
	If $hWnd <> $g_hGui Then Return $GUI_RUNDEFMSG
	Local $t = DllStructCreate("int;int;int;int;int;int;int MinX;int MinY;int;int", $lParam)
	DllStructSetData($t, "MinX", $MIN_W)
	DllStructSetData($t, "MinY", $MIN_H)
	Return 0
EndFunc   ;==>_WM_GETMINMAXINFO

; Seitenverhaeltnis des Bildes beim Ziehen halten
Func _WM_SIZING($hWnd, $iMsg, $wParam, $lParam)
	#forceref $iMsg
	If $hWnd <> $g_hGui Or Not $g_bAspect Then Return $GUI_RUNDEFMSG
	Local $t = DllStructCreate("long L;long T;long R;long B", $lParam)
	Local $iL = DllStructGetData($t, "L"), $iT = DllStructGetData($t, "T")
	Local $iR = DllStructGetData($t, "R"), $iB = DllStructGetData($t, "B")
	Local $iChW = 2 * $PAD, $iChH = $BAR_H + $PAD
	Switch $wParam
		Case 3, 6 ; oben / unten -> Breite folgt der Hoehe
			Local $iW = Round(($iB - $iT - $iChH) * $g_fAspect) + $iChW
			If $iW < $MIN_W Then $iW = $MIN_W
			DllStructSetData($t, "R", $iL + $iW)
		Case Else ; links, rechts, Ecken -> Hoehe folgt der Breite
			Local $iH = Round(($iR - $iL - $iChW) / $g_fAspect) + $iChH
			If $iH < $MIN_H Then $iH = $MIN_H
			If $wParam = 4 Or $wParam = 5 Then ; obere Ecken
				DllStructSetData($t, "T", $iB - $iH)
			Else
				DllStructSetData($t, "B", $iT + $iH)
			EndIf
	EndSwitch
	Return True
EndFunc   ;==>_WM_SIZING

Func _WM_SIZE($hWnd, $iMsg, $wParam, $lParam)
	#forceref $iMsg, $wParam, $lParam
	If $hWnd = $g_hGui Then $g_bDirty = True
	Return $GUI_RUNDEFMSG
EndFunc   ;==>_WM_SIZE

; Symbol-Knoepfe: heller beim Ueberfahren, Schliessen rot
Func _HoverCheck()
	If Not WinActive($g_hGui) And $g_idHover = 0 Then Return
	Local $a = GUIGetCursorInfo($g_hGui)
	Local $id = (@error ? 0 : $a[4])
	Local $bBtn = False
	For $b In $g_aBarBtns
		If $b = $id Then $bBtn = True
	Next
	If Not $bBtn Then $id = 0
	If $id = $g_idHover Then Return
	If $g_idHover <> 0 Then __BarBtnPaint($g_idHover, False)
	If $id <> 0 Then __BarBtnPaint($id, True)
	$g_idHover = $id
EndFunc   ;==>_HoverCheck

Func __BarBtnPaint($id, $bHover)
	If $id = $g_idBtnClose Then
		GUICtrlSetBkColor($id, $bHover ? $C_RED : $GUI_BKCOLOR_TRANSPARENT)
		GUICtrlSetColor($id, $bHover ? 0xFFFFFF : $C_TXT2)
		Return
	EndIf
	If $id = $g_idBtnPin And $g_bOnTop Then
		GUICtrlSetColor($id, $C_GREEN)
	ElseIf $id = $g_idBtnOvl And $g_iOverlay > 0 Then
		GUICtrlSetColor($id, $bHover ? 0xFFFFFF : $C_GREEN)
	ElseIf $id = $g_idBtnLight And __St("light") = "on" Then
		GUICtrlSetColor($id, $bHover ? 0xFFFFFF : $C_AMBER)
	ElseIf $id = $g_idBtnView And $g_bForceCover Then
		GUICtrlSetColor($id, $bHover ? 0xFFFFFF : $C_GREEN)
	Else
		GUICtrlSetColor($id, $bHover ? 0xFFFFFF : $C_TXT2)
	EndIf
	GUICtrlSetBkColor($id, $bHover ? 0x2C3036 : $GUI_BKCOLOR_TRANSPARENT)
EndFunc   ;==>__BarBtnPaint

Func __PinStyle()
	GUICtrlSetData($g_idBtnPin, ChrW($g_bOnTop ? 0xE718 : 0xE77A))
	GUICtrlSetColor($g_idBtnPin, $g_bOnTop ? $C_GREEN : $C_TXT2)
	GUICtrlSetColor($g_idBtnOvl, $g_iOverlay > 0 ? $C_GREEN : $C_TXT2)
	GUICtrlSetState($g_idMnuTop, $g_bOnTop ? $GUI_CHECKED : $GUI_UNCHECKED)
EndFunc   ;==>__PinStyle

; =============================================================================
;  Aktionen
; =============================================================================
Func _CycleOverlay()
	$g_iOverlay = Mod($g_iOverlay + 1, 3)
	IniWrite($INI, "Anzeige", "Overlay", $g_iOverlay)
	__PinStyle()
	__ToastShow("Overlay: " & $OVL_NAMES[$g_iOverlay])
EndFunc   ;==>_CycleOverlay

Func _ToggleOnTop()
	$g_bOnTop = Not $g_bOnTop
	WinSetOnTop($g_hGui, "", $g_bOnTop ? 1 : 0)
	IniWrite($INI, "Fenster", "Vordergrund", $g_bOnTop ? "1" : "0")
	__PinStyle()
	__ToastShow("Immer im Vordergrund: " & ($g_bOnTop ? "an" : "aus"))
EndFunc   ;==>_ToggleOnTop

; speichert das Originalbild der Kamera (ohne Overlay) als JPG
Func _Snapshot()
	If BinaryLen($g_bFrame) = 0 Then
		__ToastShow("Noch kein Bild für einen Snapshot")
		Return
	EndIf
	If Not FileExists($g_sSnapDir) Then DirCreate($g_sSnapDir)
	Local $sFile = $g_sSnapDir & "\BambuWatch_" & @YEAR & "-" & @MON & "-" & @MDAY & "_" & @HOUR & "-" & @MIN & "-" & @SEC & ".jpg"
	Local $hF = FileOpen($sFile, $FO_OVERWRITE + $FO_BINARY)
	If $hF = -1 Then
		__ToastShow("Snapshot fehlgeschlagen – Ordner prüfen")
		Return
	EndIf
	FileWrite($hF, $g_bFrame)
	FileClose($hF)
	__ToastShow(ChrW(0x2713) & " Snapshot gespeichert")
EndFunc   ;==>_Snapshot

Func _SnapFolderOpen()
	If Not FileExists($g_sSnapDir) Then DirCreate($g_sSnapDir)
	ShellExecute($g_sSnapDir)
EndFunc   ;==>_SnapFolderOpen

Func _Minimize()
	If $g_bTray Then
		GUISetState(@SW_HIDE, $g_hGui)
		If $g_bFirstTrayHint Then
			TrayTip($APP_NAME, "Läuft im Infobereich weiter – Klick auf das Symbol holt das Fenster zurück.", 5, $TIP_ICONASTERISK)
			$g_bFirstTrayHint = False
		EndIf
	Else
		GUISetState(@SW_MINIMIZE, $g_hGui)
	EndIf
EndFunc   ;==>_Minimize

Func _TrayShow()
	$g_iTbState = -1 ; Taskleisten-Knopf ist neu -> Fortschritt wieder setzen
	GUISetState(@SW_SHOW, $g_hGui)
	GUISetState(@SW_RESTORE, $g_hGui)
	WinActivate($g_hGui)
	$g_bDirty = True
EndFunc   ;==>_TrayShow

; Groessen-Vorgaben: Bildbreite 400 / 640 / 960 px, Hoehe nach Seitenverhaeltnis
Func _SizeSmall()
	__SizeSet(400)
EndFunc   ;==>_SizeSmall

Func _SizeMedium()
	__SizeSet(640)
EndFunc   ;==>_SizeMedium

Func _SizeLarge()
	__SizeSet(960)
EndFunc   ;==>_SizeLarge

Func __SizeSet($iVW)
	Local $p = WinGetPos($g_hGui)
	If Not IsArray($p) Then Return
	WinMove($g_hGui, "", $p[0], $p[1], $iVW + 2 * $PAD, Round($iVW / $g_fAspect) + $BAR_H + $PAD)
	$g_bDirty = True
EndFunc   ;==>__SizeSet

Func __ToastShow($s)
	$g_sToast = $s
	$g_tToast = TimerInit()
	$g_bDirty = True
EndFunc   ;==>__ToastShow

Func __Notify($sTitle, $sText, $iCol = 0xFF00AE42)
	If Not $g_bNotify Then Return
	__PopupShow($sTitle, $sText, $iCol)
	If $g_bSound Then
		Local $sWav = @WindowsDir & "\Media\Windows Notify System Generic.wav"
		If Not FileExists($sWav) Then $sWav = @WindowsDir & "\Media\notify.wav"
		If FileExists($sWav) Then SoundPlay($sWav, 0)
	EndIf
	__ToastShow($sTitle)
EndFunc   ;==>__Notify

; Meldungsfenster unten rechts: Druckobjekt (oder letztes Kamerabild), Titel, Auftrag. Klick oeffnet BambuWatch.
Func __PopupShow($sTitle, $sText, $iCol)
	__PopupClose()
	Local Const $W = 380, $H = 112
	Local $tR = DllStructCreate("int L;int T;int R;int B")
	DllCall("user32.dll", "bool", "SystemParametersInfoW", "uint", 0x0030, "uint", 0, "struct*", $tR, "uint", 0) ; SPI_GETWORKAREA
	Local $x = DllStructGetData($tR, "R") - $W - 16, $y = DllStructGetData($tR, "B") - $H - 16
	If DllStructGetData($tR, "R") <= 0 Then
		$x = @DesktopWidth - $W - 16
		$y = @DesktopHeight - $H - 64
	EndIf
	$g_hPop = GUICreate($sTitle & " – BambuWatch", $W, $H, $x, $y, $WS_POPUP, BitOR($WS_EX_TOOLWINDOW, $WS_EX_TOPMOST))
	GUISetBkColor($C_BG, $g_hPop)
	__WinStyleModern($g_hPop)
	$g_idPopPic = GUICtrlCreatePic("", 0, 0, $W, $H)
	GUICtrlSetOnEvent($g_idPopPic, "_PopupClick")
	GUICtrlSetCursor($g_idPopPic, 0)
	; Inhalt mit GDI+ zeichnen
	Local $hBmp = _GDIPlus_BitmapCreateFromScan0($W, $H)
	Local $hG = _GDIPlus_ImageGetGraphicsContext($hBmp)
	_GDIPlus_GraphicsSetSmoothingMode($hG, 4)
	_GDIPlus_GraphicsSetTextRenderingHint($hG, 4)
	_GDIPlus_GraphicsClear($hG, 0xFF1B1D21)
	__Fill($hG, 0, 0, 5, $H, $iCol)
	Local $xT = 18
	Local $hImg = ($g_hCover <> 0 ? $g_hCover : $g_hFrame)
	If $hImg <> 0 Then
		Local $iw = _GDIPlus_ImageGetWidth($hImg), $ih = _GDIPlus_ImageGetHeight($hImg)
		If $iw > 0 And $ih > 0 Then
			__Fill($hG, 14, 12, 88, 88, 0xFF15171B)
			Local $f = (88 / $iw < 88 / $ih ? 88 / $iw : 88 / $ih)
			_GDIPlus_GraphicsSetInterpolationMode($hG, 7)
			_GDIPlus_GraphicsDrawImageRect($hG, $hImg, 14 + (88 - $iw * $f) / 2, 12 + (88 - $ih * $f) / 2, $iw * $f, $ih * $f)
			$xT = 116
		EndIf
	EndIf
	__Txt($hG, $sTitle, 15, 1, 0xFFFFFFFF, $xT, 14, $W - $xT - 14, 24, 0, 1)
	__Txt($hG, $sText, 12, 0, 0xFFE8EAED, $xT, 42, $W - $xT - 14, 22, 0, 1)
	__Txt($hG, $g_sPrinterName & "  ·  " & @HOUR & ":" & @MIN & "  ·  Klick öffnet BambuWatch", 10.5, 0, 0xFF9AA0A8, $xT, $H - 34, $W - $xT - 14, 20, 0, 1)
	Local $hH = _GDIPlus_BitmapCreateHBITMAPFromBitmap($hBmp)
	_GDIPlus_GraphicsDispose($hG)
	_GDIPlus_BitmapDispose($hBmp)
	Local $hOld = GUICtrlSendMsg($g_idPopPic, $BW_STM_SETIMAGE, $BW_IMAGE_BITMAP, $hH)
	If $hOld Then DllCall("gdi32.dll", "bool", "DeleteObject", "handle", $hOld)
	GUISetState(@SW_SHOWNOACTIVATE, $g_hPop)
	GUISwitch($g_hGui)
	$g_tPop = TimerInit()
EndFunc   ;==>__PopupShow

Func _PopupClick()
	__PopupClose()
	_TrayShow()
EndFunc   ;==>_PopupClick

Func __PopupClose()
	If $g_hPop = 0 Then Return
	Local $hCur = GUICtrlSendMsg($g_idPopPic, $BW_STM_GETIMAGE, $BW_IMAGE_BITMAP, 0)
	GUIDelete($g_hPop)
	If $hCur Then DllCall("gdi32.dll", "bool", "DeleteObject", "handle", $hCur)
	$g_hPop = 0
	GUISwitch($g_hGui)
EndFunc   ;==>__PopupClose

; ---------------------------------------------------------------- Druckerlicht
; im Heimnetz direkt (MQTT), sonst ueber Home Assistant; Cloud nicht (nicht geprueft)
Func _LightToggle()
	If $g_iPidLight Then Return
	If $g_bStudioPause Then
		__ToastShow("Pausiert – Licht bitte in Bambu Studio schalten")
		Return
	EndIf
	Local $sNew = (__St("light") = "on" ? "off" : "on")
	If __LanFresh() And __CanLan(True) Then
		$g_iPidLight = __RunHelperEnv('-Mode Light -Ip "' & $g_sIp & '" -Serial "' & $g_sSerial & '" -LightState ' & $sNew, "BW_CODE", $g_sCode)
	ElseIf __HaConfigured() And $g_sHaLight <> "" Then
		$g_iPidLight = __RunHelperEnv('-Mode HaLight -HaEntity "' & $g_sHaLight & '" -LightState ' & $sNew & ($g_bHaInsecure ? " -HaInsecure" : ""), _
				"BW_HATOKEN", $g_sHaToken, "BW_HAURLS", $g_sHaUrlIn & "|" & $g_sHaUrlOut)
	Else
		__ToastShow("Licht: nur im Heimnetz oder über Home Assistant")
		Return
	EndIf
	If Not $g_iPidLight Then Return
	$g_sBufLight = ""
	$g_tLight = TimerInit()
EndFunc   ;==>_LightToggle

Func __PollLight()
	Local $a = __ReadLines($g_iPidLight, $g_sBufLight)
	Local $s
	For $i = 1 To $a[0]
		$s = $a[$i]
		If StringLeft($s, 6) = "LIGHT|" Then
			$g_mSt["light"] = StringTrimLeft($s, 6) ; sofort anzeigen, der naechste Status bestaetigt es
			__LightStyle()
			__ToastShow("Licht " & (__St("light") = "on" ? "an" : "aus"))
		ElseIf StringLeft($s, 9) = "ERR|AUTH|" Then
			__ToastShow("Licht: " & StringTrimLeft($s, 9))
		ElseIf StringLeft($s, 4) = "ERR|" Then
			__ToastShow(StringTrimLeft($s, 4))
		EndIf
	Next
	If $g_iPidLight And TimerDiff($g_tLight) > 20000 Then
		ProcessClose($g_iPidLight)
		$g_iPidLight = 0
	EndIf
EndFunc   ;==>__PollLight

; Gluehbirne oben: gelb = an, grau = aus/unbekannt
Func __LightStyle()
	Static $sLast = "-"
	Local $s = __St("light")
	If $s = $sLast Then Return
	$sLast = $s
	GUICtrlSetColor($g_idBtnLight, ($s = "on" ? $C_AMBER : $C_TXT2))
EndFunc   ;==>__LightStyle

; ---------------------------------------------------------------- Taskleiste (ITaskbarList3)
Func __TaskbarInit()
	$g_oTaskbar = ObjCreateInterface("{56FDF344-FD6D-11d0-958A-006097C9A090}", "{EA1AFB91-9E28-4B86-90E9-9E9F8A5EEE6F}", _
			"HrInit hresult();AddTab hresult(hwnd);DeleteTab hresult(hwnd);ActivateTab hresult(hwnd);SetActiveAlt hresult(hwnd);" & _
			"MarkFullscreenWindow hresult(hwnd;int);SetProgressValue hresult(hwnd;uint64;uint64);SetProgressState hresult(hwnd;int);")
	If IsObj($g_oTaskbar) Then
		$g_oTaskbar.HrInit()
	Else
		$g_oTaskbar = 0
	EndIf
EndFunc   ;==>__TaskbarInit

; Fortschritt im Taskleisten-Symbol: gruen druckt, gelb pausiert, rot abgebrochen, laufend = Vorbereitung
Func __TaskbarUpdate()
	If Not IsObj($g_oTaskbar) Then Return
	Local $iState = 0, $iVal = 0
	If $g_bHaveSt And TimerDiff($g_tSt) < 120000 Then
		$iVal = __Clamp(Int(__St("pct")), 0, 100)
		Switch __St("st")
			Case "RUNNING"
				$iState = 2 ; TBPF_NORMAL
			Case "PAUSE"
				$iState = 8 ; TBPF_PAUSED
			Case "FAILED"
				$iState = 4 ; TBPF_ERROR
			Case "PREPARE", "SLICING", "INIT"
				$iState = 1 ; TBPF_INDETERMINATE
		EndSwitch
	EndIf
	If $iState = $g_iTbState And $iVal = $g_iTbVal Then Return
	$g_oTaskbar.SetProgressState($g_hGui, $iState)
	If $iState = 2 Or $iState = 4 Or $iState = 8 Then $g_oTaskbar.SetProgressValue($g_hGui, $iVal, 100)
	$g_iTbState = $iState
	$g_iTbVal = $iVal
EndFunc   ;==>__TaskbarUpdate

Func __ComErr($oErr)
	; bewusst leer: ein COM-Fehler (z. B. Taskleiste nicht verfuegbar) soll BambuWatch nicht beenden
	#forceref $oErr
EndFunc   ;==>__ComErr

Func _Quit()
	__PopupClose()
	If $g_iPidLight Then ProcessClose($g_iPidLight)
	_HelpersStop()
	_ProbeStop()
	_AccountClose_Proc()
	If $g_iPidHaList Then ProcessClose($g_iPidHaList)
	__SavePos()
	If $g_hFrame Then _GDIPlus_ImageDispose($g_hFrame)
	If $g_hFamUI Then _GDIPlus_FontFamilyDispose($g_hFamUI)
	If $g_hFamSym Then _GDIPlus_FontFamilyDispose($g_hFamSym)
	_GDIPlus_Shutdown()
	FileDelete($WORK & "\bw_frame_*.jpg")
	FileDelete($WORK & "\bw_ha_*.jpg")
	Exit
EndFunc   ;==>_Quit

Func __SavePos()
	Local $iWS = WinGetState($g_hGui)
	If BitAND($iWS, $WIN_STATE_MINIMIZED) Then Return
	Local $p = WinGetPos($g_hGui)
	If Not IsArray($p) Or $p[0] <= -30000 Then Return
	IniWrite($INI, "Fenster", "X", $p[0])
	IniWrite($INI, "Fenster", "Y", $p[1])
	IniWrite($INI, "Fenster", "W", $p[2])
	IniWrite($INI, "Fenster", "H", $p[3])
EndFunc   ;==>__SavePos

; Position pruefen: liegt das Fenster (z. B. nach Abstecken eines Monitors) ausserhalb, wird es zentriert
Func __PosCheck()
	If $g_iW < $MIN_W Then $g_iW = 560
	If $g_iH < $MIN_H Then $g_iH = Round(($g_iW - 2 * $PAD) / $g_fAspect) + $BAR_H + $PAD
	Local $aVX = DllCall("user32.dll", "int", "GetSystemMetrics", "int", 76) ; SM_XVIRTUALSCREEN
	Local $aVY = DllCall("user32.dll", "int", "GetSystemMetrics", "int", 77)
	Local $aVW = DllCall("user32.dll", "int", "GetSystemMetrics", "int", 78)
	Local $aVH = DllCall("user32.dll", "int", "GetSystemMetrics", "int", 79)
	If @error Then Return
	Local $iVX = $aVX[0], $iVY = $aVY[0], $iVW = $aVW[0], $iVH = $aVH[0]
	If $g_iX = -1 And $g_iY = -1 Then Return
	If $g_iX < $iVX - $g_iW + 80 Or $g_iX > $iVX + $iVW - 80 Or $g_iY < $iVY Or $g_iY > $iVY + $iVH - 60 Then
		$g_iX = -1
		$g_iY = -1
	EndIf
EndFunc   ;==>__PosCheck

; =============================================================================
;  Einstellungen
; =============================================================================
Func _SettingsLoad()
	$g_iOverlay = Int(IniRead($INI, "Anzeige", "Overlay", 1))
	If $g_iOverlay < 0 Or $g_iOverlay > 2 Then $g_iOverlay = 1
	$g_bAspect = (IniRead($INI, "Anzeige", "Seitenverhaeltnis", "1") = "1")
	$g_bOnTop = (IniRead($INI, "Fenster", "Vordergrund", "0") = "1")
	$g_bTray = (IniRead($INI, "Fenster", "Infobereich", "1") = "1")
	$g_iAlpha = Int(IniRead($INI, "Fenster", "Deckkraft", 255))
	If $g_iAlpha < 100 Or $g_iAlpha > 255 Then $g_iAlpha = 255
	$g_bNotify = (IniRead($INI, "Meldungen", "Hinweis", "1") = "1")
	$g_bSound = (IniRead($INI, "Meldungen", "Ton", "1") = "1")
	$g_bStudioWatch = (IniRead($INI, "Fenster", "BambuStudioPause", "1") = "1")
	$g_sSnapDir = IniRead($INI, "Snapshots", "Ordner", @MyDocumentsDir & "\BambuWatch")
	$g_iFontAdd = __Clamp(Int(IniRead($INI, "Anzeige", "Schrift", 1)), 0, 3)
	$g_iX = Int(IniRead($INI, "Fenster", "X", -1))
	$g_iY = Int(IniRead($INI, "Fenster", "Y", -1))
	$g_iW = Int(IniRead($INI, "Fenster", "W", 560))
	$g_iH = Int(IniRead($INI, "Fenster", "H", 0))
	$g_sToken = _DpapiUnprotect(IniRead($INI, "Konto", "Token", ""))
	$g_sUser = IniRead($INI, "Konto", "Benutzer", "")
	$g_sEmail = IniRead($INI, "Konto", "EMail", "")
	$g_sRegion = IniRead($INI, "Konto", "Region", "Global")
	If $g_sRegion <> "China" Then $g_sRegion = "Global"
	$g_sHaUrlIn = IniRead($INI, "HomeAssistant", "AdresseHeimnetz", "")
	$g_sHaUrlOut = IniRead($INI, "HomeAssistant", "AdresseUnterwegs", "")
	$g_sHaToken = _DpapiUnprotect(IniRead($INI, "HomeAssistant", "Token", ""))
	$g_bHaInsecure = (IniRead($INI, "HomeAssistant", "ZertifikatIgnorieren", "0") = "1")
	$g_sFfmpeg = IniRead($INI, "Video", "Ffmpeg", "")
	__PrnLoadAll()
EndFunc   ;==>_SettingsLoad

; =============================================================================
;  Druckerprofile
; =============================================================================
; neue Zeile mit Standardwerten anhaengen
Func __PrnAdd(ByRef $a, ByRef $n, $sName)
	$n += 1
	ReDim $a[$n + 1][$PF_COUNT]
	For $c = 0 To $PF_COUNT - 1
		$a[$n][$c] = ""
	Next
	$a[$n][$PF_NAME] = $sName
	$a[$n][$PF_CAMT] = 0
	$a[$n][$PF_IMG] = 0
	$a[$n][$PF_CONN] = 0
	$a[$n][$PF_CAMON] = 1
	$a[$n][$PF_STON] = 1
	$a[$n][$PF_COVER] = 1
EndFunc   ;==>__PrnAdd

Func __PrnLoadAll()
	Local $n = Int(IniRead($INI, "Allgemein", "Anzahl", 0))
	; bis v1.3 galten Kamera/Status an-aus fuer alle Drucker -> Vorgabe fuer jedes Profil
	Local $iCamDef = (IniRead($INI, "Anzeige", "Kamera", "1") = "0" ? 0 : 1)
	Local $iStDef = (IniRead($INI, "Anzeige", "Status", "1") = "0" ? 0 : 1)
	ReDim $g_aPrn[1][$PF_COUNT]
	$g_iPrnN = 0
	If $n = 0 Then
		; Uebernahme der Einstellungen aus v1.0 / v1.1 (ein Drucker)
		Local $sIp = IniRead($INI, "Drucker", "IP", ""), $sSerial = IniRead($INI, "Drucker", "Seriennummer", "")
		Local $sCode = _DpapiUnprotect(IniRead($INI, "Drucker", "AccessCode", ""))
		If $sIp <> "" Or $sSerial <> "" Or $sCode <> "" Then
			Local $sName = IniRead($INI, "Konto", "Drucker", "")
			__PrnAdd($g_aPrn, $g_iPrnN, ($sName <> "" ? $sName : "Mein Drucker"))
			$g_aPrn[1][$PF_IP] = $sIp
			$g_aPrn[1][$PF_CODE] = $sCode
			$g_aPrn[1][$PF_SERIAL] = $sSerial
			$g_aPrn[1][$PF_CONN] = Int(IniRead($INI, "Verbindung", "Modus", 0))
			$g_aPrn[1][$PF_CAMON] = $iCamDef
			$g_aPrn[1][$PF_STON] = $iStDef
			$g_iPrn = 1
			__PrnSaveAll()
			IniDelete($INI, "Drucker")
			IniDelete($INI, "Verbindung")
			IniDelete($INI, "Konto", "Drucker")
		EndIf
	Else
		For $i = 1 To $n
			Local $sSec = "Drucker_" & $i
			__PrnAdd($g_aPrn, $g_iPrnN, IniRead($INI, $sSec, "Name", "Drucker " & $i))
			$g_aPrn[$i][$PF_IP] = IniRead($INI, $sSec, "IP", "")
			$g_aPrn[$i][$PF_CODE] = _DpapiUnprotect(IniRead($INI, $sSec, "AccessCode", ""))
			$g_aPrn[$i][$PF_SERIAL] = IniRead($INI, $sSec, "Seriennummer", "")
			$g_aPrn[$i][$PF_MODEL] = IniRead($INI, $sSec, "Modell", "")
			$g_aPrn[$i][$PF_CAMT] = __Clamp(Int(IniRead($INI, $sSec, "Kameratyp", 0)), 0, 2)
			$g_aPrn[$i][$PF_IMG] = __Clamp(Int(IniRead($INI, $sSec, "Bildquelle", 0)), 0, 2)
			$g_aPrn[$i][$PF_HACAM] = IniRead($INI, $sSec, "HAKamera", "")
			$g_aPrn[$i][$PF_CONN] = __Clamp(Int(IniRead($INI, $sSec, "StatusQuelle", 0)), 0, 3)
			$g_aPrn[$i][$PF_CAMON] = (IniRead($INI, $sSec, "KameraAn", $iCamDef) = "0" ? 0 : 1)
			$g_aPrn[$i][$PF_STON] = (IniRead($INI, $sSec, "StatusAn", $iStDef) = "0" ? 0 : 1)
			$g_aPrn[$i][$PF_COVER] = (IniRead($INI, $sSec, "Druckobjekt", "1") = "0" ? 0 : 1)
			$g_aPrn[$i][$PF_HACOVER] = IniRead($INI, $sSec, "HATitelbild", "")
			$g_aPrn[$i][$PF_HALIGHT] = IniRead($INI, $sSec, "HALicht", "")
		Next
		$g_iPrn = Int(IniRead($INI, "Allgemein", "Aktiv", 1))
	EndIf
	If $g_iPrnN = 0 Then __PrnAdd($g_aPrn, $g_iPrnN, "Mein Drucker")
	__PrnActivate($g_iPrn)
EndFunc   ;==>__PrnLoadAll

Func __PrnSaveAll()
	IniWrite($INI, "Allgemein", "Anzahl", $g_iPrnN)
	IniWrite($INI, "Allgemein", "Aktiv", $g_iPrn)
	For $i = 1 To $g_iPrnN
		Local $sSec = "Drucker_" & $i
		IniWrite($INI, $sSec, "Name", $g_aPrn[$i][$PF_NAME])
		IniWrite($INI, $sSec, "IP", $g_aPrn[$i][$PF_IP])
		IniWrite($INI, $sSec, "AccessCode", _DpapiProtect($g_aPrn[$i][$PF_CODE]))
		IniWrite($INI, $sSec, "Seriennummer", $g_aPrn[$i][$PF_SERIAL])
		IniWrite($INI, $sSec, "Modell", $g_aPrn[$i][$PF_MODEL])
		IniWrite($INI, $sSec, "Kameratyp", $g_aPrn[$i][$PF_CAMT])
		IniWrite($INI, $sSec, "Bildquelle", $g_aPrn[$i][$PF_IMG])
		IniWrite($INI, $sSec, "HAKamera", $g_aPrn[$i][$PF_HACAM])
		IniWrite($INI, $sSec, "StatusQuelle", $g_aPrn[$i][$PF_CONN])
		IniWrite($INI, $sSec, "KameraAn", $g_aPrn[$i][$PF_CAMON])
		IniWrite($INI, $sSec, "StatusAn", $g_aPrn[$i][$PF_STON])
		IniWrite($INI, $sSec, "Druckobjekt", $g_aPrn[$i][$PF_COVER])
		IniWrite($INI, $sSec, "HATitelbild", $g_aPrn[$i][$PF_HACOVER])
		IniWrite($INI, $sSec, "HALicht", $g_aPrn[$i][$PF_HALIGHT])
	Next
	; alte, fuer alle Drucker geltende Schalter (bis v1.3) sind jetzt pro Drucker gespeichert
	IniDelete($INI, "Anzeige", "Kamera")
	IniDelete($INI, "Anzeige", "Status")
	For $i = $g_iPrnN + 1 To $g_iPrnN + 20 ; geloeschte Drucker entfernen
		IniDelete($INI, "Drucker_" & $i)
	Next
EndFunc   ;==>__PrnSaveAll

; Werte eines Profils in die "aktiven" Variablen uebernehmen
Func __PrnActivate($i)
	If $i < 1 Or $i > $g_iPrnN Then $i = 1
	$g_iPrn = $i
	$g_sPrinterName = $g_aPrn[$i][$PF_NAME]
	$g_sIp = $g_aPrn[$i][$PF_IP]
	$g_sCode = $g_aPrn[$i][$PF_CODE]
	$g_sSerial = $g_aPrn[$i][$PF_SERIAL]
	$g_sModel = $g_aPrn[$i][$PF_MODEL]
	$g_iCamType = Int($g_aPrn[$i][$PF_CAMT])
	$g_iImgSrc = Int($g_aPrn[$i][$PF_IMG])
	$g_sHaCam = $g_aPrn[$i][$PF_HACAM]
	$g_iConnMode = Int($g_aPrn[$i][$PF_CONN])
	$g_bCamOn = (Int($g_aPrn[$i][$PF_CAMON]) = 1)
	$g_bStatusOn = (Int($g_aPrn[$i][$PF_STON]) = 1)
	$g_bCoverOn = (Int($g_aPrn[$i][$PF_COVER]) = 1)
	$g_sHaCover = $g_aPrn[$i][$PF_HACOVER]
	$g_sHaLight = $g_aPrn[$i][$PF_HALIGHT]
EndFunc   ;==>__PrnActivate

; aktive Werte zurueck ins Profil (z. B. nach IP-Uebernahme aus der Cloud)
Func __PrnStoreActive()
	$g_aPrn[$g_iPrn][$PF_IP] = $g_sIp
	$g_aPrn[$g_iPrn][$PF_CODE] = $g_sCode
	$g_aPrn[$g_iPrn][$PF_SERIAL] = $g_sSerial
	$g_aPrn[$g_iPrn][$PF_HACAM] = $g_sHaCam
	$g_aPrn[$g_iPrn][$PF_HACOVER] = $g_sHaCover
	$g_aPrn[$g_iPrn][$PF_HALIGHT] = $g_sHaLight
EndFunc   ;==>__PrnStoreActive

Func __Clamp($v, $iMin, $iMax)
	If $v < $iMin Then Return $iMin
	If $v > $iMax Then Return $iMax
	Return $v
EndFunc   ;==>__Clamp

; Schriftgroesse in Punkt plus Einstellung "Schrift groesser" (1 px = 0,75 pt bei 96 dpi)
Func __Pt($fSize)
	Return $fSize + $g_iFontAdd * 0.75
EndFunc   ;==>__Pt

; Druckername oben in der Leiste
Func __TitleLabel()
	GUICtrlSetData($g_idTitle, $g_sPrinterName & "  ▾")
EndFunc   ;==>__TitleLabel

; Druckermenue unter dem Namen aufklappen
Func _PrnMenuShow()
	For $i = 1 To $g_aPrnMenuIds[0]
		GUICtrlDelete($g_aPrnMenuIds[$i])
	Next
	Local $aIds[1] = [0], $mMap[]
	$g_aPrnMenuIds = $aIds
	$g_mPrnMenu = $mMap
	Local $id
	For $i = 1 To $g_iPrnN
		$id = GUICtrlCreateMenuItem($g_aPrn[$i][$PF_NAME] & ($g_aPrn[$i][$PF_MODEL] <> "" ? "  (" & $g_aPrn[$i][$PF_MODEL] & ")" : ""), $g_idPrnCtx, -1, 1)
		If $i = $g_iPrn Then GUICtrlSetState($id, $GUI_CHECKED)
		GUICtrlSetOnEvent($id, "_PrnMenuPick")
		$g_mPrnMenu[$id] = $i
		__Push1($g_aPrnMenuIds, $id)
	Next
	__Push1($g_aPrnMenuIds, GUICtrlCreateMenuItem("", $g_idPrnCtx))
	$id = GUICtrlCreateMenuItem("Drucker verwalten …", $g_idPrnCtx)
	GUICtrlSetOnEvent($id, "_PrnManage")
	__Push1($g_aPrnMenuIds, $id)
	Local $p = WinGetPos($g_hGui), $c = ControlGetPos($g_hGui, "", $g_idTitle)
	If Not IsArray($p) Or Not IsArray($c) Then Return
	DllCall("user32.dll", "int", "TrackPopupMenu", "handle", GUICtrlGetHandle($g_idPrnCtx), "uint", 0, _
			"int", $p[0] + $c[0], "int", $p[1] + $c[1] + $c[3], "int", 0, "hwnd", $g_hGui, "ptr", 0)
EndFunc   ;==>_PrnMenuShow

Func _PrnMenuPick()
	If MapExists($g_mPrnMenu, @GUI_CtrlId) Then _PrnSwitch($g_mPrnMenu[@GUI_CtrlId])
EndFunc   ;==>_PrnMenuPick

Func _PrnManage()
	_SettingsShow()
	If $g_hSet <> 0 Then
		$g_iSetTab = 0
		__SetTabApply()
	EndIf
EndFunc   ;==>_PrnManage

Func _PrnSwitch($i)
	If $i = $g_iPrn Or $i < 1 Or $i > $g_iPrnN Then Return
	__PrnActivate($i)
	IniWrite($INI, "Allgemein", "Aktiv", $g_iPrn)
	__TitleLabel()
	If $g_hFrame Then _GDIPlus_ImageDispose($g_hFrame)
	$g_hFrame = 0
	$g_bFrame = Binary("")
	_HelpersStart()
	__TitleUpdate()
	__ToastShow($g_sPrinterName)
EndFunc   ;==>_PrnSwitch

Func __Push1(ByRef $a, $v)
	$a[0] += 1
	ReDim $a[$a[0] + 1]
	$a[$a[0]] = $v
EndFunc   ;==>__Push1

Func _SettingsShow()
	If $g_hSet <> 0 Then
		WinActivate($g_hSet)
		Return
	EndIf
	If Not BitAND(WinGetState($g_hGui), $WIN_STATE_VISIBLE) Then _TrayShow()
	; Arbeitskopie: Aenderungen gelten erst mit "Speichern"
	$g_aSetPrn = $g_aPrn
	$g_iSetPrnN = $g_iPrnN
	$g_iSetPrnSel = $g_iPrn
	$g_iSetActive = $g_iPrn
	Local Const $W = 480
	$g_hSet = GUICreate("Einstellungen – BambuWatch", $W, 600, -1, -1, $WS_POPUP, BitOR($WS_EX_TOOLWINDOW, ($g_bOnTop ? $WS_EX_TOPMOST : 0)), $g_hGui)
	GUISetBkColor($C_BG, $g_hSet)
	GUISetFont(__Pt(9), 400, 0, "Segoe UI", $g_hSet)
	GUISetOnEvent($GUI_EVENT_CLOSE, "_SettingsClose", $g_hSet)
	GUISetOnEvent($GUI_EVENT_PRIMARYDOWN, "_DragAny", $g_hSet)
	__WinStyleModern($g_hSet)

	Local $y = __DlgHead("Einstellungen", $W, "_SettingsClose")
	Local $aTabNames[3] = ["Drucker", "Konten & Dienste", "Anzeige & Fenster"], $aTabX[3] = [20, 114, 268], $aTabW[3] = [90, 150, 150]
	For $i = 0 To 2
		$g_aSetTab[$i] = __Lbl($aTabNames[$i], $aTabX[$i], $y + 4, $aTabW[$i], 26, BitOR($SS_CENTER, $SS_CENTERIMAGE), $C_TXT2, 9, 600)
		GUICtrlSetOnEvent($g_aSetTab[$i], "_SetTabClick")
		GUICtrlSetCursor($g_aSetTab[$i], 0)
	Next
	$g_iSetTabLineY = $y + 30
	$g_idSetTabLine = GUICtrlCreateLabel("", 20, $g_iSetTabLineY, 90, 2)
	GUICtrlSetBkColor($g_idSetTabLine, $C_GREEN)
	Local $y0 = $y + 34

	; ======== Seite 1: Drucker (IDs zwischen den Dummies = Seite)
	$y = $y0
	$g_aSetPage[0][0] = GUICtrlCreateDummy()
	$g_idSetPrnSel = __CycleLbl("", 20, $y + 2, "_SetPrnNext", 262)
	GUICtrlSetTip($g_idSetPrnSel, "Klick: nächster Drucker")
	_PillBtn("+ Neu", 290, $y, 76, 26, $BTN_DARK, "_SetPrnNew", False, $C_BG)
	_PillBtn("Entfernen", 372, $y, 88, 26, $BTN_DARK, "_SetPrnDel", False, $C_BG)
	$y += 34
	$g_idSetName = __Field("Name", "", 20, $y, 230)
	__DragLbl(__Lbl("Druckstatus über", 262, $y, 198, 16, 0, $C_TXT2, 8, 400))
	$g_idSetConn = __CycleLbl("", 262, $y + 19, "_SetConnCycle", 198)
	$y += 52
	$g_idTogCam = __Toggle("Kamerabild anzeigen", True, 20, $y, 220)
	$g_idTogSt = __Toggle("Druckstatus abrufen", True, 262, $y, 198)
	$y += 24
	$y = __DlgSection("Heimnetz", $y)
	$g_idSetIp = __Field("IP-Adresse", "", 20, $y, 190)
	$g_idSetSerial = __Field("Seriennummer", "", 220, $y, $W - 240)
	$y += 52
	$g_idSetCode = __Field("Access Code", "", 20, $y, 190, $ES_PASSWORD)
	$g_idSetEye = __Lbl(ChrW(0xE890), 214, $y + 18, 26, 24, BitOR($SS_CENTER, $SS_CENTERIMAGE), $C_TXT2, 11, 400, $GLYPH_FONT) ; "View" (Auge)
	GUICtrlSetOnEvent($g_idSetEye, "_SetCodeEye")
	GUICtrlSetCursor($g_idSetEye, 0)
	GUICtrlSetTip($g_idSetEye, "Access Code anzeigen / verbergen")
	$g_bCodeVisible = False
	$g_idSetProbeBtn = _PillBtn("Verbindung testen", $W - 160, $y + 17, 140, 26, $BTN_DARK, "_ProbeStart", False, $C_BG)
	$y += 50
	$g_idSetProbeCam = __Lbl("", 20, $y, $W - 40, 18, $SS_LEFTNOWORDWRAP, $C_TXT2, 8.5, 400)
	$g_idSetProbeMq = __Lbl("", 20, $y + 18, $W - 40, 18, $SS_LEFTNOWORDWRAP, $C_TXT2, 8.5, 400)
	$y += 38
	$y = __DlgSection("Kamerabild", $y)
	__DragLbl(__Lbl("Bildquelle", 20, $y, 90, 22, $SS_CENTERIMAGE, $C_TXT, 9, 400))
	$g_idSetImg = __CycleLbl("", 110, $y, "_SetImgCycle", 220)
	$y += 28
	__DragLbl(__Lbl("Kameratyp", 20, $y, 90, 22, $SS_CENTERIMAGE, $C_TXT, 9, 400))
	$g_idSetCamT = __CycleLbl("", 110, $y, "_SetCamTCycle", 220)
	$y += 28
	__DragLbl(__Lbl("HA-Kamera", 20, $y, 90, 22, $SS_CENTERIMAGE, $C_TXT, 9, 400))
	$g_idSetHaCam = __CycleLbl("", 110, $y, "_SetHaCamCycle", 220)
	_PillBtn("Aus HA laden", $W - 136, $y - 2, 116, 26, $BTN_DARK, "_SetHaLoad", False, $C_BG)
	$y += 30
	$y = __DlgSection("Druckobjekt statt Kamerabild", $y)
	$g_idTogCover = __Toggle("Bild des Druckobjekts zeigen, wenn kein Kamerabild kommt", True, 20, $y, $W - 40)
	$y += 26
	__DragLbl(__Lbl("HA-Titelbild", 20, $y, 90, 22, $SS_CENTERIMAGE, $C_TXT, 9, 400))
	$g_idSetHaCov = __CycleLbl("", 110, $y, "_SetHaCovCycle", 220)
	$y += 28
	$y = __DlgHint("Druckobjekt: „Titelbild“ aus Home Assistant (ha-bambulab), sonst letzter Cloud-Auftrag. „Automatisch“ heißt: im Heimnetz direkt vom Drucker, sonst über Home Assistant, der Status zuletzt über die Bambu-Cloud. HA-Kamera, Titelbild und Licht findet BambuWatch über die Seriennummer selbst.", 20, $y, $W - 40)
	$g_aSetPage[0][1] = GUICtrlCreateDummy()
	Local $y1 = $y

	; ======== Seite 2: Konten & Dienste
	$y = $y0
	$g_aSetPage[1][0] = GUICtrlCreateDummy()
	$y = __DlgSection("Bambu-Konto", $y)
	$g_idSetAcc = __Lbl("", 20, $y, $W - 40, 34, 0, $C_TXT, 9, 400)
	$y += 40
	$g_idSetLogin = _PillBtn("Anmelden …", 20, $y, 116, 26, $BTN_GREEN, "_AccountShow", False, $C_BG)
	$g_idSetLogout = _PillBtn("Abmelden", 142, $y, 96, 26, $BTN_DARK, "_AccountLogout", False, $C_BG)
	__DragLbl(__Lbl("Region", 266, $y, 54, 26, $SS_CENTERIMAGE, $C_TXT2, 8.5, 400))
	$g_iSetRegion = ($g_sRegion = "China" ? 1 : 0)
	$g_idSetRegion = __CycleLbl($REGION_NAMES[$g_iSetRegion], 320, $y + 2, "_SetRegionCycle", 140)
	$y += 32
	$y = __DlgHint("Beim Anmelden werden alle Drucker des Kontos übernommen – mit Seriennummer und Access Code. Status kommt dann auch unterwegs.", 20, $y, $W - 40)
	$y = __DlgSection("Home Assistant (Bild und Status, auch unterwegs)", $y)
	$g_idSetHaIn = __Field("Adresse im Heimnetz", $g_sHaUrlIn, 20, $y, 214)
	$g_idSetHaOut = __Field("Adresse von unterwegs", $g_sHaUrlOut, 246, $y, $W - 266)
	$y += 52
	$g_idSetHaTok = __Field("Langlebiges Zugriffstoken (HA-Profil › Sicherheit)", $g_sHaToken, 20, $y, 300, $ES_PASSWORD)
	$y += 50
	$g_idTogHaIns = __Toggle("Zertifikat nicht prüfen (selbstsigniert)", $g_bHaInsecure, 20, $y, 280)
	_PillBtn("Verbindung testen", $W - 160, $y - 2, 140, 26, $BTN_DARK, "_SetHaLoad", False, $C_BG)
	$y += 28
	$g_idSetHaRes = __Lbl("z. B. http://192.168.1.10:8123 und https://ha.deine-domain.de – eine Adresse reicht.", 20, $y, $W - 40, 18, $SS_LEFTNOWORDWRAP, $C_TXT2, 8.5, 400)
	$y += 22
	$y = __DlgSection("Videostream (H2D, X1, P2S)", $y)
	$g_idSetFf = __Input($g_sFfmpeg, 20, $y, $W - 80)
	Local $idFfB = _PillBtn("…", $W - 54, $y, 34, 24, $BTN_DARK, "_SetFfBrowse", False, $C_BG)
	GUICtrlSetTip($idFfB, "ffmpeg.exe wählen")
	$y += 28
	$g_idSetFfRes = __Lbl("", 20, $y, $W - 40, 18, $SS_LEFTNOWORDWRAP, $C_TXT2, 8.5, 400)
	$y += 20
	$y = __DlgHint("ffmpeg.exe (kostenlos, z. B. von ffmpeg.org) macht aus dem Videostream Einzelbilder. Liegt sie neben BambuWatch oder im PATH, wird sie automatisch gefunden.", 20, $y, $W - 40)
	$g_aSetPage[1][1] = GUICtrlCreateDummy()
	Local $y2 = $y

	; ======== Seite 3: Anzeige & Fenster
	$y = $y0
	$g_aSetPage[2][0] = GUICtrlCreateDummy()
	$y = __DlgSection("Anzeige", $y)
	__DragLbl(__Lbl("Status-Overlay", 20, $y, 120, 22, $SS_CENTERIMAGE, $C_TXT, 9, 400))
	$g_iSetOvl = $g_iOverlay
	$g_idSetOvl = __CycleLbl($OVL_NAMES[$g_iSetOvl], 140, $y, "_SetOvlCycle")
	__DragLbl(__Lbl("Deckkraft", 240, $y, 80, 22, $SS_CENTERIMAGE, $C_TXT, 9, 400))
	$g_iSetAlpha = 0
	For $i = 0 To 3
		If $ALPHA_STEPS[$i] = $g_iAlpha Then $g_iSetAlpha = $i
	Next
	$g_idSetAlpha = __CycleLbl($ALPHA_NAMES[$g_iSetAlpha], 320, $y, "_SetAlphaCycle")
	$y += 28
	$g_idTogAspect = __Toggle("Seitenverhältnis beim Ziehen halten", $g_bAspect, 20, $y, $W - 40)
	$y += 26
	__DragLbl(__Lbl("Schriftgröße", 20, $y, 120, 22, $SS_CENTERIMAGE, $C_TXT, 9, 400))
	$g_iSetFont = $g_iFontAdd
	$g_idSetFont = __CycleLbl($FONT_NAMES[$g_iSetFont], 140, $y, "_SetFontCycle")
	__DragLbl(__Lbl("Leiste ab dem nächsten Start", 240, $y, 220, 22, $SS_CENTERIMAGE, $C_TXT2, 8, 400))
	$y += 28
	$y = __DlgSection("Fenster", $y)
	$g_idTogTop = __Toggle("Immer im Vordergrund", $g_bOnTop, 20, $y, 210)
	$g_idTogTray = __Toggle("Minimieren in den Infobereich", $g_bTray, 240, $y, 220)
	$y += 26
	$g_idTogStudio = __Toggle("Pausieren, solange Bambu Studio läuft", $g_bStudioWatch, 20, $y, $W - 40)
	$y += 26
	$y = __DlgSection("Meldungen (aktiver Drucker)", $y)
	$g_idTogNotify = __Toggle("Hinweis bei Druckende, Pause, Abbruch", $g_bNotify, 20, $y, 280)
	$g_idTogSound = __Toggle("mit Ton", $g_bSound, 320, $y, 120)
	$y += 26
	$y = __DlgSection("Snapshots", $y)
	$g_idSetSnap = __Input($g_sSnapDir, 20, $y, $W - 80)
	Local $idBrowse = _PillBtn("…", $W - 54, $y, 34, 24, $BTN_DARK, "_SetSnapBrowse", False, $C_BG)
	GUICtrlSetTip($idBrowse, "Ordner wählen")
	$y += 34
	$g_aSetPage[2][1] = GUICtrlCreateDummy()

	$y = ($y > $y1 ? $y : $y1)
	$y = ($y > $y2 ? $y : $y2) + 14
	_PillBtn("Speichern", $W - 230, $y, 100, 30, $BTN_GREEN, "_SettingsSave", False, $C_BG)
	_PillBtn("Abbrechen", $W - 120, $y, 100, 30, $BTN_DARK, "_SettingsClose", False, $C_BG)
	$y += 48

	WinMove($g_hSet, "", 0, 0, $W, $y)
	__DlgCenter($g_hSet)
	; HA-Kameraliste: bekannte Werte aus den Profilen vorbelegen (ohne Abfrage)
	ReDim $g_aSetHaCams[1][2]
	$g_iSetHaCamN = 0
	ReDim $g_aSetHaImgs[1][2]
	$g_iSetHaImgN = 0
	__SetPrnLoad()
	__SetFfShow()
	$g_iSetTab = 0
	__SetTabApply()
	GUISetState(@SW_SHOW, $g_hSet)
	GUISwitch($g_hGui)
EndFunc   ;==>_SettingsShow

Func _SetTabClick()
	For $i = 0 To 2
		If @GUI_CtrlId = $g_aSetTab[$i] Then $g_iSetTab = $i
	Next
	__SetTabApply()
EndFunc   ;==>_SetTabClick

Func __SetTabApply()
	__TabApply($g_hSet, $g_aSetTab, $g_aSetPage, $g_iSetTab, $g_idSetTabLine, $g_iSetTabLineY)
	__AccountLabelUpdate() ; blendet "Abmelden" passend ein/aus
EndFunc   ;==>__SetTabApply

; ---------------------------------------------------------------- Drucker-Reiter
; Felder -> Arbeitskopie (mit Pruefung); False bei ungueltigen Eingaben
Func __SetPrnStore()
	Local $k = $g_iSetPrnSel
	Local $sIp = StringStripWS(GUICtrlRead($g_idSetIp), 8)
	Local $sSerial = StringUpper(StringStripWS(GUICtrlRead($g_idSetSerial), 8))
	If $sIp <> "" And Not StringRegExp($sIp, "^[A-Za-z0-9.\-]+$") Then
		__SetTabGo(0)
		__ProbeShow("err:IP-Adresse enthält ungültige Zeichen", "")
		Return False
	EndIf
	If $sSerial <> "" And Not StringRegExp($sSerial, "^[A-Z0-9]+$") Then
		__SetTabGo(0)
		__ProbeShow("", "err:Seriennummer: nur Buchstaben und Ziffern")
		Return False
	EndIf
	Local $sName = StringStripWS(GUICtrlRead($g_idSetName), 3)
	$g_aSetPrn[$k][$PF_NAME] = ($sName <> "" ? $sName : "Drucker " & $k)
	$g_aSetPrn[$k][$PF_IP] = $sIp
	$g_aSetPrn[$k][$PF_CODE] = StringStripWS(GUICtrlRead($g_idSetCode), 8)
	$g_aSetPrn[$k][$PF_SERIAL] = $sSerial
	$g_aSetPrn[$k][$PF_CONN] = $g_iSetConn
	$g_aSetPrn[$k][$PF_IMG] = $g_iSetImg
	$g_aSetPrn[$k][$PF_CAMT] = $g_iSetCamT
	$g_aSetPrn[$k][$PF_HACAM] = $g_sSetHaCam
	$g_aSetPrn[$k][$PF_CAMON] = (__TogGet($g_idTogCam) ? 1 : 0)
	$g_aSetPrn[$k][$PF_STON] = (__TogGet($g_idTogSt) ? 1 : 0)
	$g_aSetPrn[$k][$PF_COVER] = (__TogGet($g_idTogCover) ? 1 : 0)
	$g_aSetPrn[$k][$PF_HACOVER] = $g_sSetHaCov
	Return True
EndFunc   ;==>__SetPrnStore

; Arbeitskopie -> Felder
Func __SetPrnLoad()
	Local $k = $g_iSetPrnSel
	GUICtrlSetData($g_idSetPrnSel, $g_aSetPrn[$k][$PF_NAME] & ($g_aSetPrn[$k][$PF_MODEL] <> "" ? " · " & $g_aSetPrn[$k][$PF_MODEL] : "") & _
			"   (" & $k & "/" & $g_iSetPrnN & ($k = $g_iSetActive ? ", aktiv" : "") & ")  ›")
	GUICtrlSetData($g_idSetName, $g_aSetPrn[$k][$PF_NAME])
	GUICtrlSetData($g_idSetIp, $g_aSetPrn[$k][$PF_IP])
	GUICtrlSetData($g_idSetSerial, $g_aSetPrn[$k][$PF_SERIAL])
	GUICtrlSetData($g_idSetCode, $g_aSetPrn[$k][$PF_CODE])
	$g_iSetConn = Int($g_aSetPrn[$k][$PF_CONN])
	GUICtrlSetData($g_idSetConn, $CONN_NAMES[$g_iSetConn] & "  ›")
	$g_iSetImg = Int($g_aSetPrn[$k][$PF_IMG])
	GUICtrlSetData($g_idSetImg, $IMGSRC_NAMES[$g_iSetImg] & "  ›")
	$g_iSetCamT = Int($g_aSetPrn[$k][$PF_CAMT])
	GUICtrlSetData($g_idSetCamT, $CAMTYPE_NAMES[$g_iSetCamT] & "  ›")
	$g_sSetHaCam = $g_aSetPrn[$k][$PF_HACAM]
	__SetHaCamShow()
	__TogSet($g_idTogCam, Int($g_aSetPrn[$k][$PF_CAMON]) = 1)
	__TogSet($g_idTogSt, Int($g_aSetPrn[$k][$PF_STON]) = 1)
	__TogSet($g_idTogCover, Int($g_aSetPrn[$k][$PF_COVER]) = 1)
	$g_sSetHaCov = $g_aSetPrn[$k][$PF_HACOVER]
	__SetHaCovShow()
	__ProbeShow("", "")
EndFunc   ;==>__SetPrnLoad

Func __SetTabGo($i)
	If $g_iSetTab = $i Then Return
	$g_iSetTab = $i
	__SetTabApply()
EndFunc   ;==>__SetTabGo

Func _SetPrnNext()
	If Not __SetPrnStore() Then Return
	$g_iSetPrnSel = Mod($g_iSetPrnSel, $g_iSetPrnN) + 1
	__SetPrnLoad()
EndFunc   ;==>_SetPrnNext

Func _SetPrnNew()
	If Not __SetPrnStore() Then Return
	__PrnAdd($g_aSetPrn, $g_iSetPrnN, "Drucker " & ($g_iSetPrnN + 1))
	$g_iSetPrnSel = $g_iSetPrnN
	__SetPrnLoad()
	GUICtrlSetState($g_idSetName, $GUI_FOCUS)
EndFunc   ;==>_SetPrnNew

Func _SetPrnDel()
	If $g_iSetPrnN <= 1 Then
		__ProbeShow("err:Der letzte Drucker kann nicht entfernt werden", "")
		Return
	EndIf
	Local $k = $g_iSetPrnSel
	For $r = $k To $g_iSetPrnN - 1
		For $c = 0 To $PF_COUNT - 1
			$g_aSetPrn[$r][$c] = $g_aSetPrn[$r + 1][$c]
		Next
	Next
	$g_iSetPrnN -= 1
	ReDim $g_aSetPrn[$g_iSetPrnN + 1][$PF_COUNT]
	; aktiven Drucker nachfuehren
	If $g_iSetActive = $k Then
		$g_iSetActive = 1
	ElseIf $g_iSetActive > $k Then
		$g_iSetActive -= 1
	EndIf
	If $g_iSetPrnSel > $g_iSetPrnN Then $g_iSetPrnSel = $g_iSetPrnN
	__SetPrnLoad()
EndFunc   ;==>_SetPrnDel

Func _SetImgCycle()
	$g_iSetImg = Mod($g_iSetImg + 1, 3)
	GUICtrlSetData($g_idSetImg, $IMGSRC_NAMES[$g_iSetImg] & "  ›")
EndFunc   ;==>_SetImgCycle

Func _SetCamTCycle()
	$g_iSetCamT = Mod($g_iSetCamT + 1, 3)
	GUICtrlSetData($g_idSetCamT, $CAMTYPE_NAMES[$g_iSetCamT] & "  ›")
EndFunc   ;==>_SetCamTCycle

; HA-Kamera: Anzeige (Klarname, wenn aus der Liste bekannt)
Func __SetHaCamShow()
	Local $sTxt = ($g_sSetHaCam = "" ? "– keine –" : $g_sSetHaCam)
	For $i = 1 To $g_iSetHaCamN
		If $g_aSetHaCams[$i][0] = $g_sSetHaCam Then $sTxt = $g_aSetHaCams[$i][1] & " (" & $g_sSetHaCam & ")"
	Next
	GUICtrlSetData($g_idSetHaCam, $sTxt & "  ›")
EndFunc   ;==>__SetHaCamShow

Func _SetHaCamCycle()
	If $g_iSetHaCamN = 0 Then
		GUICtrlSetData($g_idSetHaCam, "erst „Aus HA laden“  ›")
		Return
	EndIf
	; naechster Eintrag; Index 0 = "– keine –"
	Local $iCur = 0
	For $i = 1 To $g_iSetHaCamN
		If $g_aSetHaCams[$i][0] = $g_sSetHaCam Then $iCur = $i
	Next
	$iCur = Mod($iCur + 1, $g_iSetHaCamN + 1)
	$g_sSetHaCam = $g_aSetHaCams[$iCur][0]
	__SetHaCamShow()
EndFunc   ;==>_SetHaCamCycle

; HA-Titelbild (Bild-Entitaet): Anzeige und Auswahl wie bei der HA-Kamera
Func __SetHaCovShow()
	Local $sTxt = ($g_sSetHaCov = "" ? "– keines –" : $g_sSetHaCov)
	For $i = 1 To $g_iSetHaImgN
		If $g_aSetHaImgs[$i][0] = $g_sSetHaCov Then $sTxt = $g_aSetHaImgs[$i][1] & " (" & $g_sSetHaCov & ")"
	Next
	GUICtrlSetData($g_idSetHaCov, $sTxt & "  ›")
EndFunc   ;==>__SetHaCovShow

Func _SetHaCovCycle()
	If $g_iSetHaImgN = 0 Then
		GUICtrlSetData($g_idSetHaCov, "erst „Aus HA laden“  ›")
		Return
	EndIf
	Local $iCur = 0
	For $i = 1 To $g_iSetHaImgN
		If $g_aSetHaImgs[$i][0] = $g_sSetHaCov Then $iCur = $i
	Next
	$iCur = Mod($iCur + 1, $g_iSetHaImgN + 1)
	$g_sSetHaCov = $g_aSetHaImgs[$iCur][0]
	__SetHaCovShow()
EndFunc   ;==>_SetHaCovCycle

; Vorschlag fuers Titelbild: ha-bambulab nennt es ..._titelbild (deutsch) bzw. ..._cover_image;
; bevorzugt das Geraet der gewaehlten HA-Kamera (gleicher Namensanfang)
Func __HaCoverGuess()
	Local $sPre = ""
	If $g_sSetHaCam <> "" Then $sPre = StringRegExpReplace(StringRegExpReplace($g_sSetHaCam, "^[a-z_]+\.", ""), "_[^_]+$", "")
	Local $sFirst = "", $sId
	For $i = 1 To $g_iSetHaImgN
		$sId = $g_aSetHaImgs[$i][0]
		If Not StringRegExp($sId, "_(titelbild|cover_image)$") Then ContinueLoop
		If $sFirst = "" Then $sFirst = $sId
		If $sPre <> "" And StringLeft(StringTrimLeft($sId, 6), StringLen($sPre)) = $sPre Then Return $sId
	Next
	Return $sFirst
EndFunc   ;==>__HaCoverGuess

; ---------------------------------------------------------------- Home Assistant: Kameraliste / Test
Func _SetHaLoad()
	If $g_iPidHaList Then Return
	Local $sIn = StringStripWS(GUICtrlRead($g_idSetHaIn), 3), $sOut = StringStripWS(GUICtrlRead($g_idSetHaOut), 3)
	Local $sTok = StringStripWS(GUICtrlRead($g_idSetHaTok), 3)
	If ($sIn = "" And $sOut = "") Or $sTok = "" Then
		__SetHaMsg("Adresse und Token von Home Assistant eintragen (Reiter „Konten & Dienste“)", $C_RED)
		Return
	EndIf
	$g_sBufHaList = ""
	$g_iPidHaList = __RunHelperEnv("-Mode HaList" & (__TogGet($g_idTogHaIns) ? " -HaInsecure" : ""), "BW_HATOKEN", $sTok, "BW_HAURLS", $sIn & "|" & $sOut)
	If Not $g_iPidHaList Then
		__SetHaMsg("PowerShell konnte nicht gestartet werden", $C_RED)
		Return
	EndIf
	$g_tHaList = TimerInit()
	ReDim $g_aSetHaCams[1][2]
	$g_iSetHaCamN = 0
	ReDim $g_aSetHaImgs[1][2]
	$g_iSetHaImgN = 0
	__SetHaMsg("Frage Home Assistant …", $C_AMBER)
EndFunc   ;==>_SetHaLoad

Func _HaListPoll()
	If $g_iPidHaList = 0 Then Return
	Local $a = __ReadLines($g_iPidHaList, $g_sBufHaList)
	Local $bEnd = (@extended = 1), $p
	For $i = 1 To $a[0]
		If StringLeft($a[$i], 4) = "CAM|" Then
			$p = StringSplit($a[$i], "|", 2)
			If UBound($p) >= 3 Then
				$g_iSetHaCamN += 1
				ReDim $g_aSetHaCams[$g_iSetHaCamN + 1][2]
				$g_aSetHaCams[$g_iSetHaCamN][0] = $p[1]
				$g_aSetHaCams[$g_iSetHaCamN][1] = ($p[2] <> "" ? $p[2] : $p[1])
			EndIf
		ElseIf StringLeft($a[$i], 4) = "IMG|" Then
			$p = StringSplit($a[$i], "|", 2)
			If UBound($p) >= 3 Then
				$g_iSetHaImgN += 1
				ReDim $g_aSetHaImgs[$g_iSetHaImgN + 1][2]
				$g_aSetHaImgs[$g_iSetHaImgN][0] = $p[1]
				$g_aSetHaImgs[$g_iSetHaImgN][1] = ($p[2] <> "" ? $p[2] : $p[1])
				; ha-bambulab kann die A1/P1-Kamera auch als Bild-Entitaet anlegen (..._kamera / ..._camera)
				If StringRegExp($p[1], "_(kamera|camera)$") Then
					$g_iSetHaCamN += 1
					ReDim $g_aSetHaCams[$g_iSetHaCamN + 1][2]
					$g_aSetHaCams[$g_iSetHaCamN][0] = $p[1]
					$g_aSetHaCams[$g_iSetHaCamN][1] = ($p[2] <> "" ? $p[2] : $p[1])
				EndIf
			EndIf
		ElseIf StringLeft($a[$i], 5) = "HAOK|" Then
			$p = StringSplit($a[$i], "|", 2)
			__SetHaMsg(ChrW(0x2713) & "  Verbunden über " & $p[1] & " – " & $g_iSetHaCamN & " Kamera(s), " & $g_iSetHaImgN & " Bild(er)", $C_GREEN)
			If $g_hSet <> 0 Then
				; noch keine Kamera / kein Titelbild gewaehlt -> vorschlagen
				If $g_sSetHaCam = "" And $g_iSetHaCamN > 0 Then $g_sSetHaCam = $g_aSetHaCams[1][0]
				__SetHaCamShow()
				If $g_sSetHaCov = "" Then $g_sSetHaCov = __HaCoverGuess()
				__SetHaCovShow()
			EndIf
		ElseIf StringLeft($a[$i], 9) = "ERR|AUTH|" Then
			__SetHaMsg(ChrW(0x2717) & "  " & StringTrimLeft($a[$i], 9), $C_RED)
		ElseIf StringLeft($a[$i], 4) = "ERR|" Then
			__SetHaMsg(ChrW(0x2717) & "  " & StringTrimLeft($a[$i], 4), $C_RED)
		EndIf
	Next
	If $bEnd Or TimerDiff($g_tHaList) > 40000 Then
		If $g_iPidHaList Then ProcessClose($g_iPidHaList)
		$g_iPidHaList = 0
	EndIf
EndFunc   ;==>_HaListPoll

Func __SetHaMsg($s, $iCol)
	If $g_hSet = 0 Then Return
	GUICtrlSetData($g_idSetHaRes, $s)
	GUICtrlSetColor($g_idSetHaRes, $iCol)
EndFunc   ;==>__SetHaMsg

Func _SetFfBrowse()
	Local $s = FileOpenDialog("ffmpeg.exe wählen", @ProgramFilesDir, "ffmpeg (ffmpeg.exe)", 1, "ffmpeg.exe", $g_hSet)
	If Not @error And $s <> "" Then GUICtrlSetData($g_idSetFf, $s)
	__SetFfShow()
EndFunc   ;==>_SetFfBrowse

Func __SetFfShow()
	Local $sFound = __FfmpegPath(StringStripWS(GUICtrlRead($g_idSetFf), 3))
	GUICtrlSetData($g_idSetFfRes, ($sFound <> "" ? ChrW(0x2713) & "  gefunden: " & $sFound : "ffmpeg.exe nicht gefunden – nur für Videostream-Drucker nötig"))
	GUICtrlSetColor($g_idSetFfRes, ($sFound <> "" ? $C_GREEN : $C_TXT2))
EndFunc   ;==>__SetFfShow

Func _SetRegionCycle()
	$g_iSetRegion = Mod($g_iSetRegion + 1, 2)
	GUICtrlSetData($g_idSetRegion, $REGION_NAMES[$g_iSetRegion] & "  ›")
EndFunc   ;==>_SetRegionCycle

Func _SetConnCycle()
	$g_iSetConn = Mod($g_iSetConn + 1, 4)
	GUICtrlSetData($g_idSetConn, $CONN_NAMES[$g_iSetConn] & "  ›")
EndFunc   ;==>_SetConnCycle

; Text im Konto-Bereich der Einstellungen
Func __AccountLabelUpdate()
	If $g_hSet = 0 Then Return
	Local $sTxt, $iCol
	If $g_sToken = "" Then
		$sTxt = "Nicht angemeldet." & @CRLF & "Optional: für den Status von unterwegs und die automatische Einrichtung."
		$iCol = $C_TXT2
	Else
		$sTxt = "Angemeldet" & ($g_sEmail <> "" ? ": " & $g_sEmail : " (per Token)") & @CRLF & _
				"Status von unterwegs für alle Konto-Drucker"
		$iCol = $C_GREEN
		If $g_bClAuthBad Then
			$sTxt = "Anmeldung abgelaufen – bitte neu anmelden." & @CRLF & "Bis dahin Status nur im Heimnetz"
			$iCol = $C_AMBER
		EndIf
	EndIf
	GUICtrlSetData($g_idSetAcc, $sTxt)
	GUICtrlSetColor($g_idSetAcc, $iCol)
	If $g_iSetTab = 1 Then GUICtrlSetState($g_idSetLogout, $g_sToken <> "" ? $GUI_SHOW : $GUI_HIDE)
EndFunc   ;==>__AccountLabelUpdate

Func _SetCodeEye()
	$g_bCodeVisible = Not $g_bCodeVisible
	GUICtrlSendMsg($g_idSetCode, $BW_EM_SETPASSWORDCHAR, $g_bCodeVisible ? 0 : 0x25CF, 0)
	GUICtrlSetData($g_idSetCode, GUICtrlRead($g_idSetCode)) ; neu zeichnen
	GUICtrlSetColor($g_idSetEye, $g_bCodeVisible ? $C_GREEN : $C_TXT2)
EndFunc   ;==>_SetCodeEye

Func _SetOvlCycle()
	$g_iSetOvl = Mod($g_iSetOvl + 1, 3)
	GUICtrlSetData($g_idSetOvl, $OVL_NAMES[$g_iSetOvl] & "  ›")
EndFunc   ;==>_SetOvlCycle

Func _SetAlphaCycle()
	$g_iSetAlpha = Mod($g_iSetAlpha + 1, 4)
	GUICtrlSetData($g_idSetAlpha, $ALPHA_NAMES[$g_iSetAlpha] & "  ›")
	WinSetTrans($g_hGui, "", $ALPHA_STEPS[$g_iSetAlpha]) ; Vorschau
EndFunc   ;==>_SetAlphaCycle

Func _SetFontCycle()
	$g_iSetFont = Mod($g_iSetFont + 1, 4)
	GUICtrlSetData($g_idSetFont, $FONT_NAMES[$g_iSetFont] & "  ›")
EndFunc   ;==>_SetFontCycle

Func _SetSnapBrowse()
	Local $s = FileSelectFolder("Ordner für Snapshots wählen", "", 1, GUICtrlRead($g_idSetSnap), $g_hSet)
	If Not @error And $s <> "" Then GUICtrlSetData($g_idSetSnap, $s)
EndFunc   ;==>_SetSnapBrowse

Func _SettingsSave()
	If Not __SetPrnStore() Then Return
	Local $sOld = __ConnSig()
	; Profile uebernehmen
	$g_aPrn = $g_aSetPrn
	$g_iPrnN = $g_iSetPrnN
	$g_iPrn = $g_iSetActive
	; Dienste
	$g_sHaUrlIn = StringStripWS(GUICtrlRead($g_idSetHaIn), 3)
	$g_sHaUrlOut = StringStripWS(GUICtrlRead($g_idSetHaOut), 3)
	$g_sHaToken = StringStripWS(GUICtrlRead($g_idSetHaTok), 3)
	$g_bHaInsecure = __TogGet($g_idTogHaIns)
	$g_sFfmpeg = StringStripWS(GUICtrlRead($g_idSetFf), 3)
	; Region gewechselt -> das Token gehoert zur alten Region, also neu anmelden
	If $REGION_KEYS[$g_iSetRegion] <> $g_sRegion Then
		$g_sRegion = $REGION_KEYS[$g_iSetRegion]
		If $g_sToken <> "" Then
			__AccountClear()
			__ToastShow("Region geändert – bitte neu anmelden")
		EndIf
	EndIf
	; Anzeige & Fenster (Kamera/Status an-aus stehen jetzt im Drucker-Profil)
	$g_iOverlay = $g_iSetOvl
	$g_iAlpha = $ALPHA_STEPS[$g_iSetAlpha]
	$g_iFontAdd = $g_iSetFont
	$g_bAspect = __TogGet($g_idTogAspect)
	$g_bOnTop = __TogGet($g_idTogTop)
	$g_bTray = __TogGet($g_idTogTray)
	$g_bNotify = __TogGet($g_idTogNotify)
	$g_bSound = __TogGet($g_idTogSound)
	$g_bStudioWatch = __TogGet($g_idTogStudio)
	Local $sSnap = StringStripWS(GUICtrlRead($g_idSetSnap), 3)
	If $sSnap <> "" Then $g_sSnapDir = $sSnap

	__PrnActivate($g_iPrn)
	__PrnSaveAll()
	IniWrite($INI, "HomeAssistant", "AdresseHeimnetz", $g_sHaUrlIn)
	IniWrite($INI, "HomeAssistant", "AdresseUnterwegs", $g_sHaUrlOut)
	IniWrite($INI, "HomeAssistant", "Token", _DpapiProtect($g_sHaToken))
	IniWrite($INI, "HomeAssistant", "ZertifikatIgnorieren", $g_bHaInsecure ? "1" : "0")
	IniWrite($INI, "Video", "Ffmpeg", $g_sFfmpeg)
	IniWrite($INI, "Anzeige", "Overlay", $g_iOverlay)
	IniWrite($INI, "Anzeige", "Schrift", $g_iFontAdd)
	IniWrite($INI, "Anzeige", "Seitenverhaeltnis", $g_bAspect ? "1" : "0")
	IniWrite($INI, "Fenster", "Vordergrund", $g_bOnTop ? "1" : "0")
	IniWrite($INI, "Fenster", "Infobereich", $g_bTray ? "1" : "0")
	IniWrite($INI, "Fenster", "Deckkraft", $g_iAlpha)
	IniWrite($INI, "Meldungen", "Hinweis", $g_bNotify ? "1" : "0")
	IniWrite($INI, "Meldungen", "Ton", $g_bSound ? "1" : "0")
	IniWrite($INI, "Fenster", "BambuStudioPause", $g_bStudioWatch ? "1" : "0")
	IniWrite($INI, "Snapshots", "Ordner", $g_sSnapDir)
	IniWrite($INI, "Konto", "Region", $g_sRegion)

	WinSetOnTop($g_hGui, "", $g_bOnTop ? 1 : 0)
	WinSetTrans($g_hGui, "", $g_iAlpha)
	__PinStyle()
	__TitleLabel()
	__SettingsDestroy()
	If __ConnSig() <> $sOld Then
		If $g_hFrame Then _GDIPlus_ImageDispose($g_hFrame)
		$g_hFrame = 0
		$g_bFrame = Binary("")
		_HelpersStart()
	EndIf
	__TitleUpdate()
	$g_bDirty = True
EndFunc   ;==>_SettingsSave

; alles, was eine neue Verbindung noetig macht, als ein Vergleichstext
Func __ConnSig()
	Return $g_iPrn & "|" & $g_sIp & "|" & $g_sCode & "|" & $g_sSerial & "|" & $g_iConnMode & "|" & $g_iCamType & "|" & $g_iImgSrc & "|" & $g_sHaCam & "|" & _
			$g_sHaUrlIn & "|" & $g_sHaUrlOut & "|" & $g_sHaToken & "|" & $g_bHaInsecure & "|" & $g_sFfmpeg & "|" & $g_bCamOn & "|" & $g_bStatusOn & "|" & _
			$g_sRegion & "|" & $g_sToken & "|" & $g_bCoverOn & "|" & $g_sHaCover
EndFunc   ;==>__ConnSig

Func _SettingsClose()
	WinSetTrans($g_hGui, "", $g_iAlpha) ; Deckkraft-Vorschau zuruecknehmen
	__SettingsDestroy()
EndFunc   ;==>_SettingsClose

Func __SettingsDestroy()
	_ProbeStop()
	_AccountClose()
	If $g_iPidHaList Then ProcessClose($g_iPidHaList)
	$g_iPidHaList = 0
	If $g_hSet <> 0 Then GUIDelete($g_hSet)
	$g_hSet = 0
	Local $m1[], $m2[]
	$g_mTogState = $m1
	$g_mTogLink = $m2
	GUISwitch($g_hGui)
EndFunc   ;==>__SettingsDestroy

; ---------------------------------------------------------------- Verbindungstest
Func _ProbeStart()
	If $g_iPidProbe Then Return
	Local $sIp = StringStripWS(GUICtrlRead($g_idSetIp), 8)
	Local $sCode = StringStripWS(GUICtrlRead($g_idSetCode), 8)
	Local $sSerial = StringUpper(StringStripWS(GUICtrlRead($g_idSetSerial), 8))
	If $sIp = "" Or $sCode = "" Then
		__ProbeShow("err:Bitte IP-Adresse und Access Code eintragen", "")
		Return
	EndIf
	If Not StringRegExp($sIp, "^[A-Za-z0-9.\-]+$") Then
		__ProbeShow("err:IP-Adresse enthält ungültige Zeichen", "")
		Return
	EndIf
	$g_sBufProbe = ""
	Local $sArgs = '-Mode Probe -Ip "' & $sIp & '" -CamType ' & $CAMTYPE_KEYS[$g_iSetCamT]
	If $sSerial <> "" Then $sArgs &= ' -Serial "' & $sSerial & '"'
	Local $sFf = __FfmpegPath(StringStripWS(GUICtrlRead($g_idSetFf), 3))
	If $sFf <> "" Then $sArgs &= ' -Ffmpeg "' & $sFf & '"'
	$g_iPidProbe = __RunHelperEnv($sArgs, "BW_CODE", $sCode)
	If Not $g_iPidProbe Then
		__ProbeShow("err:PowerShell konnte nicht gestartet werden", "")
		Return
	EndIf
	$g_tProbe = TimerInit()
	GUICtrlSetColor($g_idSetProbeCam, $C_AMBER)
	GUICtrlSetData($g_idSetProbeCam, "Teste Kamera und Status … (bis ca. 30 s)")
	GUICtrlSetData($g_idSetProbeMq, "")
EndFunc   ;==>_ProbeStart

Func _ProbePoll()
	If $g_iPidProbe = 0 Then Return
	Local $a = __ReadLines($g_iPidProbe, $g_sBufProbe)
	Local $bEnd = (@extended = 1)
	For $i = 1 To $a[0]
		If StringLeft($a[$i], 6) = "PROBE|" Then
			Local $p = StringSplit($a[$i], "|", 2)
			If UBound($p) >= 3 Then __ProbeShow($p[1], $p[2])
			_ProbeStop()
			Return
		ElseIf StringLeft($a[$i], 4) = "ERR|" Then
			__ProbeShow("err:" & StringTrimLeft($a[$i], 4), "")
			_ProbeStop()
			Return
		EndIf
	Next
	If $bEnd Or TimerDiff($g_tProbe) > 40000 Then
		__ProbeShow("err:Test ohne Ergebnis beendet", "")
		_ProbeStop()
	EndIf
EndFunc   ;==>_ProbePoll

Func _ProbeStop()
	If $g_iPidProbe Then ProcessClose($g_iPidProbe)
	$g_iPidProbe = 0
EndFunc   ;==>_ProbeStop

; "ok:Text" / "warn:Text" / "err:Text"
Func __ProbeShow($sCam, $sMq)
	If $g_hSet = 0 Then Return
	__ProbeLine($g_idSetProbeCam, "Kamera", $sCam)
	__ProbeLine($g_idSetProbeMq, "Status", $sMq)
EndFunc   ;==>__ProbeShow

Func __ProbeLine($id, $sWhat, $s)
	If $s = "" Then
		GUICtrlSetData($id, "")
		Return
	EndIf
	Local $p = StringInStr($s, ":")
	Local $sKind = StringLeft($s, $p - 1), $sTxt = StringMid($s, $p + 1)
	Local $iCol = $C_RED, $sSym = ChrW(0x2717)
	If $sKind = "ok" Then
		$iCol = $C_GREEN
		$sSym = ChrW(0x2713)
	ElseIf $sKind = "warn" Then
		$iCol = $C_AMBER
		$sSym = "!"
	EndIf
	GUICtrlSetColor($id, $iCol)
	GUICtrlSetData($id, $sSym & "  " & $sWhat & ": " & $sTxt)
EndFunc   ;==>__ProbeLine

; =============================================================================
;  Bambu-Konto: Anmeldung (Ablauf wie ha-bambulab / BambuHelper, siehe Helfer)
; =============================================================================
Func _AccountShow()
	If $g_hAcc <> 0 Then
		WinActivate($g_hAcc)
		Return
	EndIf
	Local Const $W = 440
	Local $hParent = ($g_hSet <> 0 ? $g_hSet : $g_hGui)
	$g_hAcc = GUICreate("Bambu-Konto – BambuWatch", $W, 600, -1, -1, $WS_POPUP, BitOR($WS_EX_TOOLWINDOW, ($g_bOnTop ? $WS_EX_TOPMOST : 0)), $hParent)
	GUISetBkColor($C_BG, $g_hAcc)
	GUISetFont(__Pt(9), 400, 0, "Segoe UI", $g_hAcc)
	GUISetOnEvent($GUI_EVENT_CLOSE, "_AccountClose", $g_hAcc)
	GUISetOnEvent($GUI_EVENT_PRIMARYDOWN, "_DragAny", $g_hAcc)
	__WinStyleModern($g_hAcc)

	Local $y = __DlgHead("Bei Bambu Lab anmelden", $W, "_AccountClose")
	$y = __DlgHint("BambuWatch übernimmt damit alle Drucker des Kontos mit Seriennummer und Access Code (die IP kommt aus dem Status) und zeigt den Druckstatus auch unterwegs. Das Passwort wird nicht gespeichert – nur das Anmelde-Token, verschlüsselt mit Windows.", 20, $y + 8, $W - 40)

	$y = __DlgSection("Mit E-Mail und Passwort", $y)
	$g_idAccEmail = __Field("E-Mail", $g_sEmail, 20, $y, 230)
	$g_idAccPass = __Field("Passwort", "", 260, $y, $W - 280, $ES_PASSWORD)
	$y += 52
	__DragLbl(__Lbl("Region", 20, $y, 50, 28, $SS_CENTERIMAGE, $C_TXT2, 8.5, 400))
	$g_iAccRegion = ($g_hSet <> 0 ? $g_iSetRegion : ($g_sRegion = "China" ? 1 : 0))
	$g_idAccRegion = __CycleLbl($REGION_NAMES[$g_iAccRegion], 70, $y + 3, "_AccRegionCycle", 130)
	$g_idAccLoginBtn = _PillBtn("Anmelden", $W - 140, $y, 120, 28, $BTN_GREEN, "_AccLogin", False, $C_BG)
	$y += 40

	; Zeile fuer den Code (E-Mail oder Authenticator) - erscheint bei Bedarf
	$g_aAccCodeRange[0] = GUICtrlCreateDummy()
	$g_idAccCodeLbl = __Lbl("Code aus der E-Mail", 20, $y, $W - 40, 16, 0, $C_TXT2, 8, 400)
	$g_idAccCode = __Input("", 20, $y + 18, 130)
	_PillBtn("Bestätigen", 160, $y + 17, 110, 26, $BTN_GREEN, "_AccCodeConfirm", False, $C_BG)
	$g_aAccCodeRange[1] = GUICtrlCreateDummy()
	$g_idAccResend = __Lbl("Neuen Code senden", 284, $y + 18, 140, 24, $SS_CENTERIMAGE, $C_BLUE, 8.5, 400)
	GUICtrlSetFont($g_idAccResend, __Pt(8.5), 400, 4, "Segoe UI") ; unterstrichen
	GUICtrlSetOnEvent($g_idAccResend, "_AccResend")
	GUICtrlSetCursor($g_idAccResend, 0)
	$y += 50
	$g_idAccStatus = __Lbl("", 20, $y, $W - 40, 34, 0, $C_TXT2, 8.5, 400)
	$y += 38

	$y = __DlgSection("Oder: Token aus dem Browser", $y)
	$y = __DlgHint("Hilft, wenn die Anmeldung oben blockiert wird: auf bambulab.com anmelden, F12 drücken → Anwendung (Application) → Cookies → bambulab.com → Wert von „token“ kopieren und hier einfügen.", 20, $y, $W - 40)
	$y += 2
	$g_idAccToken = __Input("", 20, $y, $W - 170, $ES_PASSWORD)
	_PillBtn("Token prüfen", $W - 140, $y - 1, 120, 26, $BTN_DARK, "_AccTokenCheck", False, $C_BG)
	$y += 44
	_PillBtn("Schließen", ($W - 110) / 2, $y, 110, 28, $BTN_DARK, "_AccountClose", False, $C_BG)
	$y += 46

	WinMove($g_hAcc, "", 0, 0, $W, $y)
	__DlgCenter($g_hAcc)
	__RangeShow($g_aAccCodeRange, False)
	GUICtrlSetState($g_idAccResend, $GUI_HIDE)
	$g_sAccStage = ""
	GUISetState(@SW_SHOW, $g_hAcc)
	GUISwitch($g_hGui)
EndFunc   ;==>_AccountShow

Func _AccountClose()
	If $g_iPidAcc Then ProcessClose($g_iPidAcc)
	$g_iPidAcc = 0
	If $g_hAcc <> 0 Then GUIDelete($g_hAcc)
	$g_hAcc = 0
	$g_sAccStage = ""
	GUISwitch($g_hGui)
EndFunc   ;==>_AccountClose

Func _AccRegionCycle()
	$g_iAccRegion = Mod($g_iAccRegion + 1, 2)
	GUICtrlSetData($g_idAccRegion, $REGION_NAMES[$g_iAccRegion] & "  ›")
EndFunc   ;==>_AccRegionCycle

Func _AccLogin()
	Local $sEmail = StringStripWS(GUICtrlRead($g_idAccEmail), 3), $sPass = GUICtrlRead($g_idAccPass)
	If $sEmail = "" Or $sPass = "" Then
		__AccMsg("E-Mail und Passwort eintragen", $C_RED)
		Return
	EndIf
	$g_sAccEmail = $sEmail
	$g_bAccByToken = False
	__AccRun("-Mode CloudLogin", "Melde an …", "BW_EMAIL", $sEmail, "BW_PASS", $sPass)
EndFunc   ;==>_AccLogin

Func _AccCodeConfirm()
	Local $sCode = StringStripWS(GUICtrlRead($g_idAccCode), 8)
	If $sCode = "" Then
		__AccMsg("Bitte den Code eintragen", $C_RED)
		Return
	EndIf
	If $g_sAccStage = "tfa" Then
		__AccRun("-Mode CloudTfa", "Prüfe Code …", "BW_EMAIL", $g_sAccEmail, "BW_TFAKEY", $g_sAccTfaKey, "BW_CODE2", $sCode)
	Else
		__AccRun("-Mode CloudCode", "Prüfe Code …", "BW_EMAIL", $g_sAccEmail, "BW_CODE2", $sCode)
	EndIf
EndFunc   ;==>_AccCodeConfirm

Func _AccResend()
	If $g_sAccEmail = "" Then Return
	__AccRun("-Mode CloudResend", "Fordere neuen Code an …", "BW_EMAIL", $g_sAccEmail)
EndFunc   ;==>_AccResend

Func _AccTokenCheck()
	Local $sTok = StringStripWS(GUICtrlRead($g_idAccToken), 3)
	If $sTok = "" Then
		__AccMsg("Bitte zuerst das Token einfügen", $C_RED)
		Return
	EndIf
	$g_sAccEmail = ""
	$g_bAccByToken = True
	__AccRun("-Mode CloudToken", "Prüfe Token …", "BW_TOKEN", $sTok)
EndFunc   ;==>_AccTokenCheck

; startet einen Anmelde-Schritt im Helfer (laeuft im Hintergrund, Antwort in _AccPoll)
Func __AccRun($sArgs, $sBusy, $n1 = "", $v1 = "", $n2 = "", $v2 = "", $n3 = "", $v3 = "")
	If $g_iPidAcc Then Return
	$g_sBufAcc = ""
	$g_sAccTok = ""
	$g_sAccUser = ""
	$g_iAccDevN = 0
	ReDim $g_aAccDev[1][5]
	$g_iPidAcc = __RunHelperEnv($sArgs & " -Region " & $REGION_KEYS[$g_iAccRegion], $n1, $v1, $n2, $v2, $n3, $v3)
	If Not $g_iPidAcc Then
		__AccMsg("PowerShell konnte nicht gestartet werden", $C_RED)
		Return
	EndIf
	$g_tAcc = TimerInit()
	__AccMsg($sBusy, $C_AMBER)
EndFunc   ;==>__AccRun

Func __AccMsg($s, $iCol)
	If $g_hAcc = 0 Then Return
	GUICtrlSetData($g_idAccStatus, $s)
	GUICtrlSetColor($g_idAccStatus, $iCol)
EndFunc   ;==>__AccMsg

; Antworten des Helfers auswerten
Func _AccPoll()
	If $g_iPidAcc = 0 Then Return
	Local $a = __ReadLines($g_iPidAcc, $g_sBufAcc)
	Local $bEnd = (@extended = 1), $s, $p
	For $i = 1 To $a[0]
		$s = $a[$i]
		If $s = "LOGIN|code" Then
			__AccCodeRow("code")
		ElseIf StringLeft($s, 10) = "LOGIN|tfa|" Then
			$g_sAccTfaKey = StringTrimLeft($s, 10)
			__AccCodeRow("tfa")
		ElseIf StringLeft($s, 12) = "LOGIN|token|" Then
			$g_sAccTok = StringTrimLeft($s, 12)
		ElseIf StringLeft($s, 5) = "USER|" Then
			$g_sAccUser = StringTrimLeft($s, 5)
		ElseIf StringLeft($s, 4) = "DEV|" Then
			$p = StringSplit($s, "|", 2)
			If UBound($p) >= 6 Then
				$g_iAccDevN += 1
				ReDim $g_aAccDev[$g_iAccDevN + 1][5]
				For $k = 0 To 4
					$g_aAccDev[$g_iAccDevN][$k] = $p[$k + 1] ; Serie, Name, Access Code, online, Modell
				Next
			EndIf
		ElseIf $s = "DONE" Then
			_AccountClose_Proc()
			__AccDone()
			Return
		ElseIf StringLeft($s, 9) = "ERR|AUTH|" Then
			__AccMsg(StringTrimLeft($s, 9), $C_RED)
		ElseIf StringLeft($s, 4) = "ERR|" Then
			__AccMsg(StringTrimLeft($s, 4), $C_RED)
		EndIf
	Next
	If $bEnd Or TimerDiff($g_tAcc) > 45000 Then
		If TimerDiff($g_tAcc) > 45000 Then __AccMsg("Keine Antwort vom Bambu-Server", $C_RED)
		_AccountClose_Proc()
	EndIf
EndFunc   ;==>_AccPoll

Func _AccountClose_Proc()
	If $g_iPidAcc Then ProcessClose($g_iPidAcc)
	$g_iPidAcc = 0
EndFunc   ;==>_AccountClose_Proc

; Code-Zeile zeigen: "code" = per E-Mail, "tfa" = Authenticator-App
Func __AccCodeRow($sStage)
	$g_sAccStage = $sStage
	__RangeShow($g_aAccCodeRange, True)
	GUICtrlSetData($g_idAccCode, "")
	If $sStage = "tfa" Then
		GUICtrlSetData($g_idAccCodeLbl, "Code aus der Authenticator-App")
		GUICtrlSetState($g_idAccResend, $GUI_HIDE)
		__AccMsg("Zwei-Faktor-Anmeldung: Code aus der Authenticator-App eintragen.", $C_TXT)
	Else
		GUICtrlSetData($g_idAccCodeLbl, "Code aus der E-Mail an " & $g_sAccEmail)
		GUICtrlSetState($g_idAccResend, $GUI_SHOW)
		__AccMsg("Bambu hat dir einen Bestätigungscode per E-Mail geschickt.", $C_TXT)
	EndIf
	GUICtrlSetState($g_idAccCode, $GUI_FOCUS)
EndFunc   ;==>__AccCodeRow

; Anmeldung fertig: alle Drucker des Kontos als Profile uebernehmen (vorhandene per Seriennummer aktualisieren)
Func __AccDone()
	If $g_sAccTok = "" Or $g_sAccUser = "" Then Return
	If $g_iAccDevN = 0 Then Return ; Fehlermeldung kam schon vom Helfer
	$g_sToken = $g_sAccTok
	$g_sUser = $g_sAccUser
	$g_sEmail = ($g_bAccByToken ? "" : $g_sAccEmail)
	$g_sRegion = $REGION_KEYS[$g_iAccRegion]
	$g_bClAuthBad = False
	Local $iNew = __PrnMerge($g_aPrn, $g_iPrnN, $g_iPrn)
	__PrnActivate($g_iPrn)
	__PrnSaveAll()
	__AccountSave()
	If $g_hSet <> 0 Then
		; offene Einstellungen: dieselben Drucker auch in die Arbeitskopie
		__SetPrnStore()
		__PrnMerge($g_aSetPrn, $g_iSetPrnN, $g_iSetActive)
		If $g_iSetPrnSel > $g_iSetPrnN Then $g_iSetPrnSel = $g_iSetActive
		__SetPrnLoad()
		$g_iSetRegion = $g_iAccRegion
		GUICtrlSetData($g_idSetRegion, $REGION_NAMES[$g_iSetRegion] & "  ›")
		__AccountLabelUpdate()
	EndIf
	_AccountClose()
	__TitleLabel()
	If $g_hFrame Then _GDIPlus_ImageDispose($g_hFrame)
	$g_hFrame = 0
	$g_bFrame = Binary("")
	_HelpersStart()
	__ToastShow("Angemeldet · " & $g_iAccDevN & " Drucker" & ($iNew > 0 ? ", " & $iNew & " neu" : ""))
EndFunc   ;==>__AccDone

; Drucker aus dem Konto einpflegen; liefert die Zahl neuer Profile. Ein leeres
; Start-Profil (ohne IP, Code, Seriennummer) wird durch den ersten Konto-Drucker ersetzt.
Func __PrnMerge(ByRef $a, ByRef $n, ByRef $iActive)
	Local $iNew = 0, $k
	For $d = 1 To $g_iAccDevN
		Local $sSer = $g_aAccDev[$d][0]
		$k = 0
		For $r = 1 To $n
			If $a[$r][$PF_SERIAL] = $sSer Then $k = $r
		Next
		If $k = 0 Then
			; leeres Profil wiederverwenden
			For $r = 1 To $n
				If $k = 0 And $a[$r][$PF_IP] = "" And $a[$r][$PF_CODE] = "" And $a[$r][$PF_SERIAL] = "" Then $k = $r
			Next
		EndIf
		If $k = 0 Then
			__PrnAdd($a, $n, $g_aAccDev[$d][1])
			$k = $n
			$iNew += 1
		EndIf
		$a[$k][$PF_SERIAL] = $sSer
		If $a[$k][$PF_NAME] = "" Or StringRegExp($a[$k][$PF_NAME], "^(Mein Drucker|Drucker \d+)$") Then $a[$k][$PF_NAME] = $g_aAccDev[$d][1]
		If $g_aAccDev[$d][2] <> "" Then $a[$k][$PF_CODE] = $g_aAccDev[$d][2]
		If $g_aAccDev[$d][4] <> "" Then $a[$k][$PF_MODEL] = $g_aAccDev[$d][4]
	Next
	If $iActive < 1 Or $iActive > $n Then $iActive = 1
	Return $iNew
EndFunc   ;==>__PrnMerge

Func __AccountSave()
	IniWrite($INI, "Konto", "Token", _DpapiProtect($g_sToken))
	IniWrite($INI, "Konto", "Benutzer", $g_sUser)
	IniWrite($INI, "Konto", "EMail", $g_sEmail)
	IniWrite($INI, "Konto", "Region", $g_sRegion)
EndFunc   ;==>__AccountSave

; Abmelden: Token weg, Heimnetz-Daten bleiben (Kamera funktioniert weiter)
Func _AccountLogout()
	__AccountClear()
	__AccountLabelUpdate()
	_HelpersStart()
	__ToastShow("Bei Bambu abgemeldet")
EndFunc   ;==>_AccountLogout

Func __AccountClear()
	$g_sToken = ""
	$g_sUser = ""
	$g_bClAuthBad = False
	__CloudStop()
	IniWrite($INI, "Konto", "Token", "")
	IniWrite($INI, "Konto", "Benutzer", "")
EndFunc   ;==>__AccountClear

; =============================================================================
;  Hilfe und Changelog
; =============================================================================
Func _HelpShow()
	If $g_hHelp <> 0 Then
		WinActivate($g_hHelp)
		Return
	EndIf
	If Not BitAND(WinGetState($g_hGui), $WIN_STATE_VISIBLE) Then _TrayShow()
	Local Const $W = 560
	$g_hHelp = GUICreate("Hilfe – BambuWatch", $W, 600, -1, -1, $WS_POPUP, BitOR($WS_EX_TOOLWINDOW, ($g_bOnTop ? $WS_EX_TOPMOST : 0)), $g_hGui)
	GUISetBkColor($C_BG, $g_hHelp)
	GUISetFont(__Pt(9), 400, 0, "Segoe UI", $g_hHelp)
	GUISetOnEvent($GUI_EVENT_CLOSE, "_HelpClose", $g_hHelp)
	GUISetOnEvent($GUI_EVENT_PRIMARYDOWN, "_DragAny", $g_hHelp)
	__WinStyleModern($g_hHelp)

	; Kopf: Icon, Titel, Versions-Chip, Schliessen
	GUICtrlCreateIcon($ICO, -1, 20, 16, 48, 48)
	__DragLbl(__Lbl($APP_NAME, 80, 16, 260, 28, $SS_CENTERIMAGE, $C_TXT, 15, 700))
	__DragLbl(__Lbl("Live-Bild und Druckstatus für den Bambu Lab A1", 80, 44, 300, 18, 0, $C_TXT2, 9, 400))
	Local $idChip = __Lbl("v" & $APP_VERSION, $W - 104, 24, 50, 22, BitOR($SS_CENTER, $SS_CENTERIMAGE), 0xFFFFFF, 9, 700)
	GUICtrlSetBkColor($idChip, $C_GREEN)
	Local $idX = __Lbl(ChrW(0xE8BB), $W - 44, 0, 44, 36, BitOR($SS_CENTER, $SS_CENTERIMAGE), $C_TXT2, 9, 400, $GLYPH_FONT)
	GUICtrlSetOnEvent($idX, "_HelpClose")
	GUICtrlSetCursor($idX, 0)
	GUICtrlCreateLabel("", 20, 74, $W - 40, 2)
	GUICtrlSetBkColor(-1, $C_GREEN)

	; Reiter (zwei Seiten, damit das Fenster auch auf Laptops passt)
	$g_aHelpTab[0] = __Lbl("Einrichtung & Bedienung", 20, 84, 190, 26, BitOR($SS_CENTER, $SS_CENTERIMAGE), $C_TXT, 9, 600)
	$g_aHelpTab[1] = __Lbl("Anzeige & Hinweise", 214, 84, 170, 26, BitOR($SS_CENTER, $SS_CENTERIMAGE), $C_TXT2, 9, 600)
	For $i = 0 To 1
		GUICtrlSetOnEvent($g_aHelpTab[$i], "_HelpTabClick")
		GUICtrlSetCursor($g_aHelpTab[$i], 0)
	Next
	$g_idHelpTabLine = GUICtrlCreateLabel("", 20, 110, 190, 2)
	GUICtrlSetBkColor($g_idHelpTabLine, $C_GREEN)

	; Seite 1 (Steuerelement-IDs sind fortlaufend -> Bereich merken zum Ein-/Ausblenden)
	Local $y0 = 112, $y = $y0
	$g_aHelpPage[0][0] = GUICtrlCreateDummy()
	$y = __DlgSection("Einrichtung", $y)
	$y = __HItem("Am einfachsten", "Einstellungen › Konten & Dienste › Bambu-Konto › Anmelden. Alle Drucker des Kontos werden übernommen, die IP kommt aus dem ersten Status.", $y, $W)
	$y = __HItem("Am Drucker", "„LAN Mode Liveview“ einschalten, sonst kein Kamerabild (Bezeichnung je nach Firmware/Sprache leicht anders). Developer Mode ist zum Zuschauen nicht nötig.", $y, $W)
	$y = __HItem("Ohne Konto", "Einstellungen › Drucker: IP-Adresse, Access Code und Seriennummer eintragen, dann „Verbindung testen“. Läuft dann nur im Heimnetz.", $y, $W)
	$y = __HItem("H2D, X1, P2S", "Bild kommt als Videostream – dafür ffmpeg.exe angeben (Konten & Dienste). Status im Heimnetz nur mit „LAN Only“ + Developer Mode am Drucker, sonst über die Cloud.", $y, $W)
	$y = __DlgSection("Bedienung", $y)
	$y = __HItem("Drucker wechseln", "Klick auf den Druckernamen oben links – dort auch „Drucker verwalten“. Kamera, Status und Druckobjekt sind pro Drucker einstellbar.", $y, $W)
	$y = __HItem("Verschieben", "an der Leiste oder direkt am Bild ziehen", $y, $W)
	$y = __HItem("Größe", "am Rand ziehen (Seitenverhältnis bleibt, abschaltbar) oder Rechtsklick › Größe", $y, $W)
	$y = __HItem("Rechtsklick", "aufs Bild öffnet das Menü mit allen Funktionen", $y, $W)
	$y = __HItem("Tasten", "S Snapshot · O Overlay · T Vordergrund · 1/2/3 Größe · F1 Hilfe · F2 Einstellungen · F5 neu verbinden", $y, $W)
	$y = __HItem("Position", "Größe und Platz werden beim Beenden gespeichert", $y, $W)
	$g_aHelpPage[0][1] = GUICtrlCreateDummy()
	Local $y1 = $y

	; Seite 2
	$y = $y0
	$g_aHelpPage[1][0] = GUICtrlCreateDummy()
	$y = __DlgSection("Anzeige", $y)
	$y = __HItem("Punkte links", "1. Punkt Kamera, 2. Punkt Druckstatus (Heimnetz oder Cloud): grün läuft · gelb verbindet · rot Fehler · grau aus (Maus drauf = Details)", $y, $W)
	$y = __HItem("Overlay", "Kompakt: Fortschritt, Restzeit, Fertig-Uhrzeit. Ausführlich: dazu Auftrag, Schicht, Temperaturen, Tempo und aktives Filament mit Farbe", $y, $W)
	$y = __HItem("Taskleiste", "Fortschrittsbalken im Taskleisten-Symbol (grün druckt, gelb pausiert, rot abgebrochen), Prozent im Fenstertitel, Fertig-Uhrzeit im Tooltip", $y, $W)
	$y = __HItem("Bild", "Knopf mit dem Bild oben (oder Rechtsklick): schaltet zwischen Kamerabild und Druckobjekt um. Grün = Druckobjekt ist gewählt.", $y, $W)
	$y = __HItem("Licht", "Glühbirne oben oder Taste L: schaltet die Druckerbeleuchtung – im Heimnetz direkt, sonst über Home Assistant", $y, $W)
	$y = __HItem("Meldungen", "Fenster unten rechts mit Bild des Druckobjekts bei Druckende, Pause (z. B. Filament leer) und Abbruch – mit Ton, wenn gewünscht", $y, $W)
	$y = __HItem("Snapshot", "speichert das Originalbild der Kamera (ohne Overlay) als JPG", $y, $W)
	$y = __HItem("Druckobjekt", "Kommt kein Kamerabild (unterwegs, Kamera aus, Störung), zeigt BambuWatch das Bild des Druckobjekts: Titelbild aus Home Assistant, sonst vom letzten Auftrag in der Bambu-Cloud. Oben links steht, woher es kommt.", $y, $W)
	$y = __DlgSection("Gut zu wissen", $y)
	$y = __HItem("Bambu Studio", "Solange Bambu Studio läuft, trennt BambuWatch alle Verbindungen und verbindet sich danach von selbst wieder (abschaltbar unter Anzeige & Fenster)", $y, $W)
	$y = __HItem("Unterwegs", "Bild und Status kommen über Home Assistant (wenn eingerichtet), sonst der Status über die Bambu-Cloud – oben links steht, woher. Das Heimnetz wird dann nur noch einmal pro Minute probiert.", $y, $W)
	$y = __HItem("Bildrate", "Der A1 liefert Einzelbilder, kein flüssiges Video. Ohne laufenden Druck kommt manchmal gar kein Bild – BambuWatch versucht es dann automatisch weiter.", $y, $W)
	$y = __HItem("Protokoll", "Kamera und Status nutzen die von der Community dokumentierte LAN-Schnittstelle. Bambu Lab unterstützt sie offiziell nicht; ein Firmware-Update kann sie ändern.", $y, $W)
	$y = __HItem("Sicherheit", "Passwort wird nie gespeichert. Access Codes, Bambu- und HA-Token liegen mit Windows (DPAPI) verschlüsselt in der INI. Drucker-Ports bitte nicht ins Internet freigeben – dafür ist der Weg über Home Assistant da.", $y, $W)
	$g_aHelpPage[1][1] = GUICtrlCreateDummy()
	$y = ($y > $y1 ? $y : $y1) + 12
	$g_iHelpTab = 0
	__HelpTabApply()

	_PillBtn("Changelog – was ist neu?", ($W - 220) / 2, $y, 220, 28, $BTN_DARK, "_ChangelogShow", False, $C_BG)
	$y += 42
	GUICtrlCreateLabel("", 20, $y, $W - 40, 1)
	GUICtrlSetBkColor(-1, $C_LINE)
	$y += 10
	Local $idCred = __Lbl("Gebaut von Justus Jonas & Michael Nowak" & @CRLF & "– mit Claude am Druckbett", 20, $y, $W - 40, 34, $SS_CENTER, $C_TXT2, 8.5, 400)
	GUICtrlSetFont($idCred, __Pt(8.5), 400, 2, "Segoe UI") ; kursiv
	__DragLbl($idCred)
	$y += 44
	_PillBtn("Alles klar", ($W - 110) / 2, $y, 110, 28, $BTN_GREEN, "_HelpClose", False, $C_BG)
	$y += 46

	WinMove($g_hHelp, "", 0, 0, $W, $y)
	__DlgCenter($g_hHelp)
	GUISetState(@SW_SHOW, $g_hHelp)
	GUISwitch($g_hGui)
EndFunc   ;==>_HelpShow

Func _HelpTabClick()
	For $i = 0 To 1
		If @GUI_CtrlId = $g_aHelpTab[$i] Then $g_iHelpTab = $i
	Next
	__HelpTabApply()
EndFunc   ;==>_HelpTabClick

; blendet die Steuerelemente der inaktiven Seite aus
Func __HelpTabApply()
	__TabApply($g_hHelp, $g_aHelpTab, $g_aHelpPage, $g_iHelpTab, $g_idHelpTabLine, 110)
EndFunc   ;==>__HelpTabApply

; Reiter fuer Dialoge: Steuerelement-IDs sind fortlaufend, also gehoert alles zwischen
; den beiden Dummy-IDs einer Seite zu dieser Seite.
Func __TabApply($hWin, ByRef $aTabs, ByRef $aPages, $iActive, $idLine, $iLineY)
	For $p = 0 To UBound($aTabs) - 1
		For $id = $aPages[$p][0] + 1 To $aPages[$p][1] - 1
			GUICtrlSetState($id, $p = $iActive ? $GUI_SHOW : $GUI_HIDE)
		Next
		GUICtrlSetColor($aTabs[$p], $p = $iActive ? $C_TXT : $C_TXT2)
	Next
	Local $aPos = ControlGetPos($hWin, "", $aTabs[$iActive])
	If IsArray($aPos) Then GUICtrlSetPos($idLine, $aPos[0], $iLineY, $aPos[2], 2)
EndFunc   ;==>__TabApply

; Bereich von Steuerelementen (zwischen zwei Dummies) ein-/ausblenden
Func __RangeShow($aRange, $bShow)
	For $id = $aRange[0] + 1 To $aRange[1] - 1
		GUICtrlSetState($id, $bShow ? $GUI_SHOW : $GUI_HIDE)
	Next
EndFunc   ;==>__RangeShow

Func _HelpClose()
	_ChangelogClose()
	If $g_hHelp <> 0 Then GUIDelete($g_hHelp)
	$g_hHelp = 0
	GUISwitch($g_hGui)
EndFunc   ;==>_HelpClose

Func _ChangelogShow()
	If $g_hLog <> 0 Then
		WinActivate($g_hLog)
		Return
	EndIf
	Local Const $W = 440
	$g_hLog = GUICreate("Changelog – BambuWatch", $W, 400, -1, -1, $WS_POPUP, BitOR($WS_EX_TOOLWINDOW, ($g_bOnTop ? $WS_EX_TOPMOST : 0)), ($g_hHelp <> 0 ? $g_hHelp : $g_hGui))
	GUISetBkColor($C_BG, $g_hLog)
	GUISetFont(__Pt(9), 400, 0, "Segoe UI", $g_hLog)
	GUISetOnEvent($GUI_EVENT_CLOSE, "_ChangelogClose", $g_hLog)
	GUISetOnEvent($GUI_EVENT_PRIMARYDOWN, "_DragAny", $g_hLog)
	__WinStyleModern($g_hLog)
	Local $y = __DlgHead("Changelog", $W, "_ChangelogClose")
	Local $aV = __ChangelogData()
	For $i = 0 To UBound($aV) - 1
		$y = __DlgSection("Version " & $aV[$i][0] & "  ·  " & $aV[$i][1], $y)
		Local $aL = StringSplit($aV[$i][2], "|", 2)
		For $sLine In $aL
			Local $iLines = __TextLines($sLine, $W - 70, 9)
			__DragLbl(__Lbl("•", 24, $y, 12, 18, 0, $C_GREEN, 9, 700))
			__DragLbl(__Lbl($sLine, 40, $y, $W - 60, (18 + $g_iFontAdd) * $iLines, 0, $C_TXT, 9, 400))
			$y += (18 + $g_iFontAdd) * $iLines + 2
		Next
	Next
	$y += 16
	_PillBtn("Schließen", ($W - 110) / 2, $y, 110, 28, $BTN_GREEN, "_ChangelogClose", False, $C_BG)
	$y += 46
	WinMove($g_hLog, "", 0, 0, $W, $y)
	__DlgCenter($g_hLog)
	GUISetState(@SW_SHOW, $g_hLog)
	GUISwitch($g_hGui)
EndFunc   ;==>_ChangelogShow

Func __ChangelogData()
	Local $aV[9][3] = [ _
			["1.7", "22.10.2026", "Pause, solange Bambu Studio läuft: alle Verbindungen getrennt, danach automatisch wieder verbunden (abschaltbar)"], _
			["1.6.1", "22.10.2026", "Fix: Videostream (H2D, X1, P2S) mit aktuellem ffmpeg – Zertifikat des Druckers wird akzeptiert"], _
			["1.6", "22.10.2026", "Umschalter Kamerabild / Druckobjekt (Knopf oben und Rechtsklick-Menü)|" & _
			"Status-Helfer schonender: kompletter Status höchstens alle 5 Minuten angefordert, bei Verbindungsfehlern wachsende Wartezeit bis 1 Minute"], _
			["1.5", "22.10.2026", "Druckstatus über Home Assistant (ha-bambulab) – Reihenfolge Heimnetz, Home Assistant, Bambu-Cloud; neue Status-Quelle „Nur Home Assistant“|" & _
			"HA-Kamera, Titelbild und Licht werden über die Seriennummer automatisch gefunden|" & _
			"Unterwegs: Heimnetz-Versuche nur noch einmal pro Minute, klarere Anzeige|" & _
			"Fortschrittsbalken im Taskleisten-Symbol|" & _
			"Meldungsfenster mit Bild des Druckobjekts|" & _
			"Druckerlicht an/aus (Glühbirne oben, Taste L)|" & _
			"Ausführliches Overlay: alle AMS-Slots mit Farbe und Material"], _
			["1.4", "22.10.2026", "Kamerabild und Druckstatus an/aus jetzt pro Drucker (Einstellungen › Drucker)|" & _
			"Kein Kamerabild? Dann erscheint das Bild des Druckobjekts – aus Home Assistant (Titelbild) oder vom letzten Cloud-Auftrag|" & _
			"Home Assistant: Bild-Entitäten werden mit geladen, Titelbild wird automatisch vorgeschlagen|" & _
			"Home Assistant: A1/P1-Kamera auch als Bild-Entität (ha-bambulab-Option) nutzbar"], _
			["1.3", "22.10.2026", "Fix: Bambu-Konto mit nur einem Drucker meldete „keine Drucker gefunden“|" & _
			"Fix: Absturz „Duplicate function name“ beim Start|" & _
			"Schriftgröße einstellbar (Standard bis +3 px), Vorgabe +1 px|" & _
			"Genauere Meldung, falls Bambu keine Drucker liefert"], _
			["1.2", "22.10.2026", "Mehrere Drucker: Profile anlegen, Wechsel per Klick auf den Namen oben links|" & _
			"Pro Drucker einstellbar: Status-Quelle, Bildquelle, Kameratyp, Home-Assistant-Kamera|" & _
			"Bambu-Anmeldung übernimmt alle Drucker des Kontos|" & _
			"Kamerabild über Home Assistant – auch unterwegs, mit Adresse fürs Heimnetz und für unterwegs|" & _
			"H2D, X1, P2S: Videostream über ffmpeg, automatische Erkennung des Kameratyps|" & _
			"H2D: beide Düsen im Overlay, AMS-Nummer bei mehreren AMS, Hinweis bei ausgeschaltetem Liveview"], _
			["1.1", "22.10.2026", "Anmeldung mit dem Bambu-Konto (E-Mail-Code, Authenticator-App oder Token aus dem Browser)|" & _
			"Seriennummer und Access Code kommen aus dem Konto, die Drucker-IP aus dem Status – nichts mehr abtippen|" & _
			"Druckstatus und Meldungen auch unterwegs über die Bambu-Cloud, im Heimnetz weiter direkt vom Drucker|" & _
			"Automatische Umschaltung Heimnetz/Cloud, Plakette „Cloud“ im Bild|" & _
			"Einstellungen mit zwei Reitern, Hinweis bei abgelaufener Anmeldung"], _
			["1.0", "22.10.2026", "Live-Bild des Bambu A1 im kleinen, frei skalierbaren Fenster ohne Windows-Rahmen|" & _
			"Druckstatus als Overlay (kompakt / ausführlich): Fortschritt, Restzeit, Fertig-Uhrzeit, Schicht, Temperaturen, Tempo, Filament mit Farbe|" & _
			"Meldung im Infobereich bei Druckende, Pause und Abbruch, optional mit Ton|" & _
			"Snapshot per Klick oder Taste S|" & _
			"Immer im Vordergrund, Deckkraft, Minimieren in den Infobereich, Größen-Vorgaben|" & _
			"Position und Größe werden gemerkt, Seitenverhältnis bleibt beim Ziehen|" & _
			"Einstellungen mit Verbindungstest, Access Code verschlüsselt gespeichert|" & _
			"Comic-TV-Icon, Hilfe und Changelog"]]
	Return $aV
EndFunc   ;==>__ChangelogData

Func _ChangelogClose()
	If $g_hLog <> 0 Then GUIDelete($g_hLog)
	$g_hLog = 0
	GUISwitch($g_hGui)
EndFunc   ;==>_ChangelogClose

; =============================================================================
;  Bausteine fuer die dunklen Dialoge
; =============================================================================
; Kopfzeile mit Titel und X; liefert die naechste y-Position
Func __DlgHead($sTitle, $W, $sCloseFunc)
	__DragLbl(__Lbl($sTitle, 20, 12, $W - 80, 30, $SS_CENTERIMAGE, $C_TXT, 14, 700))
	Local $idX = __Lbl(ChrW(0xE8BB), $W - 44, 0, 44, 36, BitOR($SS_CENTER, $SS_CENTERIMAGE), $C_TXT2, 9, 400, $GLYPH_FONT)
	GUICtrlSetOnEvent($idX, $sCloseFunc)
	GUICtrlSetCursor($idX, 0)
	GUICtrlCreateLabel("", 20, 48, $W - 40, 2)
	GUICtrlSetBkColor(-1, $C_GREEN)
	Return 54
EndFunc   ;==>__DlgHead

Func __DlgSection($sTitle, $y)
	$y += 14
	__DragLbl(__Lbl(StringUpper($sTitle), 20, $y, 400, 16, 0, $C_GREEN, 8, 700))
	Return $y + 22
EndFunc   ;==>__DlgSection

; grauer Hinweistext, Hoehe nach Textlaenge; liefert die naechste y-Position
Func __DlgHint($sText, $x, $y, $w)
	Local $iLines = __TextLines($sText, $w, 8)
	__DragLbl(__Lbl($sText, $x, $y, $w, (16 + $g_iFontAdd) * $iLines + 2, 0, $C_TXT2, 8, 400))
	Return $y + (16 + $g_iFontAdd) * $iLines + 4
EndFunc   ;==>__DlgHint

; Hilfe-Eintrag: Stichwort links, Text rechts (Hoehe nach Textlaenge)
Func __HItem($sKey, $sText, $y, $W)
	Local $iTW = $W - 40 - 112
	Local $iLines = __TextLines($sText, $iTW, 8.5)
	__DragLbl(__Lbl($sKey, 20, $y, 108, 18, 0, $C_TXT, 8.5, 700))
	__DragLbl(__Lbl($sText, 132, $y, $iTW, (17 + $g_iFontAdd) * $iLines + 2, 0, $C_TXT2, 8.5, 400))
	Return $y + 4 + (17 + $g_iFontAdd) * $iLines
EndFunc   ;==>__HItem

; Eingabefeld mit Beschriftung darueber und 1-px-Rahmen
Func __Field($sLabel, $sValue, $x, $y, $w, $iStyle = 0)
	__DragLbl(__Lbl($sLabel, $x, $y, $w, 16, 0, $C_TXT2, 8, 400))
	Return __Input($sValue, $x, $y + 18, $w, $iStyle)
EndFunc   ;==>__Field

Func __Input($sValue, $x, $y, $w, $iStyle = 0)
	; Rahmen aus vier 1-px-Linien (nichts ueberlappt das Eingabefeld)
	__Rect($x, $y, $w, 1)
	__Rect($x, $y + 23, $w, 1)
	__Rect($x, $y + 1, 1, 22)
	__Rect($x + $w - 1, $y + 1, 1, 22)
	Local $id = GUICtrlCreateInput($sValue, $x + 1, $y + 1, $w - 2, 22, BitOR($ES_AUTOHSCROLL, $iStyle), 0)
	GUICtrlSetBkColor($id, $C_PANEL)
	GUICtrlSetColor($id, $C_TXT)
	GUICtrlSetFont($id, __Pt(10), 400, 0, "Segoe UI")
	Return $id
EndFunc   ;==>__Input

Func __Rect($x, $y, $w, $h)
	Local $id = GUICtrlCreateLabel("", $x, $y, $w, $h)
	GUICtrlSetBkColor($id, $C_LINE)
	GUICtrlSetState($id, $GUI_DISABLE)
	Return $id
EndFunc   ;==>__Rect

; Haekchen im Fluent-Stil (Symbolschrift), Text daneben ebenfalls klickbar
Func __Toggle($sText, $bOn, $x, $y, $w)
	Local $idG = __Lbl("", $x, $y, 20, 22, BitOR($SS_CENTER, $SS_CENTERIMAGE), $C_TXT2, 11, 400, $GLYPH_FONT)
	Local $idT = __Lbl($sText, $x + 26, $y, $w - 26, 22, $SS_CENTERIMAGE, $C_TXT, 9, 400)
	GUICtrlSetOnEvent($idG, "_ToggleClick")
	GUICtrlSetOnEvent($idT, "_ToggleClick")
	GUICtrlSetCursor($idG, 0)
	GUICtrlSetCursor($idT, 0)
	$g_mTogState[$idG] = $bOn
	$g_mTogLink[$idG] = $idG
	$g_mTogLink[$idT] = $idG
	__ToggleDraw($idG)
	Return $idG
EndFunc   ;==>__Toggle

Func _ToggleClick()
	Local $id = @GUI_CtrlId
	If Not MapExists($g_mTogLink, $id) Then Return
	Local $idG = $g_mTogLink[$id]
	$g_mTogState[$idG] = Not $g_mTogState[$idG]
	__ToggleDraw($idG)
EndFunc   ;==>_ToggleClick

Func __ToggleDraw($idG)
	Local $bOn = $g_mTogState[$idG]
	GUICtrlSetData($idG, ChrW($bOn ? 0xE73A : 0xE739)) ; CheckboxComposite / Checkbox
	GUICtrlSetColor($idG, $bOn ? $C_GREEN : $C_TXT2)
EndFunc   ;==>__ToggleDraw

Func __TogGet($idG)
	If Not MapExists($g_mTogState, $idG) Then Return False
	Return $g_mTogState[$idG]
EndFunc   ;==>__TogGet

Func __TogSet($idG, $bOn)
	If Not MapExists($g_mTogState, $idG) Then Return
	$g_mTogState[$idG] = ($bOn ? True : False)
	__ToggleDraw($idG)
EndFunc   ;==>__TogSet

; Auswahl, die sich per Klick weiterschaltet
Func __CycleLbl($sText, $x, $y, $sFunc, $w = 98)
	Local $id = __Lbl($sText & "  ›", $x, $y, $w, 22, BitOR($SS_CENTER, $SS_CENTERIMAGE), $C_TXT, 9, 600)
	GUICtrlSetBkColor($id, $C_PANEL)
	GUICtrlSetOnEvent($id, $sFunc)
	GUICtrlSetCursor($id, 0)
	GUICtrlSetTip($id, "Klicken zum Umschalten")
	Return $id
EndFunc   ;==>__CycleLbl

Func __DragLbl($id)
	$g_mDragLbl[$id] = 1
	Return $id
EndFunc   ;==>__DragLbl

Func __DlgCenter($hDlg)
	Local $pM = WinGetPos($g_hGui), $pD = WinGetPos($hDlg)
	If Not IsArray($pM) Or Not IsArray($pD) Then Return
	Local $x = $pM[0] + ($pM[2] - $pD[2]) / 2, $y = $pM[1] + 40
	; nicht aus dem Bildschirm laufen lassen
	If $x < 0 Then $x = 0
	If $y + $pD[3] > @DesktopHeight Then $y = @DesktopHeight - $pD[3] - 40
	If $y < 0 Then $y = 0
	WinMove($hDlg, "", $x, $y)
EndFunc   ;==>__DlgCenter

; Windows 11: runde Ecken + feiner Rahmen (Windows 10 ignoriert das einfach)
Func __WinStyleModern($hWnd)
	Local $tV = DllStructCreate("int")
	DllStructSetData($tV, 1, 2) ; DWMWCP_ROUND
	DllCall("dwmapi.dll", "long", "DwmSetWindowAttribute", "hwnd", $hWnd, "dword", 33, "struct*", $tV, "dword", 4)
	DllStructSetData($tV, 1, 0x00423A36) ; Rahmenfarbe als COLORREF (BGR) = $C_LINE
	DllCall("dwmapi.dll", "long", "DwmSetWindowAttribute", "hwnd", $hWnd, "dword", 34, "struct*", $tV, "dword", 4)
EndFunc   ;==>__WinStyleModern

; =============================================================================
;  Kleine Helfer
; =============================================================================
Func __Lbl($sText, $x, $y, $w, $h, $iStyle, $iColor, $fSize, $iWeight, $sFont = "Segoe UI")
	Local $id = GUICtrlCreateLabel($sText, $x, $y, $w, $h, BitOR($iStyle, $SS_NOPREFIX))
	GUICtrlSetColor($id, $iColor)
	GUICtrlSetBkColor($id, $GUI_BKCOLOR_TRANSPARENT)
	GUICtrlSetFont($id, __Pt($fSize), $iWeight, 0, $sFont)
	Return $id
EndFunc   ;==>__Lbl

; Symbol-Knopf in der Leiste; $iPos 1 = ganz rechts
Func __BarBtn($iGlyph, $iPos, $sTip, $sFunc)
	Local Const $BW = 30
	Local $id = __Lbl(ChrW($iGlyph), $g_iW - $iPos * $BW - ($iPos = 1 ? 0 : 4), 0, $BW, $BAR_H, BitOR($SS_CENTER, $SS_CENTERIMAGE), $C_TXT2, 9.5, 400, $GLYPH_FONT)
	GUICtrlSetResizing($id, BitOR($GUI_DOCKRIGHT, $GUI_DOCKTOP, $GUI_DOCKSIZE))
	GUICtrlSetTip($id, $sTip)
	GUICtrlSetOnEvent($id, $sFunc)
	Return $id
EndFunc   ;==>__BarBtn

Func __Menu($idParent, $sText, $sFunc)
	Local $id = GUICtrlCreateMenuItem($sText, $idParent)
	GUICtrlSetOnEvent($id, $sFunc)
	Return $id
EndFunc   ;==>__Menu

Func __Dummy($sFunc)
	Local $id = GUICtrlCreateDummy()
	GUICtrlSetOnEvent($id, $sFunc)
	Return $id
EndFunc   ;==>__Dummy

Func __DirWritable($sDir)
	Local $sTest = $sDir & "\~bw_write_test.tmp"
	Local $hF = FileOpen($sTest, $FO_OVERWRITE)
	If $hF = -1 Then Return False
	FileClose($hF)
	FileDelete($sTest)
	Return True
EndFunc   ;==>__DirWritable

; Misst die Textbreite mit der echten Schrift (Segoe UI) und liefert die noetige Zeilenzahl
Func __TextLines($sText, $iWidth, $fSize)
	Local $aDC = DllCall("user32.dll", "handle", "GetDC", "hwnd", 0)
	If @error Then Return 2
	Local $hDC = $aDC[0]
	Local $aDpi = DllCall("gdi32.dll", "int", "GetDeviceCaps", "handle", $hDC, "int", 90) ; LOGPIXELSY
	Local $iDpi = (@error ? 96 : $aDpi[0])
	Local $aFont = DllCall("gdi32.dll", "handle", "CreateFontW", "int", -Round(__Pt($fSize) * $iDpi / 72), "int", 0, "int", 0, "int", 0, _
			"int", 400, "dword", 0, "dword", 0, "dword", 0, "dword", 1, "dword", 0, "dword", 0, "dword", 0, "dword", 0, "wstr", "Segoe UI")
	Local $aOld = DllCall("gdi32.dll", "handle", "SelectObject", "handle", $hDC, "handle", $aFont[0])
	Local $tSize = DllStructCreate("long X;long Y")
	DllCall("gdi32.dll", "bool", "GetTextExtentPoint32W", "handle", $hDC, "wstr", $sText, "int", StringLen($sText), "struct*", $tSize)
	DllCall("gdi32.dll", "handle", "SelectObject", "handle", $hDC, "handle", $aOld[0])
	DllCall("gdi32.dll", "bool", "DeleteObject", "handle", $aFont[0])
	DllCall("user32.dll", "int", "ReleaseDC", "hwnd", 0, "handle", $hDC)
	; 92 % der Breite ansetzen, weil der Zeilenumbruch nur an Wortgrenzen erfolgt
	Local $iLines = Ceiling(DllStructGetData($tSize, "X") / ($iWidth * 0.92))
	Return ($iLines < 1 ? 1 : $iLines)
EndFunc   ;==>__TextLines

; ---------------------------------------------------------------- Knoepfe (wie im EHV-Ticker)
; runde "Pillen", per GDI+ gemalt und als Bild-Steuerelement eingesetzt
Func _PillBtn($sText, $x, $y, $w, $h, $iFill, $sFunc, $bOutline = False, $iBg = 0xFFFFFF, $sFont = "Segoe UI", $fSize = 9)
	Local $id = GUICtrlCreatePic("", $x, $y, $w, $h)
	__PillSet($id, $sText, $w, $h, $iFill, $bOutline, $iBg, $sFont, $fSize)
	GUICtrlSetCursor($id, 0) ; Hand
	If $sFunc <> "" Then GUICtrlSetOnEvent($id, $sFunc)
	Return $id
EndFunc   ;==>_PillBtn

Func __PillSet($id, $sText, $w, $h, $iFill, $bOutline, $iBg = 0xFFFFFF, $sFont = "Segoe UI", $fSize = 9)
	Local $hOld = GUICtrlSendMsg($id, $BW_STM_SETIMAGE, $BW_IMAGE_BITMAP, __PillBitmap($sText, $w, $h, $iFill, $bOutline, $iBg, $sFont, $fSize))
	If $hOld Then DllCall("gdi32.dll", "bool", "DeleteObject", "handle", $hOld)
EndFunc   ;==>__PillSet

Func __PillBitmap($sText, $w, $h, $iFill, $bOutline, $iBg, $sFont, $fSize)
	Local $hBmp = _GDIPlus_BitmapCreateFromScan0($w, $h)
	Local $hG = _GDIPlus_ImageGetGraphicsContext($hBmp)
	_GDIPlus_GraphicsSetSmoothingMode($hG, 4)
	_GDIPlus_GraphicsSetTextRenderingHint($hG, 4)
	_GDIPlus_GraphicsClear($hG, BitOR(0xFF000000, $iBg))
	Local $d = $h - 2
	Local $hPath = _GDIPlus_PathCreate()
	_GDIPlus_PathAddArc($hPath, 1, 1, $d, $d, 90, 180)
	_GDIPlus_PathAddArc($hPath, $w - $d - 2, 1, $d, $d, 270, 180)
	_GDIPlus_PathCloseFigure($hPath)
	Local $iTxt
	If $bOutline Then
		Local $hPen = _GDIPlus_PenCreate($iFill, 1.5)
		_GDIPlus_GraphicsDrawPath($hG, $hPath, $hPen)
		_GDIPlus_PenDispose($hPen)
		$iTxt = $iFill
	Else
		Local $hBr = _GDIPlus_LineBrushCreate(0, -1, 0, $h + 1, $iFill, __Darker($iFill))
		_GDIPlus_GraphicsFillPath($hG, $hPath, $hBr)
		_GDIPlus_BrushDispose($hBr)
		$iTxt = 0xFFFFFFFF
	EndIf
	_GDIPlus_PathDispose($hPath)
	Local $hFam = _GDIPlus_FontFamilyCreate($sFont)
	Local $hFont = _GDIPlus_FontCreate($hFam, __Pt($fSize), 1) ; fett, Punkt
	Local $hFmt = _GDIPlus_StringFormatCreate()
	_GDIPlus_StringFormatSetAlign($hFmt, 1)
	_GDIPlus_StringFormatSetLineAlign($hFmt, 1)
	Local $tRect = _GDIPlus_RectFCreate(0, 0, $w, $h)
	Local $hTxtBr = _GDIPlus_BrushCreateSolid($iTxt)
	_GDIPlus_GraphicsDrawStringEx($hG, $sText, $hFont, $tRect, $hFmt, $hTxtBr)
	_GDIPlus_BrushDispose($hTxtBr)
	_GDIPlus_StringFormatDispose($hFmt)
	_GDIPlus_FontDispose($hFont)
	_GDIPlus_FontFamilyDispose($hFam)
	Local $hHBmp = _GDIPlus_BitmapCreateHBITMAPFromBitmap($hBmp)
	_GDIPlus_GraphicsDispose($hG)
	_GDIPlus_BitmapDispose($hBmp)
	Return $hHBmp
EndFunc   ;==>__PillBitmap

Func __Darker($iArgb)
	Local $r = BitAND(BitShift($iArgb, 16), 0xFF), $g = BitAND(BitShift($iArgb, 8), 0xFF), $b = BitAND($iArgb, 0xFF)
	Return BitOR(0xFF000000, BitShift(Int($r * 0.8), -16), BitShift(Int($g * 0.8), -8), Int($b * 0.8))
EndFunc   ;==>__Darker

; ---------------------------------------------------------------- Access Code verschluesseln (Windows DPAPI)
; Ergebnis "dpapi:<hex>"; nur mit demselben Windows-Konto wieder lesbar
Func _DpapiProtect($s)
	If $s = "" Then Return ""
	Local $bIn = StringToBinary($s, $SB_UTF8)
	Local $iLen = BinaryLen($bIn)
	Local $tData = DllStructCreate("byte[" & $iLen & "]")
	DllStructSetData($tData, 1, $bIn)
	Local $tIn = DllStructCreate("dword cb;ptr pb")
	DllStructSetData($tIn, "cb", $iLen)
	DllStructSetData($tIn, "pb", DllStructGetPtr($tData))
	Local $tOut = DllStructCreate("dword cb;ptr pb")
	Local $a = DllCall("crypt32.dll", "bool", "CryptProtectData", "struct*", $tIn, "wstr", "BambuWatch", "ptr", 0, "ptr", 0, "ptr", 0, "dword", 1, "struct*", $tOut)
	If @error Or Not $a[0] Then Return $s ; Notfall: unverschluesselt
	Local $tRes = DllStructCreate("byte[" & DllStructGetData($tOut, "cb") & "]", DllStructGetData($tOut, "pb"))
	Local $bRes = DllStructGetData($tRes, 1)
	DllCall("kernel32.dll", "handle", "LocalFree", "handle", DllStructGetData($tOut, "pb"))
	Return "dpapi:" & StringTrimLeft(String($bRes), 2)
EndFunc   ;==>_DpapiProtect

Func _DpapiUnprotect($s)
	If StringLeft($s, 6) <> "dpapi:" Then Return $s
	Local $bIn = Binary("0x" & StringTrimLeft($s, 6))
	Local $iLen = BinaryLen($bIn)
	If $iLen = 0 Then Return ""
	Local $tData = DllStructCreate("byte[" & $iLen & "]")
	DllStructSetData($tData, 1, $bIn)
	Local $tIn = DllStructCreate("dword cb;ptr pb")
	DllStructSetData($tIn, "cb", $iLen)
	DllStructSetData($tIn, "pb", DllStructGetPtr($tData))
	Local $tOut = DllStructCreate("dword cb;ptr pb")
	Local $a = DllCall("crypt32.dll", "bool", "CryptUnprotectData", "struct*", $tIn, "ptr", 0, "ptr", 0, "ptr", 0, "ptr", 0, "dword", 1, "struct*", $tOut)
	If @error Or Not $a[0] Then Return "" ; anderes Konto/anderer PC -> neu eingeben
	Local $tRes = DllStructCreate("byte[" & DllStructGetData($tOut, "cb") & "]", DllStructGetData($tOut, "pb"))
	Local $sRes = BinaryToString(DllStructGetData($tRes, 1), $SB_UTF8)
	DllCall("kernel32.dll", "handle", "LocalFree", "handle", DllStructGetData($tOut, "pb"))
	Return $sRes
EndFunc   ;==>_DpapiUnprotect
