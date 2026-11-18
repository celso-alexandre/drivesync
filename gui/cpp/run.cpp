#include "drivesync-gui/cpp/run.h"

#include <QtCore/QLockFile>
#include <QtCore/QStandardPaths>
#include <QtCore/QDir>
#include <QtGui/QIcon>
#include <QtQml/QQmlApplicationEngine>
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
    QApplication::setApplicationDisplayName(QStringLiteral("drivesync"));
    QApplication::setDesktopFileName(QStringLiteral("io.github.celso_alexandre.drivesync"));
    QApplication::setWindowIcon(QIcon(QStringLiteral(":/qt/qml/drivesync/icons/drivesync.svg")));
    QApplication::setQuitOnLastWindowClosed(false);

    QString runtime = QStandardPaths::writableLocation(QStandardPaths::RuntimeLocation);
    if (runtime.isEmpty()) {
        runtime = QDir::tempPath();
    }
    QLockFile lock(runtime + QStringLiteral("/drivesync-gui.lock"));
    lock.setStaleLockTime(0);
    if (!lock.tryLock(100)) {
        return 0;
    }

    QQmlApplicationEngine engine;
    engine.setInitialProperties({{QStringLiteral("startHidden"), start_hidden}});
    engine.load(QUrl(QStringLiteral("qrc:/qt/qml/drivesync/qml/Main.qml")));
    if (engine.rootObjects().isEmpty()) {
        return 1;
    }
    return app.exec();
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
