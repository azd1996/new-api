package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// ClientDisconnectRule scopes the "abort upstream on client disconnect" behavior
// to a single api-key (token name) combined with a client-requested model. Both
// fields are required by product rule: a rule with an empty token name or model
// never matches.
type ClientDisconnectRule struct {
	TokenName string `json:"token_name"`
	Model     string `json:"model"`
}

// ClientDisconnectSetting holds the api-key + model rules that opt a request pair
// into aborting the upstream stream when the client disconnects. The DEFAULT
// (empty rule list) keeps the upstream stream alive after a client disconnect so
// the final usage frame is still received and billed accurately; a matching rule
// switches that single pair to abort the upstream immediately instead.
type ClientDisconnectSetting struct {
	Rules []ClientDisconnectRule `json:"rules"`
}

var clientDisconnectSetting = ClientDisconnectSetting{
	Rules: []ClientDisconnectRule{},
}

func init() {
	config.GlobalConfig.Register("client_disconnect_setting", &clientDisconnectSetting)
}

func GetClientDisconnectSetting() *ClientDisconnectSetting {
	return &clientDisconnectSetting
}

// AbortUpstreamOnClientDisconnect reports whether the given api-key (token name)
// and client-requested model are explicitly configured to abort the upstream
// stream when the client disconnects. A rule matches only when both its token
// name and model equal the request's values, so a request without a token name
// or model can never match. Returns false (the default: keep the upstream alive
// to bill the final usage) when no rule is configured.
func AbortUpstreamOnClientDisconnect(tokenName, model string) bool {
	if tokenName == "" || model == "" {
		return false
	}
	for _, rule := range clientDisconnectSetting.Rules {
		if rule.TokenName == "" || rule.Model == "" {
			continue
		}
		if rule.TokenName == tokenName && rule.Model == model {
			return true
		}
	}
	return false
}
