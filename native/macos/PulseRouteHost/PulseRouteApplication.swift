import AppKit
import NetworkExtension

// PulseRouteApplication configures and starts the signed Packet Tunnel extension.
@main
final class PulseRouteApplication: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
    }

    func applicationWillTerminate(_ notification: Notification) {
        // The tunnel persists according to the user's Network Extension preference.
    }
}