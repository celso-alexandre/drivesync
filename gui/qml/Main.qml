import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Qt.labs.platform as Platform
import drivesync

ApplicationWindow {
    id: window

    property bool startHidden: false
    readonly property string iconBase: "qrc:/qt/qml/drivesync/icons/"
    readonly property var recent: JSON.parse(backend.recentJson || "[]")
    readonly property bool signedOut: backend.syncState === "signed_out"
    readonly property bool needsFolders: backend.syncState === "no_folders"

    title: "drivesync"
    width: 460
    height: 560
    minimumWidth: 380
    minimumHeight: 420
    visible: false

    function headline() {
        switch (backend.syncState) {
        case "starting": return qsTr("Starting…")
        case "syncing": return qsTr("Syncing…")
        case "idle": return qsTr("Up to date")
        case "paused": return qsTr("Paused")
        case "error": return qsTr("Something needs attention")
        case "signed_out": return qsTr("Sign in to start syncing")
        case "no_folders": return qsTr("Choose folders to sync")
        case "engine_missing": return qsTr("The sync engine is missing")
        }
        return backend.syncState
    }

    function detail() {
        if (backend.syncState === "error" || backend.syncState === "engine_missing")
            return backend.message
        if (backend.lastSync && backend.syncState !== "signed_out")
            return qsTr("Last synced %1").arg(new Date(backend.lastSync).toLocaleString(Qt.locale(), Locale.ShortFormat))
        return ""
    }

    function trayIcon() {
        switch (backend.syncState) {
        case "syncing": return iconBase + "tray-syncing.svg"
        case "idle": return iconBase + "tray-idle.svg"
        case "paused": return iconBase + "tray-paused.svg"
        case "error":
        case "engine_missing": return iconBase + "tray-error.svg"
        }
        return iconBase + "tray-offline.svg"
    }

    function describe(event) {
        switch (event.kind) {
        case "downloaded": return qsTr("Downloaded")
        case "uploaded": return qsTr("Uploaded")
        case "conflict": return qsTr("Conflict")
        case "deleted_in_drive": return qsTr("Moved to Drive trash")
        case "deleted_locally": return qsTr("Moved to local trash")
        case "folder_created": return qsTr("Folder created")
        case "renamed": return qsTr("Renamed")
        case "warning": return qsTr("Warning")
        case "error": return qsTr("Error")
        }
        return event.kind
    }

    function openFolder() {
        if (backend.root)
            Qt.openUrlExternally("file://" + backend.root)
    }

    function showWindow() {
        window.show()
        window.raise()
        window.requestActivate()
    }

    function quitApp() {
        backend.shutdown()
        Qt.quit()
    }

    onClosing: (close) => {
        close.accepted = false
        window.hide()
    }

    Component.onCompleted: {
        backend.start()
        if (!startHidden)
            showWindow()
    }

    Backend {
        id: backend
        onAlert: (title, body) => tray.showMessage(title, body, Platform.SystemTrayIcon.Information, 8000)
        onSignedIn: {
            signIn.close()
            folders.open()
        }
        onSyncStateChanged: {
            if (signedOut && !startHidden && !signIn.visible)
                signIn.open()
        }
    }

    Platform.SystemTrayIcon {
        id: tray
        visible: true
        icon.source: window.trayIcon()
        tooltip: "drivesync: " + window.headline()
        onActivated: (reason) => {
            if (reason === Platform.SystemTrayIcon.Trigger) {
                if (window.visible)
                    window.hide()
                else
                    window.showWindow()
            }
        }

        menu: Platform.Menu {
            Platform.MenuItem { text: window.headline(); enabled: false }
            Platform.MenuSeparator {}
            Platform.MenuItem { text: qsTr("Open Google Drive folder"); enabled: backend.root !== ""; onTriggered: window.openFolder() }
            Platform.MenuItem { text: qsTr("Sync now"); enabled: !window.signedOut && !backend.paused; onTriggered: backend.syncNow() }
            Platform.MenuItem { text: backend.paused ? qsTr("Resume syncing") : qsTr("Pause syncing"); enabled: !window.signedOut; onTriggered: backend.setSyncPaused(!backend.paused) }
            Platform.MenuSeparator {}
            Platform.MenuItem { text: qsTr("Choose folders…"); enabled: !window.signedOut; onTriggered: folders.open() }
            Platform.MenuItem { text: qsTr("Show status"); onTriggered: window.showWindow() }
            Platform.MenuItem { text: window.signedOut ? qsTr("Sign in…") : qsTr("Sign out"); onTriggered: window.signedOut ? signIn.open() : backend.signOut() }
            Platform.MenuItem { text: qsTr("Start at login"); checkable: true; checked: backend.startAtLogin; onTriggered: backend.setLoginStart(!backend.startAtLogin) }
            Platform.MenuSeparator {}
            Platform.MenuItem { text: qsTr("Quit drivesync"); onTriggered: window.quitApp() }
        }
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 18
        spacing: 14

        RowLayout {
            spacing: 14
            Image {
                source: window.trayIcon()
                sourceSize.width: 48
                sourceSize.height: 48
                Layout.preferredWidth: 48
                Layout.preferredHeight: 48
            }
            ColumnLayout {
                spacing: 2
                Layout.fillWidth: true
                Label { text: window.headline(); font.pointSize: Qt.application.font.pointSize * 1.35; font.bold: true }
                Label { text: window.detail(); visible: text !== ""; wrapMode: Text.Wrap; opacity: 0.75; Layout.fillWidth: true }
            }
        }

        RowLayout {
            spacing: 8
            visible: !window.signedOut
            Button { text: qsTr("Open folder"); icon.name: "folder-open"; enabled: backend.root !== ""; onClicked: window.openFolder() }
            Button { text: qsTr("Sync now"); icon.name: "view-refresh"; enabled: !backend.paused; onClicked: backend.syncNow() }
            Button { text: backend.paused ? qsTr("Resume") : qsTr("Pause"); icon.name: backend.paused ? "media-playback-start" : "media-playback-pause"; onClicked: backend.setSyncPaused(!backend.paused) }
        }

        Button {
            visible: window.signedOut
            text: qsTr("Sign in with Google…")
            highlighted: true
            onClicked: signIn.open()
        }

        Button {
            visible: window.needsFolders
            text: qsTr("Choose folders…")
            highlighted: true
            onClicked: folders.open()
        }

        Label { text: qsTr("Recent activity"); font.bold: true; visible: !window.signedOut }

        Frame {
            Layout.fillWidth: true
            Layout.fillHeight: true
            visible: !window.signedOut
            padding: 0

            ListView {
                id: activity
                anchors.fill: parent
                clip: true
                model: window.recent
                ScrollBar.vertical: ScrollBar {}
                delegate: ItemDelegate {
                    required property var modelData
                    width: activity.width
                    contentItem: ColumnLayout {
                        spacing: 1
                        Label {
                            text: window.describe(modelData) + (modelData.path ? " · " + modelData.path : "")
                            elide: Text.ElideMiddle
                            Layout.fillWidth: true
                            color: modelData.kind === "error" || modelData.kind === "conflict" ? palette.highlight : palette.text
                        }
                        Label {
                            text: (modelData.detail ? modelData.detail + " · " : "") + new Date(modelData.time).toLocaleTimeString(Qt.locale(), Locale.ShortFormat)
                            elide: Text.ElideRight
                            opacity: 0.65
                            font.pointSize: Qt.application.font.pointSize * 0.9
                            Layout.fillWidth: true
                        }
                    }
                }
                Label {
                    anchors.centerIn: parent
                    visible: activity.count === 0
                    text: qsTr("Nothing has happened yet.")
                    opacity: 0.6
                }
            }
        }

        RowLayout {
            spacing: 8
            CheckBox { text: qsTr("Start at login"); checked: backend.startAtLogin; onToggled: backend.setLoginStart(checked) }
            Item { Layout.fillWidth: true }
            Button { text: qsTr("Folders…"); flat: true; enabled: !window.signedOut; onClicked: folders.open() }
            Button { text: window.signedOut ? qsTr("Sign in…") : qsTr("Sign out"); flat: true; onClicked: window.signedOut ? signIn.open() : backend.signOut() }
        }
    }

    Dialog {
        id: folders
        title: qsTr("Choose folders to sync")
        modal: true
        anchors.centerIn: parent
        width: Math.min(window.width - 40, 420)
        height: Math.min(window.height - 40, 460)
        standardButtons: Dialog.Save | Dialog.Cancel
        property var choices: []

        onAboutToShow: {
            choices = []
            backend.loadFolders()
        }
        onAccepted: backend.saveFolders(JSON.stringify(choices.filter(c => c.selected).map(c => c.name)))

        Connections {
            target: backend
            function onFoldersJsonChanged() { folders.choices = JSON.parse(backend.foldersJson || "[]") }
        }

        ColumnLayout {
            anchors.fill: parent
            spacing: 8
            Label {
                text: qsTr("Pick the top-level folders of My Drive to keep on this computer. They appear in %1.").arg(backend.root)
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }
            BusyIndicator { running: backend.foldersLoading; visible: running; Layout.alignment: Qt.AlignHCenter }
            Label { text: backend.foldersError; visible: text !== ""; wrapMode: Text.Wrap; color: palette.highlight; Layout.fillWidth: true }
            ListView {
                id: folderList
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                model: folders.choices
                ScrollBar.vertical: ScrollBar {}
                delegate: CheckDelegate {
                    required property var modelData
                    required property int index
                    width: folderList.width
                    text: modelData.name
                    checked: modelData.selected
                    onToggled: folders.choices[index].selected = checked
                }
            }
        }
    }

    Dialog {
        id: signIn
        title: qsTr("Connect your Google account")
        modal: true
        anchors.centerIn: parent
        width: Math.min(window.width - 24, 460)
        height: Math.min(window.height - 24, 540)
        closePolicy: backend.signingIn ? Popup.NoAutoClose : Popup.CloseOnEscape

        property int step: 0
        readonly property var steps: [
            {
                title: qsTr("Let's connect drivesync to Google"),
                body: qsTr("drivesync doesn't use a shared key that someone else controls. Instead, you'll create your own free Google key. It takes about 5 minutes and you only do it once.\n\nKeep this window open: each step has a button that opens the right Google page."),
                link: ""
            },
            {
                title: qsTr("1. Create a project"),
                body: qsTr("Click the button below and sign in to Google if asked.\n\nIn \u201cProject name\u201d type drivesync, then click Create. Wait a few seconds until it's ready."),
                link: "https://console.cloud.google.com/projectcreate"
            },
            {
                title: qsTr("2. Turn on Google Drive access"),
                body: qsTr("Click the button below. At the top of the page, check that your drivesync project is selected.\n\nThen click the blue Enable button."),
                link: "https://console.cloud.google.com/apis/library/drive.googleapis.com"
            },
            {
                title: qsTr("3. Describe your app"),
                body: qsTr("Click the button below, then Get started.\n\nApp name: drivesync. Support email: your email. Click Next.\nAudience: choose External. Click Next.\nContact information: your email. Click Next.\nAgree to the policy, then click Continue and Create."),
                link: "https://console.cloud.google.com/auth/branding"
            },
            {
                title: qsTr("4. Publish it, just for you"),
                body: qsTr("Click the button below, then Publish app, then Confirm.\n\nThis only stops Google from signing you out every week. Nobody else can use your app, and Google doesn't need to review it."),
                link: "https://console.cloud.google.com/auth/audience"
            },
            {
                title: qsTr("5. Create your key"),
                body: qsTr("Click the button below.\n\nApplication type: choose Desktop app. Name: drivesync. Click Create.\n\nGoogle now shows a Client ID and a Client secret. Keep that window open for the next step."),
                link: "https://console.cloud.google.com/auth/clients/create"
            },
            { title: qsTr("6. Paste your key"), body: "", link: "" },
            { title: qsTr("7. Sign in"), body: "", link: "" }
        ]
        readonly property string idText: clientId.text.trim()
        readonly property string secretText: clientSecret.text.trim()
        readonly property bool idLooksRight: /^[0-9]+-[A-Za-z0-9_]+\.apps\.googleusercontent\.com$/.test(idText)
        readonly property bool secretLooksRight: /^GOCSPX-[A-Za-z0-9_-]{10,}$/.test(secretText)

        function idHint() {
            if (idText === "" || idLooksRight)
                return ""
            if (/^GOCSPX-/.test(idText))
                return qsTr("This is the Client secret. Paste it in the box below instead.")
            return qsTr("A Client ID ends with .apps.googleusercontent.com. Copy it again from Google.")
        }

        function secretHint() {
            if (secretText === "" || secretLooksRight)
                return ""
            if (/apps\.googleusercontent\.com$/.test(secretText))
                return qsTr("This is the Client ID. Paste it in the box above instead.")
            return qsTr("A Client secret starts with GOCSPX-. Copy it again from Google.")
        }

        onAboutToShow: step = 0

        ColumnLayout {
            anchors.fill: parent
            spacing: 12

            Label {
                text: signIn.step === 0 ? "" : qsTr("Step %1 of %2").arg(signIn.step).arg(signIn.steps.length - 1)
                visible: text !== ""
                opacity: 0.6
            }
            Label {
                text: signIn.steps[signIn.step].title
                font.bold: true
                font.pointSize: Qt.application.font.pointSize * 1.2
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }
            Label {
                text: signIn.steps[signIn.step].body
                visible: text !== ""
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }
            Button {
                visible: signIn.steps[signIn.step].link !== ""
                text: qsTr("Open this page in your browser")
                icon.name: "internet-web-browser"
                onClicked: Qt.openUrlExternally(signIn.steps[signIn.step].link)
            }

            ColumnLayout {
                visible: signIn.step === 6
                spacing: 6
                Layout.fillWidth: true
                Label { text: qsTr("Copy each value from Google's window and paste it here."); wrapMode: Text.Wrap; Layout.fillWidth: true }
                TextField { id: clientId; placeholderText: qsTr("Client ID"); Layout.fillWidth: true }
                Label { text: signIn.idHint(); visible: text !== ""; color: palette.highlight; wrapMode: Text.Wrap; Layout.fillWidth: true }
                TextField { id: clientSecret; placeholderText: qsTr("Client secret"); echoMode: showSecret.checked ? TextInput.Normal : TextInput.Password; Layout.fillWidth: true }
                Label { text: signIn.secretHint(); visible: text !== ""; color: palette.highlight; wrapMode: Text.Wrap; Layout.fillWidth: true }
                CheckBox { id: showSecret; text: qsTr("Show secret") }
            }

            ColumnLayout {
                visible: signIn.step === 7
                spacing: 8
                Layout.fillWidth: true
                Label {
                    text: qsTr("Click the button. Your browser opens: choose your Google account and click Allow.\n\nGoogle may warn that the app isn't verified. That's expected, because it's your own app: click Advanced, then Go to drivesync.")
                    wrapMode: Text.Wrap
                    Layout.fillWidth: true
                }
                Button {
                    text: backend.signingIn ? qsTr("Waiting for you in the browser…") : qsTr("Sign in with Google")
                    highlighted: true
                    enabled: !backend.signingIn
                    Layout.fillWidth: true
                    onClicked: backend.signIn(signIn.idText, signIn.secretText)
                }
                BusyIndicator { running: backend.signingIn; visible: running; Layout.alignment: Qt.AlignHCenter }
                Label {
                    text: qsTr("Sign-in didn't finish. Check that you clicked Allow, then try again.")
                    visible: !backend.signingIn && backend.signInLog !== "" && backend.signInLog !== qsTr("Starting sign-in…")
                    wrapMode: Text.Wrap
                    color: palette.highlight
                    Layout.fillWidth: true
                }
                Button {
                    id: detailsToggle
                    text: checked ? qsTr("Hide details") : qsTr("Show details")
                    checkable: true
                    flat: true
                    visible: backend.signInLog !== ""
                }
                TextArea {
                    visible: detailsToggle.checked
                    text: backend.signInLog
                    readOnly: true
                    wrapMode: Text.WrapAnywhere
                    selectByMouse: true
                    Layout.fillWidth: true
                }
            }

            Item { Layout.fillHeight: true }

            RowLayout {
                Layout.fillWidth: true
                Button {
                    text: qsTr("Back")
                    flat: true
                    visible: signIn.step > 0
                    enabled: !backend.signingIn
                    onClicked: signIn.step -= 1
                }
                Item { Layout.fillWidth: true }
                Button {
                    visible: signIn.step < 7
                    highlighted: true
                    text: signIn.step === 0 ? qsTr("Start") : qsTr("Next")
                    enabled: signIn.step !== 6 || (signIn.idLooksRight && signIn.secretLooksRight)
                    onClicked: signIn.step += 1
                }
            }
        }
    }
}
