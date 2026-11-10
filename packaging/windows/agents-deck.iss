; agents deck for Windows: a per-user installer, no administrator prompt.
;
;   iscc /DVersion=0.2.0 /DSourceDir=dist\windows /DOutputDir=dist packaging\windows\agents-deck.iss
;
; SourceDir holds agentctl.exe and, when the build made one, the app (AppExe and
; the files next to it). Without the app, the Start menu entry opens the web
; dashboard instead (agentctl web).
;
; Installs to %LOCALAPPDATA%\Programs\agents deck. Silent installs (what the
; updater runs) work with /VERYSILENT /SUPPRESSMSGBOXES /NORESTART (the updater
; adds /CLOSEAPPLICATIONS /RESTARTAPPLICATIONS; the script stops the app and the
; service itself either way); add /MERGETASKS="!startup" to leave "start at
; login" off. After every install the service is started again (agentctl serve).
;
; Signing: pass /DSign and /Ssigntool="<command> $f" to iscc to sign the setup
; and the uninstaller (see sign.ps1). Without /DSign nothing is signed.

#ifndef Version
  #define Version "0.0.0-dev"
#endif
#ifndef SourceDir
  #define SourceDir "..\..\dist\windows"
#endif
#ifndef OutputDir
  #define OutputDir "..\..\dist"
#endif
#ifndef AppExe
  #define AppExe "agents_deck.exe"
#endif
#define HaveApp FileExists(AddBackslash(SourceDir) + AppExe)

[Setup]
AppId={{BB6FC344-88F0-4BE9-95D9-764FA9C295E6}
AppName=agents deck
AppVersion={#Version}
AppVerName=agents deck {#Version}
AppPublisher=agents deck
AppPublisherURL=https://github.com/brushknight/agents-deck
AppSupportURL=https://github.com/brushknight/agents-deck/issues
DefaultDirName={localappdata}\Programs\agents deck
DefaultGroupName=agents deck
DisableProgramGroupPage=yes
DisableDirPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={#OutputDir}
OutputBaseFilename=agents-deck_{#Version}_windows_amd64_setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
; We stop the app and the service ourselves (PrepareToInstall), so no Restart Manager prompt.
CloseApplications=no
RestartApplications=no
UninstallDisplayName=agents deck
#if HaveApp
UninstallDisplayIcon={app}\{#AppExe}
#else
UninstallDisplayIcon={app}\agentctl.exe
#endif
#ifdef Sign
SignTool=signtool
SignedUninstaller=yes
#endif

[Tasks]
Name: startup; Description: "Start agents deck when I log in"; Flags: checkedonce

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
#if HaveApp
Name: "{userprograms}\agents deck"; Filename: "{app}\{#AppExe}"; WorkingDir: "{app}"
#else
Name: "{userprograms}\agents deck"; Filename: "{app}\agentctl.exe"; Parameters: "web"; WorkingDir: "{app}"; Comment: "Open the agents deck dashboard in your browser"
#endif
Name: "{userprograms}\Uninstall agents deck"; Filename: "{uninstallexe}"

[Run]
; Start at login: agentctl install registers the service for this user and starts it.
Filename: "{app}\agentctl.exe"; Parameters: "install"; Flags: runhidden; Tasks: startup; StatusMsg: "Starting the agents deck service..."
; The service runs again after every install, silent ones (the updater) included.
; If agentctl install already started it, this second copy finds it running and exits.
Filename: "{app}\agentctl.exe"; Parameters: "serve"; Flags: runhidden nowait; StatusMsg: "Starting the agents deck service..."
#if HaveApp
Filename: "{app}\{#AppExe}"; Description: "Open agents deck"; Flags: nowait postinstall skipifsilent
; A silent install (the updater) starts the app again if it was running.
Filename: "{app}\{#AppExe}"; Flags: nowait; Check: WizardSilent and AppWasRunning
#endif

[UninstallRun]
Filename: "{app}\agentctl.exe"; Parameters: "uninstall"; Flags: runhidden; RunOnceId: "agentctl-uninstall"
Filename: "{sys}\taskkill.exe"; Parameters: "/F /IM agentctl.exe"; Flags: runhidden; RunOnceId: "kill-agentctl"
#if HaveApp
Filename: "{sys}\taskkill.exe"; Parameters: "/F /IM {#AppExe}"; Flags: runhidden; RunOnceId: "kill-app"
#endif

[Code]
var
  WasRunning: Boolean;

function AppWasRunning(): Boolean;
begin
  Result := WasRunning;
end;

function Kill(Image: String): Boolean;
var
  Code: Integer;
begin
  { taskkill exits 0 when it stopped something, 128 when nothing was running }
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM "' + Image + '"', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Result := Code = 0;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  { The files can't be replaced while they run: stop the app, then the service. }
#if HaveApp
  WasRunning := Kill('{#AppExe}');
#endif
  Kill('agentctl.exe');
  Sleep(500);
  Result := '';
end;
