import QtQuick
import QtQuick.Effects

// Parallelogram game card delegate.
// Properties are set explicitly by the delegate (not via model role context),
// which lets the carousel use a virtual repeating model for infinite scroll.
Item {
    id: card

    // Set by the delegate in shell.qml
    property string uuid:      ""
    property string name:      ""
    property string coverPath: ""  // local file path from Go cover cache
    property string launcher:  ""  // "steam", "epic", etc.

    // Distance from the focused card (0 = focused, 1 = adjacent, 2+ = far)
    readonly property int dist: Math.abs(index - ListView.view.currentIndex)

    signal launchRequested()
    signal focusRequested(int idx)

    opacity: 1.0
    z: -dist  // focused card renders on top

    // Width is driven from the delegate binding; animate it smoothly on focus change
    Behavior on width { NumberAnimation { duration: 220; easing.type: Easing.OutCubic } }

    // Focused card pops out ~8%. Scale is baked into the shear matrix so both
    // happen in one transform — no Qt scale/Matrix4x4 interaction artifacts.
    // The card scales around its visual center (cx - 0.18*cy, cy) which stays fixed.
    property real cardScale: dist === 0 ? 1.08 : 1.0
    Behavior on cardScale { NumberAnimation { duration: 220; easing.type: Easing.OutCubic } }

    transform: Matrix4x4 {
        property real s:  card.cardScale
        property real cx: card.width  / 2
        property real cy: card.height / 2
        matrix: Qt.matrix4x4(
            s,  -0.18*s,  0,  (1-s)*(cx - 0.18*cy),
            0,   s,       0,  (1-s)*cy,
            0,   0,       1,  0,
            0,   0,       0,  1
        )
    }

    // Solid base — no radius, fully fills card so rounded Image corners don't bleed
    Rectangle {
        anchors.fill: parent
        color: "#13131e"
    }

    // Cover art
    Image {
        id: coverImage
        anchors.fill: parent
        // Prefer locally cached file (written by uplink-client games).
        // Fall back to Steam CDN if Go download failed, else nothing.
        source: coverPath !== "" ? ("file://" + coverPath) : ""
        fillMode: Image.PreserveAspectCrop
        asynchronous: true
        cache: true
        layer.enabled: true
        layer.effect: MultiEffect {
            maskEnabled: true
            maskSource: ShaderEffectSource {
                sourceItem: Rectangle { width: coverImage.width; height: coverImage.height; radius: 6; color: "white" }
            }
        }

        // Fallback when no cover or load error
        Rectangle {
            anchors.fill: parent
            visible: parent.status !== Image.Ready
            color: "#13131e"
            radius: 6
            gradient: Gradient {
                GradientStop { position: 0.0; color: "#1a1a2e" }
                GradientStop { position: 1.0; color: "#0d0d16" }
            }

            Text {
                anchors.centerIn: parent
                text: name
                color: "#c8c8d8"
                font.pixelSize: 14
                font.weight: Font.Medium
                width: parent.width - 24
                wrapMode: Text.WordWrap
                horizontalAlignment: Text.AlignHCenter
            }
        }
    }

    // Launcher watermark — bottom-right corner, ~35% opacity
    Image {
        visible: launcher !== ""
        source: launcher !== "" ? Qt.resolvedUrl("launcher_" + launcher + ".png") : ""
        width: 36; height: 36
        anchors { right: parent.right; top: parent.top; margins: 10 }
        opacity: 0.35
        smooth: true
        z: 2
    }

    MouseArea {
        anchors.fill: parent

        onClicked: {
            if (dist === 0) {
                card.launchRequested()
            } else {
                card.focusRequested(index)
            }
        }
    }
}
