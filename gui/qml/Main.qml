import QtCore
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Window
import Qt.labs.platform as Platform
import drivesync

ApplicationWindow {
    id: window

    property bool startHidden: false
    readonly property string iconBase: "qrc:/qt/qml/drivesync/icons/"

    readonly property var recent: parseJson(backend.recentJson, [])
    readonly property var progress: parseJson(backend.progressJson, null)
    readonly property var questions: parseJson(backend.attentionJson, [])
    readonly property var selectedFolders: parseJson(backend.selectedJson, [])
    readonly property var conflicts: recent.filter(e => e.kind === "conflict" && prefs.dismissed.indexOf(e.time) < 0).slice(0, 3)
    readonly property bool hasError: backend.syncState === "error" || backend.syncState === "engine_missing"
    readonly property int attentionCount: questions.length + conflicts.length + (hasError ? 1 : 0)
    readonly property bool signedOut: backend.syncState === "signed_out"

    readonly property string lang: prefs.language === "pt-BR" || (prefs.language !== "en-US" && Qt.locale().name.indexOf("pt") === 0) ? "pt-BR" : "en-US"
    readonly property color muted: Qt.alpha(palette.windowText, 0.68)
    readonly property color line: Qt.alpha(palette.windowText, 0.13)
    readonly property color surface: Qt.alpha(palette.windowText, 0.045)
    readonly property color good: "#3aa874"
    readonly property color warn: "#d9822b"

    property var samples: []
    property real speed: 0
    property real etaSeconds: -1
    property real etaShownAt: 0
    readonly property real sampleWindow: 60000
    readonly property real minimumWindow: 10000

    title: "drivesync"
    width: 460
    height: 640
    minimumWidth: 400
    minimumHeight: 520
    visible: false
    color: palette.window

    Settings {
        id: prefs
        category: "gui"
        property string language: "auto"
        property var dismissed: []
    }

    readonly property var pt: ({
        "Starting…": "Iniciando…",
        "Everything is up to date": "Tudo em dia",
        "Last checked %1 · %2 folders": "Verificado %1 · %2 pastas",
        "Last checked %1 · 1 folder": "Verificado %1 · 1 pasta",
        "Checking for changes": "Procurando alterações",
        "Looking through your Drive": "Olhando seu Drive",
        "Syncing your files": "Sincronizando seus arquivos",
        "%1 of %2 files · %3 left": "%1 de %2 arquivos · faltam %3",
        "About %1 left": "Cerca de %1 restantes",
        "Paused": "Pausado",
        "Nothing is copied until you resume.": "Nada é copiado até você retomar.",
        "Choose folders to sync": "Escolha as pastas",
        "Pick which Drive folders to keep on this computer.": "Escolha quais pastas do Drive manter neste computador.",
        "Not connected to Google": "Não conectado ao Google",
        "Connect your Google account to start.": "Conecte sua conta do Google para começar.",
        "1 thing needs you": "1 coisa precisa de você",
        "%1 things need you": "%1 coisas precisam de você",
        "Everything else is up to date": "O resto está em dia",
        "The sync engine is missing": "O mecanismo de sincronização não foi encontrado",
        "Open GoogleDrive folder": "Abrir pasta GoogleDrive",
        "Sync now": "Sincronizar agora",
        "Pause": "Pausar",
        "Resume": "Retomar",
        "Pause syncing": "Pausar sincronização",
        "Resume syncing": "Retomar sincronização",
        "Connect Google account": "Conectar conta do Google",
        "Choose folders…": "Escolher pastas…",
        "Choose folders": "Escolher pastas",
        "Folders on this computer": "Pastas neste computador",
        "Recent activity": "Atividade recente",
        "Now": "Agora",
        "Nothing has happened yet.": "Nada aconteceu ainda.",
        "Start when I log in": "Iniciar ao entrar no sistema",
        "Account": "Conta",
        "Signed in as %1": "Conectado como %1",
        "Language": "Idioma",
        "Automatic": "Automático",
        "Sign out": "Sair da conta",
        "Show activity": "Mostrar atividade",
        "Quit drivesync": "Sair do drivesync",
        "Downloaded": "Baixado",
        "Uploaded": "Enviado",
        "Changed in two places": "Alterado em dois lugares",
        "Moved to Drive trash": "Movido para a lixeira do Drive",
        "Moved to local trash": "Movido para a lixeira local",
        "Folder created": "Pasta criada",
        "Renamed to match Drive": "Renomeado como no Drive",
        "Warning": "Aviso",
        "Couldn't sync": "Não sincronizou",
        "Delete %1 files from %2?": "Apagar %1 arquivos de %2?",
        "They were removed from this computer. That's more than half of the folder, so drivesync stopped to check with you first.": "Eles foram removidos deste computador. É mais da metade da pasta, então o drivesync parou para confirmar com você.",
        "Keep them on Drive": "Manter no Drive",
        "Yes, move to Drive trash": "Sim, mover para a lixeira do Drive",
        "%1 changed in two places": "%1 foi alterado em dois lugares",
        "Nothing was lost. The Drive version kept the name, and yours was saved next to it with \"conflict\" in its name.": "Nada se perdeu. A versão do Drive manteve o nome e a sua foi salva ao lado, com \"conflict\" no nome.",
        "Show both files": "Mostrar os dois arquivos",
        "Got it": "Entendi",
        "Something went wrong": "Algo deu errado",
        "drivesync will try again in a moment.": "O drivesync vai tentar de novo em instantes.",
        "A file changed in two places": "Um arquivo foi alterado em dois lugares",
        "drivesync could not sync something": "O drivesync não conseguiu sincronizar algo",
        "drivesync needs your attention": "O drivesync precisa da sua atenção",
        "What should be on this computer?": "O que deve ficar neste computador?",
        "Ticked folders are copied into your GoogleDrive folder and kept in sync. Everything else stays only on Drive.": "As pastas marcadas são copiadas para a sua pasta GoogleDrive e mantidas em sincronia. O resto fica só no Drive.",
        "Find a folder": "Procurar pasta",
        "Select all": "Selecionar tudo",
        "%1 folders": "%1 pastas",
        "1 folder": "1 pasta",
        "%1 files · %2": "%1 arquivos · %2",
        "1 file · %1": "1 arquivo · %1",
        "Counting…": "Contando…",
        "Very large. The first download could take hours. Start with smaller folders if you're trying drivesync out.": "Muito grande. O primeiro download pode levar horas. Comece com pastas menores se estiver testando o drivesync.",
        "%1 to download": "%1 para baixar",
        "%1 free": "%1 livres",
        "Not enough free space on this computer.": "Não há espaço livre suficiente neste computador.",
        "Cancel": "Cancelar",
        "Start syncing": "Começar a sincronizar",
        "Loading your folders…": "Carregando suas pastas…",
        "Set up drivesync": "Configurar o drivesync",
        "Let's connect your Google Drive": "Vamos conectar seu Google Drive",
        "drivesync doesn't borrow a key from anyone. You'll make your own free Google key, so only you can reach your files. Each step opens the right page and tells you exactly what to click.": "O drivesync não pega emprestada a chave de ninguém. Você vai criar sua própria chave gratuita do Google, para que só você acesse seus arquivos. Cada passo abre a página certa e diz exatamente onde clicar.",
        "About 5 minutes": "Cerca de 5 minutos",
        "Only once": "Só uma vez",
        "Free, no card needed": "Grátis, sem cartão",
        "What we'll do": "O que vamos fazer",
        "Create your key on Google's website": "Criar sua chave no site do Google",
        "Paste it here": "Colar aqui",
        "Sign in and choose your folders": "Entrar e escolher suas pastas",
        "Later": "Depois",
        "Get started": "Começar",
        "Step %1 of %2": "Passo %1 de %2",
        "Create your key": "Criar sua chave",
        "Paste your key": "Colar sua chave",
        "Sign in": "Entrar",
        "Back": "Voltar",
        "Next": "Próximo",
        "Open Google's page": "Abrir a página do Google",
        "Create a project": "Criar um projeto",
        "Click <b>Open Google's page</b> below and sign in to Google if it asks.": "Clique em <b>Abrir a página do Google</b> abaixo e entre no Google se ele pedir.",
        "In <b>Project name</b> type <b>drivesync</b>.": "Em <b>Nome do projeto</b> digite <b>drivesync</b>.",
        "Click <b>Create</b> and wait a few seconds until it's ready.": "Clique em <b>Criar</b> e espere alguns segundos até ficar pronto.",
        "Come back here and click <b>Next</b>.": "Volte aqui e clique em <b>Próximo</b>.",
        "Already have a drivesync project? Just click Next.": "Já tem um projeto drivesync? É só clicar em Próximo.",
        "Turn on Google Drive": "Ativar o Google Drive",
        "At the top of the page, check that it says <b>drivesync</b>. That's the project you just made.": "No topo da página, confira se aparece <b>drivesync</b>. É o projeto que você acabou de criar.",
        "Click the blue button <b>Enable</b>.": "Clique no botão azul <b>Ativar</b>.",
        "Button says <b>Manage</b> instead of Enable? It's already on, so just click Next.": "O botão diz <b>Gerenciar</b> em vez de Ativar? Já está ativado, é só clicar em Próximo.",
        "Describe your app": "Descrever seu app",
        "Click <b>Get started</b>. App name: <b>drivesync</b>. Support email: your email. Click <b>Next</b>.": "Clique em <b>Começar</b>. Nome do app: <b>drivesync</b>. E-mail de suporte: o seu. Clique em <b>Próximo</b>.",
        "Audience: choose <b>External</b>. Click <b>Next</b>.": "Público: escolha <b>Externo</b>. Clique em <b>Próximo</b>.",
        "Contact information: your email. Click <b>Next</b>, agree to the policy and click <b>Create</b>.": "Informações de contato: seu e-mail. Clique em <b>Próximo</b>, aceite a política e clique em <b>Criar</b>.",
        "Google already shows your app's overview? This step is done, so click Next.": "O Google já mostra a visão geral do seu app? Este passo está feito, clique em Próximo.",
        "Publish it, just for you": "Publicar, só para você",
        "Click <b>Publish app</b>, then <b>Confirm</b>.": "Clique em <b>Publicar app</b> e depois em <b>Confirmar</b>.",
        "This only stops Google from signing you out every week. Nobody else can use your app, and Google doesn't need to review it.": "Isso só impede o Google de desconectar você toda semana. Ninguém mais pode usar seu app, e o Google não precisa revisá-lo.",
        "Says <b>In production</b> already? Then just click Next.": "Já aparece <b>Em produção</b>? Então é só clicar em Próximo.",
        "Application type: choose <b>Desktop app</b>. Name: <b>drivesync</b>.": "Tipo de aplicativo: escolha <b>App para computador</b>. Nome: <b>drivesync</b>.",
        "Click <b>Create</b>. Google shows your <b>Client ID</b> and <b>Client secret</b>.": "Clique em <b>Criar</b>. O Google mostra seu <b>ID do cliente</b> e sua <b>Chave secreta do cliente</b>.",
        "Keep that window open and click <b>Next</b> here.": "Deixe essa janela aberta e clique em <b>Próximo</b> aqui.",
        "Google shows a window called <b>OAuth client created</b>. Copy each value with the copy icon next to it, then press Paste here.": "O Google mostra uma janela chamada <b>Cliente OAuth criado</b>. Copie cada valor com o ícone de copiar ao lado e depois toque em Colar aqui.",
        "Client ID": "ID do cliente",
        "Client secret": "Chave secreta do cliente",
        "Paste": "Colar",
        "Looks right": "Parece certo",
        "This is the Client secret. Paste it in the box below instead.": "Esta é a chave secreta. Cole na caixa de baixo.",
        "A Client ID ends with .apps.googleusercontent.com. Copy it again from Google.": "O ID do cliente termina com .apps.googleusercontent.com. Copie de novo no Google.",
        "This looks like the Client ID again. The secret is the line below it in Google's window and starts with <b>GOCSPX-</b>.": "Isso parece o ID do cliente de novo. A chave secreta é a linha de baixo na janela do Google e começa com <b>GOCSPX-</b>.",
        "A Client secret starts with <b>GOCSPX-</b>. Copy it again from Google.": "A chave secreta começa com <b>GOCSPX-</b>. Copie de novo no Google.",
        "Show secret": "Mostrar chave",
        "These are stored in your system's password storage, never in a file. Closed Google's window already? Open it again from <a href='https://console.cloud.google.com/auth/clients'>your clients list</a>.": "Elas ficam guardadas no cofre de senhas do sistema, nunca em arquivo. Já fechou a janela do Google? Abra de novo pela <a href='https://console.cloud.google.com/auth/clients'>sua lista de clientes</a>.",
        "Finish in your browser": "Termine no navegador",
        "Waiting for you…": "Esperando você…",
        "Choose your Google account.": "Escolha sua conta do Google.",
        "Google will say <b>\"Google hasn't verified this app\"</b>. That's expected: the app is the one you just made, and only you use it.": "O Google vai dizer <b>\"O Google não verificou este app\"</b>. Isso é esperado: o app é o que você acabou de criar, e só você o usa.",
        "Click <b>Continue</b>. Older screens call it Advanced → Go to drivesync.": "Clique em <b>Continuar</b>. Telas antigas chamam de Avançado → Acessar drivesync.",
        "Tick <b>See, edit, create and delete your Google Drive files</b> and click <b>Continue</b>. This window moves on by itself.": "Marque <b>Ver, editar, criar e excluir seus arquivos do Google Drive</b> e clique em <b>Continuar</b>. Esta janela avança sozinha.",
        "Browser didn't open? Open it again": "O navegador não abriu? Abrir de novo",
        "Sign-in didn't finish. Check that you clicked Allow, then try again.": "A entrada não terminou. Confira se você permitiu o acesso e tente de novo.",
        "Try again": "Tentar de novo",
        "Show details": "Mostrar detalhes",
        "Hide details": "Ocultar detalhes",
        "just now": "agora mesmo",
        "%1 min ago": "há %1 min"
    })

    function tr(text, a, b, c) {
        let s = lang === "pt-BR" && pt[text] !== undefined ? pt[text] : text
        if (a !== undefined) s = s.replace("%1", a)
        if (b !== undefined) s = s.replace("%2", b)
        if (c !== undefined) s = s.replace("%3", c)
        return s
    }

    function parseJson(text, fallback) {
        try {
            const value = JSON.parse(text || "")
            return value === null && fallback !== null ? fallback : value
        } catch (e) {
            return fallback
        }
    }

    function count(n) {
        return Number(n).toLocaleString(Qt.locale(lang), "f", 0)
    }

    function bytes(n) {
        const units = ["B", "KB", "MB", "GB", "TB"]
        let value = Number(n)
        let unit = 0
        while (value >= 1000 && unit < units.length - 1) {
            value /= 1000
            unit++
        }
        const digits = unit === 0 || value >= 10 ? 0 : 1
        return Number(value).toLocaleString(Qt.locale(lang), "f", digits) + " " + units[unit]
    }

    function duration(seconds) {
        if (seconds < 90) return lang === "pt-BR" ? "1 minuto" : "1 minute"
        const minutes = Math.round(seconds / 60)
        if (minutes < 90) return lang === "pt-BR" ? minutes + " minutos" : minutes + " minutes"
        const hours = Math.round(minutes / 60)
        return lang === "pt-BR" ? hours + " horas" : hours + " hours"
    }

    function ago(iso) {
        if (!iso) return ""
        const seconds = (Date.now() - new Date(iso).getTime()) / 1000
        if (seconds < 60) return tr("just now")
        if (seconds < 3600) return tr("%1 min ago", Math.floor(seconds / 60))
        return new Date(iso).toLocaleString(Qt.locale(lang), Locale.ShortFormat)
    }

    function clock(iso) {
        return new Date(iso).toLocaleTimeString(Qt.locale(lang), Locale.ShortFormat)
    }

    function baseName(p) {
        const i = p.lastIndexOf("/")
        return i < 0 ? p : p.slice(i + 1)
    }

    function folderOf(p) {
        const i = p.lastIndexOf("/")
        return i < 0 ? "" : p.slice(0, i).split("/").join(" / ")
    }

    function parentOf(p) {
        return p.slice(0, Math.max(0, p.lastIndexOf("/")))
    }

    function headline() {
        if (signedOut) return tr("Not connected to Google")
        if (backend.syncState === "engine_missing") return tr("The sync engine is missing")
        if (attentionCount === 1) return tr("1 thing needs you")
        if (attentionCount > 1) return tr("%1 things need you", attentionCount)
        if (backend.syncState === "starting") return tr("Starting…")
        if (backend.syncState === "paused") return tr("Paused")
        if (backend.syncState === "no_folders") return tr("Choose folders to sync")
        if (backend.syncState === "syncing") return progress ? tr("Syncing your files") : tr("Checking for changes")
        return tr("Everything is up to date")
    }

    function subline() {
        if (signedOut) return tr("Connect your Google account to start.")
        if (attentionCount > 0) return tr("Everything else is up to date")
        if (backend.syncState === "paused") return tr("Nothing is copied until you resume.")
        if (backend.syncState === "no_folders") return tr("Pick which Drive folders to keep on this computer.")
        if (backend.syncState === "syncing" && progress)
            return tr("%1 of %2 files · %3 left", count(progress.files_done), count(progress.files), bytes(Math.max(0, progress.bytes - progress.bytes_done)))
        if (backend.syncState === "syncing") return backend.message ? tr(backend.message) + "…" : ""
        if (backend.lastSync)
            return selectedFolders.length === 1 ? tr("Last checked %1 · 1 folder", ago(backend.lastSync)) : tr("Last checked %1 · %2 folders", ago(backend.lastSync), selectedFolders.length)
        return ""
    }

    function stateIcon() {
        if (signedOut) return iconBase + "tray-offline.svg"
        if (attentionCount > 0) return iconBase + "tray-error.svg"
        if (backend.syncState === "paused") return iconBase + "tray-paused.svg"
        if (backend.syncState === "syncing" || backend.syncState === "starting") return iconBase + "tray-syncing.svg"
        return iconBase + "tray-idle.svg"
    }

    function describe(kind) {
        switch (kind) {
        case "downloaded": return tr("Downloaded")
        case "uploaded": return tr("Uploaded")
        case "conflict": return tr("Changed in two places")
        case "deleted_in_drive": return tr("Moved to Drive trash")
        case "deleted_locally": return tr("Moved to local trash")
        case "folder_created": return tr("Folder created")
        case "renamed": return tr("Renamed to match Drive")
        case "warning": return tr("Warning")
        case "error": return tr("Couldn't sync")
        }
        return kind
    }

    function eventColor(kind) {
        if (kind === "uploaded") return good
        if (kind === "error" || kind === "warning" || kind === "conflict") return warn
        if (kind === "downloaded") return palette.highlight
        return muted
    }

    function openFolder(relative) {
        if (!backend.root) return
        Qt.openUrlExternally("file://" + backend.root + (relative ? "/" + relative : ""))
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

    function dismissConflict(time) {
        const list = prefs.dismissed.slice(-49)
        list.push(time)
        prefs.dismissed = list
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

    onProgressChanged: {
        if (!progress) {
            samples = []
            speed = 0
            etaSeconds = -1
            return
        }
        const now = Date.now()
        let list = samples.filter(s => now - s.at <= sampleWindow && s.bytes <= progress.bytes_done && s.files <= progress.files_done)
        list.push({ at: now, bytes: progress.bytes_done, files: progress.files_done })
        samples = list
        const first = list[0]
        const elapsed = (now - first.at) / 1000
        if (now - first.at < minimumWindow || elapsed <= 0) {
            speed = 0
            etaSeconds = -1
            return
        }
        const byteRate = (progress.bytes_done - first.bytes) / elapsed
        const fileRate = (progress.files_done - first.files) / elapsed
        speed = byteRate
        const bytesLeft = Math.max(0, progress.bytes - progress.bytes_done)
        const filesLeft = Math.max(0, progress.files - progress.files_done)
        const byBytes = byteRate > 0 ? bytesLeft / byteRate : Infinity
        const byFiles = fileRate > 0 ? filesLeft / fileRate : Infinity
        const estimate = Math.max(bytesLeft > 0 ? byBytes : 0, filesLeft > 0 ? byFiles : 0)
        if (!isFinite(estimate))
            return
        if (etaSeconds < 0 || now - etaShownAt >= 5000) {
            etaSeconds = estimate
            etaShownAt = now
        }
    }

    Backend {
        id: backend
        onAlert: (title, body) => tray.showMessage(window.tr(title), body, Platform.SystemTrayIcon.Information, 8000)
        onSignedIn: {
            wizard.close()
            folders.showPicker()
        }
        onSyncStateChanged: {
            if (window.signedOut && !window.startHidden && !wizard.visible)
                wizard.begin()
        }
    }

    Platform.SystemTrayIcon {
        id: tray
        visible: true
        icon.source: window.stateIcon()
        tooltip: "drivesync · " + window.headline()
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
            Platform.MenuItem { text: window.tr("Open GoogleDrive folder"); enabled: backend.root !== "" && !window.signedOut; onTriggered: window.openFolder("") }
            Platform.MenuItem { text: window.tr("Sync now"); enabled: !window.signedOut && !backend.paused; onTriggered: backend.syncNow() }
            Platform.MenuItem { text: backend.paused ? window.tr("Resume syncing") : window.tr("Pause syncing"); enabled: !window.signedOut; onTriggered: backend.setSyncPaused(!backend.paused) }
            Platform.MenuSeparator {}
            Platform.MenuItem { text: window.tr("Show activity"); onTriggered: window.showWindow() }
            Platform.MenuItem { text: window.signedOut ? window.tr("Connect Google account") : window.tr("Choose folders…"); onTriggered: window.signedOut ? wizard.begin() : folders.showPicker() }
            Platform.MenuSeparator {}
            Platform.MenuItem { text: window.tr("Quit drivesync"); onTriggered: window.quitApp() }
        }
    }

    component SectionLabel: Label {
        font.pointSize: Qt.application.font.pointSize * 0.85
        font.weight: Font.DemiBold
        font.capitalization: Font.AllUppercase
        font.letterSpacing: 0.6
        color: window.muted
    }

    component Card: Rectangle {
        color: window.surface
        radius: 10
        border.color: window.line
    }

    component QuestionCard: Card {
        id: questionCard
        property string heading
        property string body
        property bool urgent: false
        default property alias buttons: buttonRow.data
        implicitHeight: questionColumn.implicitHeight + 28
        border.color: urgent ? Qt.alpha(window.warn, 0.55) : window.line
        ColumnLayout {
            id: questionColumn
            anchors.fill: parent
            anchors.margins: 14
            spacing: 10
            Label { text: questionCard.heading; font.weight: Font.DemiBold; wrapMode: Text.Wrap; Layout.fillWidth: true }
            Label { text: questionCard.body; color: window.muted; wrapMode: Text.Wrap; lineHeight: 1.15; Layout.fillWidth: true }
            RowLayout { id: buttonRow; spacing: 8 }
        }
    }

    component LanguageBox: ComboBox {
        textRole: "label"
        valueRole: "value"
        implicitWidth: 200
        model: [
            { label: window.tr("Automatic"), value: "auto" },
            { label: "English (US)", value: "en-US" },
            { label: "Português (Brasil)", value: "pt-BR" }
        ]
        currentIndex: indexOfValue(prefs.language)
        onActivated: prefs.language = currentValue
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        RowLayout {
            spacing: 16
            Layout.fillWidth: true
            Layout.margins: 22
            Layout.bottomMargin: 0
            Rectangle {
                Layout.preferredWidth: 52
                Layout.preferredHeight: 52
                radius: 14
                color: window.attentionCount > 0 ? Qt.alpha(window.warn, 0.18) : Qt.alpha(palette.highlight, 0.16)
                Image { anchors.centerIn: parent; source: window.stateIcon(); sourceSize.width: 34; sourceSize.height: 34 }
            }
            ColumnLayout {
                spacing: 2
                Layout.fillWidth: true
                Label { text: window.headline(); font.pointSize: Qt.application.font.pointSize * 1.45; font.weight: Font.DemiBold; wrapMode: Text.Wrap; Layout.fillWidth: true }
                Label { text: window.subline(); visible: text !== ""; color: window.muted; wrapMode: Text.Wrap; Layout.fillWidth: true }
            }
        }

        ColumnLayout {
            visible: backend.syncState === "syncing" && window.attentionCount === 0
            spacing: 6
            Layout.fillWidth: true
            Layout.leftMargin: 22
            Layout.rightMargin: 22
            Layout.topMargin: 16
            ProgressBar {
                Layout.fillWidth: true
                indeterminate: !window.progress || window.progress.bytes === 0
                value: window.progress && window.progress.bytes > 0 ? window.progress.bytes_done / window.progress.bytes : 0
            }
            RowLayout {
                visible: !!window.progress && window.etaSeconds >= 0
                Layout.fillWidth: true
                Label {
                    text: window.etaSeconds >= 0 ? window.tr("About %1 left", window.duration(window.etaSeconds)) : ""
                    color: window.muted
                    font.pointSize: Qt.application.font.pointSize * 0.9
                    Layout.fillWidth: true
                }
                Label { text: window.bytes(window.speed) + "/s"; color: window.muted; font.pointSize: Qt.application.font.pointSize * 0.9 }
            }
        }

        RowLayout {
            spacing: 8
            Layout.margins: 22
            Layout.bottomMargin: 0
            Layout.topMargin: 16
            Button {
                visible: !window.signedOut && backend.syncState !== "no_folders"
                text: window.tr("Open GoogleDrive folder")
                icon.name: "folder-open"
                highlighted: true
                onClicked: window.openFolder("")
            }
            Button {
                visible: !window.signedOut && backend.syncState !== "no_folders" && backend.syncState !== "syncing"
                text: window.tr("Sync now")
                icon.name: "view-refresh"
                enabled: !backend.paused
                onClicked: backend.syncNow()
            }
            Button {
                visible: !window.signedOut && backend.syncState !== "no_folders"
                text: backend.paused ? window.tr("Resume") : window.tr("Pause")
                icon.name: backend.paused ? "media-playback-start" : "media-playback-pause"
                onClicked: backend.setSyncPaused(!backend.paused)
            }
            Button {
                visible: window.signedOut
                text: window.tr("Connect Google account")
                highlighted: true
                onClicked: wizard.begin()
            }
            Button {
                visible: backend.syncState === "no_folders"
                text: window.tr("Choose folders…")
                highlighted: true
                onClicked: folders.showPicker()
            }
        }

        ScrollView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            Layout.topMargin: 18
            contentWidth: availableWidth
            clip: true

            ColumnLayout {
                width: parent.width
                spacing: 12

                Repeater {
                    model: window.questions
                    delegate: QuestionCard {
                        required property var modelData
                        Layout.fillWidth: true
                        Layout.leftMargin: 14
                        Layout.rightMargin: 14
                        urgent: true
                        heading: window.tr("Delete %1 files from %2?", window.count(modelData.count), modelData.folder)
                        body: window.tr("They were removed from this computer. That's more than half of the folder, so drivesync stopped to check with you first.")
                        Button { text: window.tr("Keep them on Drive"); highlighted: true; onClicked: backend.answer(modelData.id, "keep") }
                        Button { text: window.tr("Yes, move to Drive trash"); onClicked: backend.answer(modelData.id, "delete") }
                    }
                }

                Repeater {
                    model: window.conflicts
                    delegate: QuestionCard {
                        required property var modelData
                        Layout.fillWidth: true
                        Layout.leftMargin: 14
                        Layout.rightMargin: 14
                        heading: window.tr("%1 changed in two places", window.baseName(modelData.path))
                        body: window.tr("Nothing was lost. The Drive version kept the name, and yours was saved next to it with \"conflict\" in its name.")
                        Button { text: window.tr("Show both files"); onClicked: window.openFolder(window.parentOf(modelData.path)) }
                        Button { text: window.tr("Got it"); flat: true; onClicked: window.dismissConflict(modelData.time) }
                    }
                }

                QuestionCard {
                    visible: window.hasError
                    Layout.fillWidth: true
                    Layout.leftMargin: 14
                    Layout.rightMargin: 14
                    urgent: true
                    heading: window.tr("Something went wrong")
                    body: (backend.message ? backend.message + "\n" : "") + window.tr("drivesync will try again in a moment.")
                }

                ColumnLayout {
                    visible: !!window.progress && !!window.progress.active && window.progress.active.length > 0
                    spacing: 8
                    Layout.fillWidth: true
                    SectionLabel { text: window.tr("Now"); Layout.leftMargin: 22 }
                    Card {
                        Layout.fillWidth: true
                        Layout.leftMargin: 14
                        Layout.rightMargin: 14
                        implicitHeight: activeColumn.implicitHeight + 16
                        ColumnLayout {
                            id: activeColumn
                            anchors.left: parent.left
                            anchors.right: parent.right
                            anchors.top: parent.top
                            anchors.margins: 12
                            anchors.topMargin: 8
                            spacing: 10
                            Repeater {
                                model: window.progress && window.progress.active ? window.progress.active : []
                                delegate: ColumnLayout {
                                    required property var modelData
                                    spacing: 4
                                    Layout.fillWidth: true
                                    RowLayout {
                                        Layout.fillWidth: true
                                        Label { text: (modelData.direction === "up" ? "↑ " : "↓ ") + window.baseName(modelData.path); elide: Text.ElideMiddle; Layout.fillWidth: true }
                                        Label {
                                            text: modelData.bytes > 0 ? window.bytes(modelData.done) + " / " + window.bytes(modelData.bytes) : ""
                                            color: window.muted
                                            font.pointSize: Qt.application.font.pointSize * 0.9
                                        }
                                    }
                                    ProgressBar { Layout.fillWidth: true; value: modelData.bytes > 0 ? modelData.done / modelData.bytes : 0 }
                                }
                            }
                        }
                    }
                }

                ColumnLayout {
                    visible: window.selectedFolders.length > 0 && !window.progress && !window.signedOut
                    spacing: 8
                    Layout.fillWidth: true
                    SectionLabel { text: window.tr("Folders on this computer"); Layout.leftMargin: 22 }
                    Card {
                        Layout.fillWidth: true
                        Layout.leftMargin: 14
                        Layout.rightMargin: 14
                        implicitHeight: folderColumn.implicitHeight
                        ColumnLayout {
                            id: folderColumn
                            anchors.left: parent.left
                            anchors.right: parent.right
                            spacing: 0
                            Repeater {
                                model: window.selectedFolders
                                delegate: ItemDelegate {
                                    required property string modelData
                                    required property int index
                                    Layout.fillWidth: true
                                    icon.name: "folder"
                                    text: modelData
                                    onClicked: window.openFolder(modelData)
                                    Rectangle { anchors.left: parent.left; anchors.right: parent.right; anchors.bottom: parent.bottom; height: 1; color: window.line; visible: index < window.selectedFolders.length - 1 }
                                    Image {
                                        anchors.right: parent.right
                                        anchors.rightMargin: 12
                                        anchors.verticalCenter: parent.verticalCenter
                                        source: backend.syncState === "idle" ? window.iconBase + "tray-idle.svg" : window.iconBase + "tray-syncing.svg"
                                        sourceSize.width: 18
                                        sourceSize.height: 18
                                    }
                                }
                            }
                        }
                    }
                }

                ColumnLayout {
                    visible: !window.signedOut
                    spacing: 4
                    Layout.fillWidth: true
                    Layout.bottomMargin: 10
                    SectionLabel { text: window.tr("Recent activity"); Layout.leftMargin: 22 }
                    Label { visible: window.recent.length === 0; text: window.tr("Nothing has happened yet."); color: window.muted; Layout.leftMargin: 22; Layout.topMargin: 6 }
                    Repeater {
                        model: window.recent.slice(0, 12)
                        delegate: ItemDelegate {
                            required property var modelData
                            Layout.fillWidth: true
                            Layout.leftMargin: 8
                            Layout.rightMargin: 8
                            onClicked: if (modelData.path) window.openFolder(window.parentOf(modelData.path))
                            contentItem: RowLayout {
                                spacing: 12
                                Rectangle { Layout.preferredWidth: 8; Layout.preferredHeight: 8; radius: 4; color: window.eventColor(modelData.kind) }
                                ColumnLayout {
                                    spacing: 0
                                    Layout.fillWidth: true
                                    Label { text: modelData.path ? window.baseName(modelData.path) : window.describe(modelData.kind); elide: Text.ElideMiddle; Layout.fillWidth: true }
                                    Label {
                                        text: (modelData.path ? window.folderOf(modelData.path) + " · " : "") + window.describe(modelData.kind)
                                        color: window.muted
                                        elide: Text.ElideRight
                                        font.pointSize: Qt.application.font.pointSize * 0.9
                                        Layout.fillWidth: true
                                    }
                                }
                                Label { text: window.clock(modelData.time); color: window.muted; font.pointSize: Qt.application.font.pointSize * 0.9 }
                            }
                        }
                    }
                }
            }
        }

        Rectangle { Layout.fillWidth: true; Layout.preferredHeight: 1; color: window.line }

        RowLayout {
            Layout.fillWidth: true
            Layout.leftMargin: 14
            Layout.rightMargin: 14
            Layout.preferredHeight: 56
            spacing: 6
            CheckBox {
                text: window.tr("Start when I log in")
                checked: backend.startAtLogin
                onToggled: backend.setLoginStart(checked)
                Layout.fillWidth: true
            }
            Button { text: window.tr("Choose folders"); flat: true; enabled: !window.signedOut; onClicked: folders.showPicker() }
            Button { text: window.tr("Account"); flat: true; onClicked: accountMenu.open() }
        }
    }

    Popup {
        id: accountMenu
        modal: true
        anchors.centerIn: parent
        width: Math.min(window.width - 40, 380)
        padding: 18
        ColumnLayout {
            anchors.fill: parent
            spacing: 14
            Label { text: window.tr("Account"); font.weight: Font.DemiBold; font.pointSize: Qt.application.font.pointSize * 1.15 }
            Label { visible: backend.account !== ""; text: window.tr("Signed in as %1", backend.account); color: window.muted; wrapMode: Text.WrapAnywhere; Layout.fillWidth: true }
            RowLayout {
                Layout.fillWidth: true
                Label { text: window.tr("Language"); Layout.fillWidth: true }
                LanguageBox {}
            }
            Button {
                text: window.signedOut ? window.tr("Connect Google account") : window.tr("Sign out")
                Layout.alignment: Qt.AlignRight
                onClicked: {
                    accountMenu.close()
                    if (window.signedOut)
                        wizard.begin()
                    else
                        backend.signOut()
                }
            }
        }
    }

    Window {
        id: folders
        title: window.tr("Choose folders")
        width: 520
        height: 660
        minimumWidth: 420
        minimumHeight: 480
        color: palette.window
        modality: Qt.ApplicationModal

        property var choices: []
        property string filter: ""
        property real freeBytes: -1
        readonly property var shown: choices.filter(c => c.name.toLowerCase().indexOf(filter.toLowerCase()) >= 0)
        readonly property var picked: choices.filter(c => c.selected)
        readonly property real pickedBytes: picked.reduce((sum, c) => sum + (c.bytes || 0), 0)
        readonly property real allBytes: shown.reduce((sum, c) => sum + (c.bytes || 0), 0)
        readonly property bool sized: choices.length > 0 && choices.every(c => c.bytes !== undefined)
        readonly property real largeBytes: 20e9
        readonly property bool tooBig: sized && freeBytes >= 0 && pickedBytes > freeBytes

        function showPicker() {
            choices = []
            filter = ""
            search.text = ""
            freeBytes = backend.freeSpace()
            backend.loadFolders()
            show()
            raise()
            requestActivate()
        }

        function setAll(on) {
            const list = choices.slice()
            for (const c of list) {
                if (c.name.toLowerCase().indexOf(filter.toLowerCase()) >= 0)
                    c.selected = on
            }
            choices = list
        }

        function toggle(name, on) {
            const list = choices.slice()
            for (const c of list) {
                if (c.name === name)
                    c.selected = on
            }
            choices = list
        }

        Connections {
            target: backend
            function onFoldersJsonChanged() {
                const fresh = window.parseJson(backend.foldersJson, [])
                const previous = {}
                for (const c of folders.choices)
                    previous[c.name] = c.selected
                for (const c of fresh) {
                    if (previous[c.name] !== undefined)
                        c.selected = previous[c.name]
                }
                folders.choices = fresh
            }
        }

        Shortcut {
            sequences: [StandardKey.SelectAll]
            enabled: !search.activeFocus
            onActivated: folders.setAll(true)
        }

        ColumnLayout {
            anchors.fill: parent
            spacing: 0

            ColumnLayout {
                Layout.fillWidth: true
                Layout.margins: 24
                Layout.bottomMargin: 0
                spacing: 6
                Label { text: window.tr("What should be on this computer?"); font.pointSize: Qt.application.font.pointSize * 1.6; font.weight: Font.DemiBold; wrapMode: Text.Wrap; Layout.fillWidth: true }
                Label { text: window.tr("Ticked folders are copied into your GoogleDrive folder and kept in sync. Everything else stays only on Drive."); color: window.muted; wrapMode: Text.Wrap; Layout.fillWidth: true }
            }

            TextField {
                id: search
                placeholderText: window.tr("Find a folder")
                Layout.fillWidth: true
                Layout.margins: 24
                Layout.bottomMargin: 0
                Layout.topMargin: 16
                onTextChanged: folders.filter = text
            }

            Card {
                Layout.fillWidth: true
                Layout.fillHeight: true
                Layout.margins: 14
                Layout.bottomMargin: 0
                clip: true

                ColumnLayout {
                    anchors.fill: parent
                    spacing: 0

                    BusyIndicator { running: backend.foldersLoading; visible: running; Layout.alignment: Qt.AlignHCenter; Layout.topMargin: 30 }
                    Label { text: window.tr("Loading your folders…"); visible: backend.foldersLoading; color: window.muted; Layout.alignment: Qt.AlignHCenter }
                    Label { text: backend.foldersError; visible: text !== ""; wrapMode: Text.Wrap; color: window.warn; Layout.fillWidth: true; Layout.margins: 14 }

                    RowLayout {
                        visible: folders.shown.length > 0
                        Layout.fillWidth: true
                        Layout.leftMargin: 6
                        Layout.rightMargin: 12
                        CheckBox {
                            id: selectAll
                            tristate: true
                            text: window.tr("Select all")
                            font.weight: Font.DemiBold
                            checkState: {
                                const n = folders.shown.filter(c => c.selected).length
                                return n === 0 ? Qt.Unchecked : n === folders.shown.length ? Qt.Checked : Qt.PartiallyChecked
                            }
                            nextCheckState: () => checkState === Qt.Checked ? Qt.Unchecked : Qt.Checked
                            onClicked: folders.setAll(checkState === Qt.Checked)
                            Layout.fillWidth: true
                        }
                        Label {
                            color: window.muted
                            font.pointSize: Qt.application.font.pointSize * 0.9
                            text: (folders.shown.length === 1 ? window.tr("1 folder") : window.tr("%1 folders", folders.shown.length)) + (folders.sized ? " · " + window.bytes(folders.allBytes) : "")
                        }
                    }
                    Rectangle { visible: folders.shown.length > 0; Layout.fillWidth: true; Layout.preferredHeight: 1; color: window.line }

                    ListView {
                        id: folderList
                        Layout.fillWidth: true
                        Layout.fillHeight: true
                        clip: true
                        model: folders.shown
                        ScrollBar.vertical: ScrollBar {}
                        delegate: ItemDelegate {
                            id: row
                            required property var modelData
                            readonly property bool large: (modelData.bytes || 0) >= folders.largeBytes
                            width: folderList.width
                            onClicked: folders.toggle(modelData.name, !modelData.selected)
                            contentItem: RowLayout {
                                spacing: 10
                                CheckBox {
                                    checked: row.modelData.selected
                                    onToggled: folders.toggle(row.modelData.name, checked)
                                    Layout.alignment: Qt.AlignTop
                                }
                                Image {
                                    source: "image://theme/folder"
                                    sourceSize.width: 20
                                    sourceSize.height: 20
                                    Layout.alignment: Qt.AlignTop
                                    Layout.topMargin: 10
                                }
                                ColumnLayout {
                                    spacing: 3
                                    Layout.fillWidth: true
                                    Layout.topMargin: 8
                                    RowLayout {
                                        Layout.fillWidth: true
                                        Label { text: row.modelData.name; elide: Text.ElideRight; Layout.fillWidth: true }
                                        Label {
                                            color: window.muted
                                            font.pointSize: Qt.application.font.pointSize * 0.9
                                            text: row.modelData.bytes === undefined ? (backend.foldersSizing ? window.tr("Counting…") : "")
                                                : row.modelData.files === 1 ? window.tr("1 file · %1", window.bytes(row.modelData.bytes))
                                                : window.tr("%1 files · %2", window.count(row.modelData.files), window.bytes(row.modelData.bytes))
                                        }
                                    }
                                    Label {
                                        visible: row.large && row.modelData.selected
                                        text: window.tr("Very large. The first download could take hours. Start with smaller folders if you're trying drivesync out.")
                                        color: window.warn
                                        wrapMode: Text.Wrap
                                        font.pointSize: Qt.application.font.pointSize * 0.9
                                        Layout.fillWidth: true
                                        Layout.bottomMargin: 6
                                    }
                                }
                            }
                        }
                    }
                }
            }

            ColumnLayout {
                Layout.fillWidth: true
                Layout.margins: 24
                Layout.bottomMargin: 0
                Layout.topMargin: 14
                spacing: 6
                RowLayout {
                    Layout.fillWidth: true
                    Label {
                        text: (folders.picked.length === 1 ? window.tr("1 folder") : window.tr("%1 folders", folders.picked.length)) + (folders.sized ? " · " + window.tr("%1 to download", window.bytes(folders.pickedBytes)) : "")
                        font.weight: Font.DemiBold
                        Layout.fillWidth: true
                    }
                    Label { visible: folders.freeBytes >= 0; text: window.tr("%1 free", window.bytes(folders.freeBytes)); color: window.muted }
                }
                ProgressBar {
                    visible: folders.sized && folders.freeBytes > 0
                    Layout.fillWidth: true
                    value: folders.freeBytes > 0 ? Math.min(1, folders.pickedBytes / folders.freeBytes) : 0
                }
                Label { visible: folders.tooBig; text: window.tr("Not enough free space on this computer."); color: window.warn }
            }

            RowLayout {
                Layout.fillWidth: true
                Layout.margins: 20
                spacing: 10
                Button { text: window.tr("Cancel"); onClicked: folders.close() }
                Item { Layout.fillWidth: true }
                Button {
                    text: window.tr("Start syncing")
                    highlighted: true
                    enabled: !backend.foldersLoading && backend.foldersError === "" && !folders.tooBig
                    onClicked: {
                        backend.saveFolders(JSON.stringify(folders.picked.map(c => c.name)))
                        folders.close()
                        window.showWindow()
                    }
                }
            }
        }
    }

    Window {
        id: wizard
        title: window.tr("Set up drivesync")
        width: 520
        height: 660
        minimumWidth: 460
        minimumHeight: 560
        color: palette.window
        modality: Qt.ApplicationModal

        property int step: 0
        property bool attempted: false
        readonly property int lastStep: 7
        readonly property string idText: clientId.text.trim()
        readonly property string secretText: clientSecret.text.trim()
        readonly property bool idLooksRight: /^[0-9]+-[A-Za-z0-9_]+\.apps\.googleusercontent\.com$/.test(idText)
        readonly property bool secretLooksRight: /^GOCSPX-[A-Za-z0-9_-]{10,}$/.test(secretText)
        readonly property string loginUrl: {
            const match = backend.signInLog.match(/https:\/\/accounts\.google\.com\/\S+/)
            return match ? match[0] : ""
        }
        readonly property bool signInFailed: attempted && !backend.signingIn && step === lastStep

        readonly property var pages: [
            {},
            { title: window.tr("Create a project"), link: "https://console.cloud.google.com/projectcreate",
              lines: [window.tr("Click <b>Open Google's page</b> below and sign in to Google if it asks."), window.tr("In <b>Project name</b> type <b>drivesync</b>."), window.tr("Click <b>Create</b> and wait a few seconds until it's ready."), window.tr("Come back here and click <b>Next</b>.")],
              hint: window.tr("Already have a drivesync project? Just click Next.") },
            { title: window.tr("Turn on Google Drive"), link: "https://console.cloud.google.com/apis/library/drive.googleapis.com",
              lines: [window.tr("Click <b>Open Google's page</b> below and sign in to Google if it asks."), window.tr("At the top of the page, check that it says <b>drivesync</b>. That's the project you just made."), window.tr("Click the blue button <b>Enable</b>."), window.tr("Come back here and click <b>Next</b>.")],
              hint: window.tr("Button says <b>Manage</b> instead of Enable? It's already on, so just click Next.") },
            { title: window.tr("Describe your app"), link: "https://console.cloud.google.com/auth/branding",
              lines: [window.tr("Click <b>Get started</b>. App name: <b>drivesync</b>. Support email: your email. Click <b>Next</b>."), window.tr("Audience: choose <b>External</b>. Click <b>Next</b>."), window.tr("Contact information: your email. Click <b>Next</b>, agree to the policy and click <b>Create</b>."), window.tr("Come back here and click <b>Next</b>.")],
              hint: window.tr("Google already shows your app's overview? This step is done, so click Next.") },
            { title: window.tr("Publish it, just for you"), link: "https://console.cloud.google.com/auth/audience",
              lines: [window.tr("Click <b>Publish app</b>, then <b>Confirm</b>."), window.tr("This only stops Google from signing you out every week. Nobody else can use your app, and Google doesn't need to review it."), window.tr("Come back here and click <b>Next</b>.")],
              hint: window.tr("Says <b>In production</b> already? Then just click Next.") },
            { title: window.tr("Create your key"), link: "https://console.cloud.google.com/auth/clients/create",
              lines: [window.tr("Application type: choose <b>Desktop app</b>. Name: <b>drivesync</b>."), window.tr("Click <b>Create</b>. Google shows your <b>Client ID</b> and <b>Client secret</b>."), window.tr("Keep that window open and click <b>Next</b> here.")],
              hint: "" },
            {},
            {}
        ]

        function groupName() {
            if (step <= 5) return tr("Create your key")
            if (step === 6) return tr("Paste your key")
            return tr("Sign in")
        }

        function tr(text, a, b, c) {
            return window.tr(text, a, b, c)
        }

        function begin() {
            step = 0
            attempted = false
            show()
            raise()
            requestActivate()
        }

        function idHint() {
            if (idText === "" || idLooksRight) return ""
            if (/^GOCSPX-/.test(idText)) return tr("This is the Client secret. Paste it in the box below instead.")
            return tr("A Client ID ends with .apps.googleusercontent.com. Copy it again from Google.")
        }

        function secretHint() {
            if (secretText === "" || secretLooksRight) return ""
            if (/apps\.googleusercontent\.com/.test(secretText)) return tr("This looks like the Client ID again. The secret is the line below it in Google's window and starts with <b>GOCSPX-</b>.")
            return tr("A Client secret starts with <b>GOCSPX-</b>. Copy it again from Google.")
        }

        function startSignIn() {
            attempted = true
            backend.signIn(idText, secretText)
        }

        onStepChanged: {
            if (step === lastStep && !backend.signingIn)
                startSignIn()
        }
        onClosing: backend.cancelSignIn()

        component Instruction: RowLayout {
            id: instruction
            property int number
            property string text
            spacing: 14
            Layout.fillWidth: true
            Rectangle {
                Layout.preferredWidth: 26
                Layout.preferredHeight: 26
                Layout.alignment: Qt.AlignTop
                radius: 13
                color: Qt.alpha(palette.highlight, 0.18)
                Label { anchors.centerIn: parent; text: instruction.number; color: palette.highlight; font.weight: Font.DemiBold }
            }
            Label { text: instruction.text; textFormat: Text.StyledText; wrapMode: Text.Wrap; lineHeight: 1.2; Layout.fillWidth: true; Layout.topMargin: 3 }
        }

        component Hint: Card {
            id: hint
            property string text
            implicitHeight: hintLabel.implicitHeight + 24
            Layout.fillWidth: true
            Label {
                id: hintLabel
                anchors.fill: parent
                anchors.margins: 12
                text: hint.text
                textFormat: Text.StyledText
                wrapMode: Text.Wrap
                color: window.muted
                onLinkActivated: (link) => Qt.openUrlExternally(link)
            }
        }

        ColumnLayout {
            anchors.fill: parent
            spacing: 0

            ColumnLayout {
                visible: wizard.step > 0
                Layout.fillWidth: true
                Layout.margins: 24
                Layout.bottomMargin: 0
                spacing: 8
                RowLayout {
                    Layout.fillWidth: true
                    Label { text: wizard.tr("Step %1 of %2", wizard.step, wizard.lastStep); color: window.muted; Layout.fillWidth: true }
                    Label { text: wizard.groupName(); color: window.muted }
                }
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 4
                    Repeater {
                        model: wizard.lastStep
                        Rectangle { required property int index; Layout.fillWidth: true; Layout.preferredHeight: 4; radius: 2; color: index < wizard.step ? palette.highlight : window.line }
                    }
                }
            }

            ScrollView {
                Layout.fillWidth: true
                Layout.fillHeight: true
                contentWidth: availableWidth
                clip: true

                ColumnLayout {
                    width: parent.width
                    spacing: 0

                    ColumnLayout {
                        visible: wizard.step === 0
                        Layout.fillWidth: true
                        Layout.margins: 28
                        Layout.topMargin: 16
                        spacing: 18
                        LanguageBox { Layout.alignment: Qt.AlignRight }
                        Rectangle {
                            Layout.alignment: Qt.AlignHCenter
                            Layout.preferredWidth: 96
                            Layout.preferredHeight: 96
                            radius: 24
                            color: Qt.alpha(palette.highlight, 0.16)
                            Image { anchors.centerIn: parent; source: window.iconBase + "drivesync.svg"; sourceSize.width: 64; sourceSize.height: 64 }
                        }
                        Label { text: wizard.tr("Let's connect your Google Drive"); font.pointSize: Qt.application.font.pointSize * 1.75; font.weight: Font.DemiBold; horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap; Layout.fillWidth: true }
                        Label { text: wizard.tr("drivesync doesn't borrow a key from anyone. You'll make your own free Google key, so only you can reach your files. Each step opens the right page and tells you exactly what to click."); color: window.muted; horizontalAlignment: Text.AlignHCenter; wrapMode: Text.Wrap; lineHeight: 1.2; Layout.fillWidth: true }
                        Flow {
                            Layout.alignment: Qt.AlignHCenter
                            Layout.maximumWidth: 440
                            spacing: 8
                            Repeater {
                                model: [wizard.tr("About 5 minutes"), wizard.tr("Only once"), wizard.tr("Free, no card needed")]
                                Rectangle {
                                    id: chip
                                    required property string modelData
                                    width: chipLabel.implicitWidth + 24
                                    height: 30
                                    radius: 15
                                    color: window.surface
                                    border.color: window.line
                                    Label { id: chipLabel; anchors.centerIn: parent; text: chip.modelData; font.pointSize: Qt.application.font.pointSize * 0.9 }
                                }
                            }
                        }
                        Card {
                            Layout.fillWidth: true
                            implicitHeight: planColumn.implicitHeight + 28
                            ColumnLayout {
                                id: planColumn
                                anchors.fill: parent
                                anchors.margins: 14
                                spacing: 8
                                SectionLabel { text: wizard.tr("What we'll do") }
                                Label { text: "<b>1–5</b>&nbsp;&nbsp;" + wizard.tr("Create your key on Google's website"); textFormat: Text.StyledText; wrapMode: Text.Wrap; Layout.fillWidth: true }
                                Label { text: "<b>6</b>&nbsp;&nbsp;" + wizard.tr("Paste it here"); textFormat: Text.StyledText; Layout.fillWidth: true }
                                Label { text: "<b>7</b>&nbsp;&nbsp;" + wizard.tr("Sign in and choose your folders"); textFormat: Text.StyledText; Layout.fillWidth: true }
                            }
                        }
                    }

                    ColumnLayout {
                        visible: wizard.step >= 1 && wizard.step <= 5
                        Layout.fillWidth: true
                        Layout.margins: 28
                        Layout.topMargin: 22
                        spacing: 16
                        Label { text: wizard.pages[wizard.step].title || ""; font.pointSize: Qt.application.font.pointSize * 1.6; font.weight: Font.DemiBold; wrapMode: Text.Wrap; Layout.fillWidth: true }
                        Repeater {
                            model: wizard.step >= 1 && wizard.step <= 5 ? wizard.pages[wizard.step].lines : []
                            delegate: Instruction {
                                required property string modelData
                                required property int index
                                number: index + 1
                                text: modelData
                            }
                        }
                        Button {
                            Layout.fillWidth: true
                            Layout.preferredHeight: 46
                            highlighted: true
                            icon.name: "internet-web-browser"
                            text: wizard.tr("Open Google's page")
                            onClicked: Qt.openUrlExternally(wizard.pages[wizard.step].link)
                        }
                        Hint { visible: text !== ""; text: wizard.pages[wizard.step].hint || "" }
                    }

                    ColumnLayout {
                        visible: wizard.step === 6
                        Layout.fillWidth: true
                        Layout.margins: 28
                        Layout.topMargin: 22
                        spacing: 10
                        Label { text: wizard.tr("Paste your key"); font.pointSize: Qt.application.font.pointSize * 1.6; font.weight: Font.DemiBold }
                        Label { text: wizard.tr("Google shows a window called <b>OAuth client created</b>. Copy each value with the copy icon next to it, then press Paste here."); textFormat: Text.StyledText; color: window.muted; wrapMode: Text.Wrap; Layout.fillWidth: true }
                        Label { text: wizard.tr("Client ID"); font.weight: Font.DemiBold; Layout.topMargin: 8 }
                        RowLayout {
                            Layout.fillWidth: true
                            TextField { id: clientId; Layout.fillWidth: true; font.family: "monospace" }
                            Button { text: wizard.tr("Paste"); onClicked: { clientId.clear(); clientId.paste() } }
                        }
                        Label { visible: wizard.idLooksRight; text: "✓ " + wizard.tr("Looks right"); color: window.good }
                        Label { visible: text !== ""; text: wizard.idHint(); textFormat: Text.StyledText; color: window.warn; wrapMode: Text.Wrap; Layout.fillWidth: true }
                        Label { text: wizard.tr("Client secret"); font.weight: Font.DemiBold; Layout.topMargin: 8 }
                        RowLayout {
                            Layout.fillWidth: true
                            TextField { id: clientSecret; Layout.fillWidth: true; font.family: "monospace"; echoMode: showSecret.checked ? TextInput.Normal : TextInput.Password }
                            Button { text: wizard.tr("Paste"); onClicked: { clientSecret.clear(); clientSecret.paste() } }
                        }
                        Label { visible: wizard.secretLooksRight; text: "✓ " + wizard.tr("Looks right"); color: window.good }
                        Label { visible: text !== ""; text: wizard.secretHint(); textFormat: Text.StyledText; color: window.warn; wrapMode: Text.Wrap; Layout.fillWidth: true }
                        CheckBox { id: showSecret; text: wizard.tr("Show secret") }
                        Hint { text: wizard.tr("These are stored in your system's password storage, never in a file. Closed Google's window already? Open it again from <a href='https://console.cloud.google.com/auth/clients'>your clients list</a>.") }
                    }

                    ColumnLayout {
                        visible: wizard.step === 7
                        Layout.fillWidth: true
                        Layout.margins: 28
                        Layout.topMargin: 22
                        spacing: 16
                        RowLayout {
                            spacing: 16
                            BusyIndicator { running: backend.signingIn; visible: running; Layout.preferredWidth: 44; Layout.preferredHeight: 44 }
                            ColumnLayout {
                                spacing: 2
                                Label { text: wizard.tr("Finish in your browser"); font.pointSize: Qt.application.font.pointSize * 1.6; font.weight: Font.DemiBold }
                                Label { visible: backend.signingIn; text: wizard.tr("Waiting for you…"); color: window.muted }
                            }
                        }
                        Instruction { number: 1; text: wizard.tr("Choose your Google account.") }
                        Instruction { number: 2; text: wizard.tr("Google will say <b>\"Google hasn't verified this app\"</b>. That's expected: the app is the one you just made, and only you use it.") + "<br>" + wizard.tr("Click <b>Continue</b>. Older screens call it Advanced → Go to drivesync.") }
                        Instruction { number: 3; text: wizard.tr("Tick <b>See, edit, create and delete your Google Drive files</b> and click <b>Continue</b>. This window moves on by itself.") }
                        Hint { visible: wizard.signInFailed; text: wizard.tr("Sign-in didn't finish. Check that you clicked Allow, then try again.") }
                        Button {
                            id: details
                            visible: backend.signInLog !== ""
                            flat: true
                            checkable: true
                            text: checked ? wizard.tr("Hide details") : wizard.tr("Show details")
                        }
                        TextArea { visible: details.checked; text: backend.signInLog; readOnly: true; wrapMode: Text.WrapAnywhere; selectByMouse: true; Layout.fillWidth: true }
                    }
                }
            }

            Rectangle { Layout.fillWidth: true; Layout.preferredHeight: 1; color: window.line }

            RowLayout {
                Layout.fillWidth: true
                Layout.margins: 18
                spacing: 10
                Button {
                    text: wizard.step === 0 ? wizard.tr("Later") : wizard.step === wizard.lastStep ? wizard.tr("Back") : wizard.tr("Back")
                    flat: wizard.step === 0
                    onClicked: {
                        if (wizard.step === 0) {
                            wizard.close()
                        } else {
                            if (wizard.step === wizard.lastStep)
                                backend.cancelSignIn()
                            wizard.attempted = false
                            wizard.step -= 1
                        }
                    }
                }
                Item { Layout.fillWidth: true }
                Button {
                    visible: wizard.step === wizard.lastStep && wizard.loginUrl !== "" && backend.signingIn
                    flat: true
                    text: wizard.tr("Browser didn't open? Open it again")
                    onClicked: Qt.openUrlExternally(wizard.loginUrl)
                }
                Button {
                    visible: wizard.signInFailed
                    highlighted: true
                    text: wizard.tr("Try again")
                    onClicked: wizard.startSignIn()
                }
                Button {
                    visible: wizard.step < wizard.lastStep
                    highlighted: true
                    text: wizard.step === 0 ? wizard.tr("Get started") : wizard.tr("Next")
                    enabled: wizard.step !== 6 || (wizard.idLooksRight && wizard.secretLooksRight)
                    onClicked: wizard.step += 1
                }
            }
        }
    }
}
