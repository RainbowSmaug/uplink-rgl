// Uplink game launcher overlay — Quickshell / Wayland layer shell
//
// Full-screen overlay: compositor (Niri) provides backdrop blur for the
// transparent top/bottom regions.
//
// To enable blur in Niri, add to ~/.config/niri/config.kdl:
//   window-rule {
//       match app-id="^quickshell$" at-startup=true
//       opacity 1.0
//   }
// Niri blurs layer-shell surfaces that request blur via the kde-blur protocol.
// Quickshell requests this automatically when the window background is transparent.

import QtQuick
import QtQuick.Controls
import QtMultimedia
import Quickshell
import Quickshell.Io
import Quickshell.Wayland

ShellRoot {
    PanelWindow {
        id: win

        screen: Quickshell.screens.find(s => s.name === "DP-1") ?? Quickshell.screens[0]


        // Full-screen overlay — the transparent top/bottom areas let compositor
        // blur show through. Set color transparent so Quickshell requests blur.
        color: "transparent"

        anchors {
            left:   true
            right:  true
            top:    true
            bottom: true
        }

        WlrLayershell.layer:         WlrLayer.Overlay
        WlrLayershell.keyboardFocus: WlrKeyboardFocus.Exclusive
        WlrLayershell.namespace:     "uplink"
        exclusiveZone: 0

        // ─── State ────────────────────────────────────────────────────────────

        property var    allGames:    []
        property int    focusIndex:  0
        property bool   configured:  false
        property bool   launching:   false
        property string launchName:  ""
        property string searchText:  ""
        property bool   searchActive: false

        readonly property real stripHeight: height * 0.6
        readonly property real stripY:      height * 0.2

        // ─── Model ────────────────────────────────────────────────────────────

        ListModel { id: gameModel }

        // Reusable styled text field for the setup form.
        component SetupField: TextField {
            width: parent.width
            background: Rectangle { color: "#151520"; border.color: "#2a2a3a"; radius: 6 }
            color: "#e0e0e0"; font.pixelSize: 14; padding: 10
        }

        // Virtual multiplier for infinite scroll — user starts in the middle
        // and would have to scroll ~500 games in either direction to "run out"
        readonly property int virtualMult: 101

        function rebuildModel(games) {
            gameModel.clear()
            for (var i = 0; i < games.length; i++) {
                gameModel.append(games[i])
            }
            // Jump to the middle of the virtual list without animation
            var mid = Math.floor(virtualMult / 2) * gameModel.count
            carousel.positionViewAtIndex(mid, ListView.Center)
            focusIndex = mid
            carousel.currentIndex = mid
        }

        // Search navigates to the first match without rebuilding the model,
        // so the full carousel stays intact with no repeated cards.
        function navigateToSearch(query) {
            if (query === "" || gameModel.count === 0) return
            var q = query.toLowerCase()
            for (var i = 0; i < gameModel.count; i++) {
                if (gameModel.get(i).name.toLowerCase().indexOf(q) !== -1) {
                    var target = Math.floor(virtualMult / 2) * gameModel.count + i
                    carousel.currentIndex = target
                    win.focusIndex = target
                    return
                }
            }
        }

        function launchCurrent() {
            if (gameModel.count === 0) return
            var game = gameModel.get(win.focusIndex % gameModel.count)
            win.launching = true
            win.launchName = game.name
            launchProc.command = ["uplink-client", "launch", game.name]
            launchProc.running = true
        }

        // ─── Processes ────────────────────────────────────────────────────────

        // Phase 1: fast game list (no covers) — shows carousel immediately
        Process {
            id: gamesProc
            command: ["uplink-client", "games"]
            running: true
            stdout: StdioCollector {
                id: gamesColl
                onStreamFinished: {
                    try {
                        var raw = JSON.parse(gamesColl.text)
                        if (!Array.isArray(raw) && raw.error === "not_configured") {
                            win.configured = false
                            return
                        }
                        win.configured = true
                        win.allGames = raw
                        win.rebuildModel(raw)
                        coversProc.running = true
                    } catch(e) {
                        console.log("uplink: games parse error:", e, gamesColl.text)
                    }
                }
            }
        }

        // Phase 2: cover art — runs after carousel is visible, updates in place
        Process {
            id: coversProc
            command: ["uplink-client", "games-covers"]
            running: false
            stdout: StdioCollector {
                id: coversColl
                onStreamFinished: {
                    try {
                        var coverData = JSON.parse(coversColl.text)
                        if (!Array.isArray(coverData)) return
                        var coverMap = {}
                        for (var i = 0; i < coverData.length; i++) {
                            if (coverData[i].coverPath)
                                coverMap[coverData[i].uuid] = coverData[i].coverPath
                        }
                        for (var j = 0; j < gameModel.count; j++) {
                            var uuid = gameModel.get(j).uuid
                            if (coverMap[uuid] !== undefined)
                                gameModel.setProperty(j, "coverPath", coverMap[uuid])
                        }
                    } catch(e) {
                        console.log("uplink: covers parse error:", e)
                    }
                }
            }
        }

        SoundEffect {
            id: navClick
            source: Qt.resolvedUrl("nav_click.wav")
            volume: 0.4
        }

        Process {
            id: launchProc
            running: false
            onRunningChanged: {
                if (!running && win.launching) {
                    Qt.quit()
                }
            }
        }

        Process {
            id: configProc
            running: false
            onExited: (code, status) => {
                if (code === 0) {
                    setupError.text = ""
                    gamesProc.running = false
                    gamesProc.running = true
                } else {
                    setupError.text = "Connection failed — check host and credentials"
                }
            }
        }

        // ─── Layout ───────────────────────────────────────────────────────────

        // Cinematic bars — semi-transparent dark overlay on top/bottom 20%
        Rectangle {
            anchors { left: parent.left; right: parent.right; top: parent.top }
            height: win.stripY
            gradient: Gradient {
                GradientStop { position: 0.0; color: Qt.rgba(0, 0, 0, 0.80) }
                GradientStop { position: 1.0; color: Qt.rgba(0, 0, 0, 0.0)  }
            }
        }

        Rectangle {
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            height: win.stripY
            gradient: Gradient {
                GradientStop { position: 0.0; color: Qt.rgba(0, 0, 0, 0.0)  }
                GradientStop { position: 1.0; color: Qt.rgba(0, 0, 0, 0.80) }
            }
        }

        // Strip layout container
        Item {
            id: strip
            anchors { left: parent.left; right: parent.right }
            y: win.stripY
            height: win.stripHeight


            // ── Setup overlay ─────────────────────────────────────────────────

            Column {
                id: setupOverlay
                visible: !win.configured && !win.launching
                anchors.centerIn: parent
                spacing: 14
                width: 340

                Text {
                    text: "Uplink — Connect to Apollo"
                    color: "#e0e0e0"
                    font.pixelSize: 20
                    font.weight: Font.DemiBold
                    anchors.horizontalCenter: parent.horizontalCenter
                }

                Text {
                    text: "Enter your gaming PC Apollo credentials"
                    color: "#888"
                    font.pixelSize: 13
                    anchors.horizontalCenter: parent.horizontalCenter
                }

                SetupField {
                    id: hostField
                    placeholderText: "Host (e.g. 192.168.1.9)"
                }

                SetupField {
                    id: userField
                    placeholderText: "Username"
                }

                SetupField {
                    id: passField
                    placeholderText: "Password"
                    echoMode: TextInput.Password
                    Keys.onReturnPressed: submitSetup()
                }

                Text {
                    id: setupError
                    text: ""
                    color: "#f87171"
                    font.pixelSize: 12
                    visible: text !== ""
                    anchors.horizontalCenter: parent.horizontalCenter
                    wrapMode: Text.WordWrap
                    width: parent.width
                }

                Rectangle {
                    width: parent.width; height: 40
                    color: connectArea.pressed ? "#1d4ed8" : "#2563eb"
                    radius: 6
                    Behavior on color { ColorAnimation { duration: 80 } }

                    Text {
                        anchors.centerIn: parent
                        text: "Connect"
                        color: "white"
                        font.pixelSize: 14
                        font.weight: Font.Medium
                    }
                    MouseArea {
                        id: connectArea
                        anchors.fill: parent
                        onClicked: submitSetup()
                    }
                }
            }

            function submitSetup() {
                setupError.text = ""
                configProc.command = ["uplink-client", "configure",
                                      hostField.text, userField.text, passField.text]
                configProc.running = true
            }

            // ── Launching overlay ─────────────────────────────────────────────

            Text {
                visible: win.launching
                anchors.centerIn: parent
                text: "Launching " + win.launchName + "…"
                color: "#e0e0e0"
                font.pixelSize: 22
                font.weight: Font.Medium
            }

            // ── Carousel ──────────────────────────────────────────────────────

            Item {
                id: carouselArea
                visible: win.configured && !win.launching
                anchors {
                    top: parent.top
                    left: parent.left
                    right: parent.right
                    bottom: searchBar.top
                    bottomMargin: 36
                }

                ListView {
                    id: carousel
                    anchors.fill: parent
                    // Virtual model: 101× real count so scrolling feels infinite.
                    // rebuildModel() positions currentIndex at the midpoint on load.
                    model: gameModel.count * win.virtualMult
                    orientation: ListView.Horizontal
                    onCurrentIndexChanged: win.focusIndex = currentIndex

                    // Keep focused card centred
                    highlightRangeMode: ListView.StrictlyEnforceRange
                    preferredHighlightBegin: (width - focusedWidth) / 2
                    preferredHighlightEnd:   (width + focusedWidth) / 2
                    snapMode: ListView.SnapToItem

                    // Actual widths per distance — no Scale transforms, so spacing:0
                    // gives a true gapless flow (shear edges align mathematically)
                    readonly property int focusedWidth: Math.round(height * 2 / 3)
                    readonly property int sideWidth:    Math.round(height * 2 / 3 * 0.58)
                    readonly property int farWidth:     Math.round(height * 2 / 3 * 0.40)
                    spacing: 0

                    highlightMoveDuration: 280
                    cacheBuffer: carousel.farWidth * 3

                    clip: false

                    delegate: GameCard {
                        readonly property int myDist: Math.abs(index - ListView.view.currentIndex)
                        readonly property var gameData: gameModel.get(index % gameModel.count)
                        uuid:      gameData ? gameData.uuid      : ""
                        name:      gameData ? gameData.name      : ""
                        coverPath: gameData ? (gameData.coverPath ?? "") : ""
                        launcher:  gameData ? (gameData.launcher  ?? "") : ""

                        width: myDist === 0 ? carousel.focusedWidth
                             : myDist === 1 ? carousel.sideWidth
                             : carousel.farWidth
                        height: carousel.height

                        onLaunchRequested: win.launchCurrent()
                        onFocusRequested: (idx) => {
                            win.focusIndex = idx
                            carousel.currentIndex = idx
                        }
                    }

                    WheelHandler {
                        onWheel: (event) => {
                            if (event.angleDelta.y > 0) {
                                carousel.decrementCurrentIndex()
                            } else {
                                carousel.incrementCurrentIndex()
                            }
                            navClick.play()
                        }
                    }
                }
            }

            // ── Edge vignettes ────────────────────────────────────────────────

            Rectangle {
                z: 10
                anchors { left: parent.left; top: parent.top; bottom: carouselArea.bottom }
                width: 160
                gradient: Gradient {
                    orientation: Gradient.Horizontal
                    GradientStop { position: 0.0; color: Qt.rgba(0, 0, 0, 0.85) }
                    GradientStop { position: 1.0; color: Qt.rgba(0, 0, 0, 0.0)  }
                }
            }

            Rectangle {
                z: 10
                anchors { right: parent.right; top: parent.top; bottom: carouselArea.bottom }
                width: 160
                gradient: Gradient {
                    orientation: Gradient.Horizontal
                    GradientStop { position: 0.0; color: Qt.rgba(0, 0, 0, 0.0)  }
                    GradientStop { position: 1.0; color: Qt.rgba(0, 0, 0, 0.85) }
                }
            }

            // ── Search bar ────────────────────────────────────────────────────

            TextField {
                id: searchBar
                visible: win.configured && !win.launching
                anchors {
                    bottom: parent.bottom
                    horizontalCenter: parent.horizontalCenter
                    bottomMargin: 24
                }
                width: 380
                height: 36
                placeholderText: win.searchActive ? "" : "Search  (press f)"
                readOnly: !win.searchActive
                text: win.searchText

                background: Rectangle {
                    color: win.searchActive ? "#10101e" : "#0d0d18"
                    border.color: win.searchActive ? "#4fbaff" : "#1e1e2e"
                    border.width: win.searchActive ? 2 : 1
                    radius: 5
                    Behavior on border.color { ColorAnimation { duration: 120 } }
                }

                color: "#d0d0e0"
                font.pixelSize: 13

                onTextEdited: {
                    if (win.searchActive) {
                        win.searchText = text
                        win.navigateToSearch(text)
                    }
                }

                Keys.onReturnPressed: win.launchCurrent()
                Keys.onEscapePressed: {
                    win.searchActive = false
                    win.searchText = ""
                    searchBar.text = ""
                    keyHandler.forceActiveFocus()
                }
            }

        }

        // ─── Keyboard ─────────────────────────────────────────────────────────

        // PanelWindow doesn't support `focus`; use a transparent overlay Item instead.
        Item {
            id: keyHandler
            anchors.fill: parent
            focus: true

            Keys.onPressed: (event) => {
                if (win.searchActive) {
                    // Escape is handled directly on searchBar; other keys fall through to it
                    return
                }

                switch (event.key) {
                case Qt.Key_H:
                case Qt.Key_Left:
                case Qt.Key_J:
                    carousel.decrementCurrentIndex()
                    navClick.play()
                    event.accepted = true
                    break
                case Qt.Key_L:
                case Qt.Key_Right:
                case Qt.Key_K:
                    carousel.incrementCurrentIndex()
                    navClick.play()
                    event.accepted = true
                    break
                case Qt.Key_Return:
                case Qt.Key_Space:
                    win.launchCurrent()
                    event.accepted = true
                    break
                case Qt.Key_F:
                    win.searchActive = true
                    searchBar.forceActiveFocus()
                    event.accepted = true
                    break
                case Qt.Key_Escape:
                    Qt.quit()
                    event.accepted = true
                    break
                }
            }
        }
    }
}
