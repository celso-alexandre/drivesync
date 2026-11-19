#include "drivesync-gui/cpp/run.h"

#include <QtCore/QStorageInfo>
#include <QtCore/QDir>
#include <QtGui/QIcon>
#include <QtQml/QQmlApplicationEngine>
#include <QtNetwork/QLocalServer>
#include <QtNetwork/QLocalSocket>
#include <QtWidgets/QApplication>

#if defined(Q_OS_LINUX)
#include <QtDBus/QDBusConnection>
#include <QtDBus/QDBusInterface>
#include <QtDBus/QDBusReply>
#endif

::std::int32_t run_app(bool start_hidden)
{
    static int argc = 1;
    static char name[] = "drivesync-gui";
    static char *argv[] = {name, nullptr};
    QApplication app(argc, argv);
    QApplication::setApplicationName(QStringLiteral("drivesync"));
    QApplication::setOrganizationName(QStringLiteral("drivesync"));
    QApplication::setOrganizationDomain(QStringLiteral("celso-alexandre.github.io"));
    QApplication::setApplicationDisplayName(QStringLiteral("drivesync"));
    QApplication::setDesktopFileName(QStringLiteral("io.github.celso_alexandre.drivesync"));
    QApplication::setWindowIcon(QIcon(QStringLiteral(":/qt/qml/drivesync/icons/drivesync.svg")));
    QApplication::setQuitOnLastWindowClosed(false);

    QString serverName = QStringLiteral("io.github.celso_alexandre.drivesync");
    const QByteArray flatpakId = qgetenv("FLATPAK_ID");
    const QByteArray runtimeDir = qgetenv("XDG_RUNTIME_DIR");
    if (!flatpakId.isEmpty() && !runtimeDir.isEmpty()) {
        const QString shared = QString::fromLocal8Bit(runtimeDir) + QStringLiteral("/app/") + QString::fromLocal8Bit(flatpakId);
        const QString socketPath = shared + QStringLiteral("/drivesync-gui.sock");
        if (socketPath.toLocal8Bit().size() < 100 && QDir().mkpath(shared)) {
            serverName = socketPath;
        }
    }
    {
        QLocalSocket probe;
        probe.connectToServer(serverName);
        if (probe.waitForConnected(300)) {
            probe.write(start_hidden ? "hidden\n" : "show\n");
            probe.waitForBytesWritten(300);
            return 0;
        }
    }
    QLocalServer::removeServer(serverName);
    QLocalServer server;
    server.setSocketOptions(QLocalServer::UserAccessOption);
    if (!server.listen(serverName)) {
        qCritical("drivesync: cannot start the single-instance socket %s: %s", qPrintable(serverName), qPrintable(server.errorString()));
        return 1;
    }

    QQmlApplicationEngine engine;
    engine.setInitialProperties({{QStringLiteral("startHidden"), start_hidden}});
    engine.load(QUrl(QStringLiteral("qrc:/qt/qml/drivesync/qml/Main.qml")));
    if (engine.rootObjects().isEmpty()) {
        qCritical("drivesync: the window could not be loaded");
        return 1;
    }
    QObject *root = engine.rootObjects().constFirst();
    QObject::connect(&server, &QLocalServer::newConnection, root, [&server, root]() {
        while (QLocalSocket *client = server.nextPendingConnection()) {
            QObject::connect(client, &QLocalSocket::readyRead, root, [client, root]() {
                if (client->readAll().startsWith("show")) {
                    QMetaObject::invokeMethod(root, "showWindow");
                }
                client->disconnectFromServer();
            });
            QObject::connect(client, &QLocalSocket::disconnected, client, &QObject::deleteLater);
        }
    });
    return app.exec();
}

::std::int64_t free_bytes(::rust::Str path)
{
    QStorageInfo storage(QString::fromUtf8(path.data(), static_cast<qsizetype>(path.size())));
    if (!storage.isValid() || !storage.isReady()) {
        return -1;
    }
    return storage.bytesAvailable();
}

bool request_background(bool autostart)
{
#if defined(Q_OS_LINUX)
    QDBusInterface portal(QStringLiteral("org.freedesktop.portal.Desktop"),
                          QStringLiteral("/org/freedesktop/portal/desktop"),
                          QStringLiteral("org.freedesktop.portal.Background"),
                          QDBusConnection::sessionBus());
    if (!portal.isValid()) {
        return false;
    }
    QVariantMap options;
    options.insert(QStringLiteral("reason"), QStringLiteral("Keep your Google Drive folders in sync while you are signed in"));
    options.insert(QStringLiteral("autostart"), autostart);
    options.insert(QStringLiteral("commandline"), QStringList{QStringLiteral("drivesync-gui"), QStringLiteral("--hidden")});
    options.insert(QStringLiteral("dbus-activatable"), false);
    QDBusReply<QDBusObjectPath> reply = portal.call(QStringLiteral("RequestBackground"), QString(), options);
    return reply.isValid();
#else
    (void)autostart;
    return false;
#endif
}
