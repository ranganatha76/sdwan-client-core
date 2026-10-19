import NetworkExtension

// PacketTunnelProvider is the privileged macOS integration point for PulseRoute.
// It is compiled inside an Xcode Network Extension target after entitlement approval.
final class PacketTunnelProvider: NEPacketTunnelProvider {
    private let agentURL = URL(string: "http://127.0.0.1:8082/v1/flows/evaluate")!

    // startTunnel applies the supplied tunnel settings; gateway details are provisioned outside source control.
    override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: "managed-by-device-policy")
        settings.ipv4Settings = NEIPv4Settings(addresses: ["10.255.0.2"], subnetMasks: ["255.255.255.255"])
        setTunnelNetworkSettings(settings, completionHandler: completionHandler)
    }

    // stopTunnel removes the Packet Tunnel settings when the extension is disabled.
    override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
        // NetworkExtension removes tunnel settings after this method returns.
        completionHandler()
    }

    // handleAppMessage lets the signed host app submit hostname metadata for policy evaluation.
    override func handleAppMessage(_ messageData: Data, completionHandler: ((Data?) -> Void)? = nil) {
        guard let request = try? JSONDecoder().decode(FlowRequest.self, from: messageData), !request.hostname.isEmpty else {
            completionHandler?(nil)
            return
        }
        Task {
            let response = try? await evaluate(request)
            completionHandler?(response.flatMap { try? JSONEncoder().encode($0) })
        }
    }

    // evaluate forwards normalized flow metadata to the local Go data-plane agent.
    private func evaluate(_ flow: FlowRequest) async throws -> FlowDecision {
        var request = URLRequest(url: agentURL)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(flow)
        let (data, response) = try await URLSession.shared.data(for: request)
        guard (response as? HTTPURLResponse)?.statusCode == 200 else {
            throw URLError(.cannotConnectToHost)
        }
        return try JSONDecoder().decode(FlowDecision.self, from: data)
    }
}

private struct FlowRequest: Codable {
    let hostname: String
}

private struct FlowDecision: Codable {
    let action: String
    let priority: String
    let matchedRule: String?
    let policyVersion: String
    let mode: String
}