import NetworkExtension

// PacketTunnelProvider is the privileged macOS integration point for PulseRoute.
// It is compiled inside an Xcode Network Extension target after entitlement approval.
final class PacketTunnelProvider: NEPacketTunnelProvider {
    private var agentURL: URL?

    // startTunnel applies managed gateway settings after validating the deployed provider configuration.
    override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
          guard let tunnelProtocol = protocolConfiguration as? NETunnelProviderProtocol,
              let configuration = TunnelConfiguration(protocolConfiguration: tunnelProtocol) else {
            completionHandler(TunnelConfigurationError.invalidProviderConfiguration)
            return
        }
        agentURL = configuration.agentURL
        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: configuration.remoteAddress)
        settings.ipv4Settings = NEIPv4Settings(addresses: [configuration.ipv4Address], subnetMasks: [configuration.ipv4SubnetMask])
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
        guard let agentURL else {
            throw TunnelConfigurationError.agentUnavailable
        }
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

private struct TunnelConfiguration {
    let remoteAddress: String
    let ipv4Address: String
    let ipv4SubnetMask: String
    let agentURL: URL

    init?(protocolConfiguration: NETunnelProviderProtocol) {
        guard let configuration = protocolConfiguration.providerConfiguration,
              let remoteAddress = configuration["remoteAddress"] as? String,
              let ipv4Address = configuration["ipv4Address"] as? String,
              let ipv4SubnetMask = configuration["ipv4SubnetMask"] as? String,
              let agentURLString = configuration["agentURL"] as? String,
              let agentURL = URL(string: agentURLString),
              agentURL.scheme == "https" || agentURL.host == "127.0.0.1" else {
            return nil
        }
        self.remoteAddress = remoteAddress
        self.ipv4Address = ipv4Address
        self.ipv4SubnetMask = ipv4SubnetMask
        self.agentURL = agentURL
    }
}

private enum TunnelConfigurationError: LocalizedError {
    case invalidProviderConfiguration
    case agentUnavailable

    var errorDescription: String? {
        switch self {
        case .invalidProviderConfiguration:
            return "Packet Tunnel configuration is missing required managed values."
        case .agentUnavailable:
            return "The local PulseRoute decision agent is unavailable."
        }
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