package connectivity

// Owner indicates which side maintains upstream CLOB WebSockets.
type Owner string

const (
	OwnerNone   Owner = "none"
	OwnerClient Owner = "client"
	OwnerServer Owner = "server"
)

// ChannelDisplay is the dashboard pill state for one upstream channel.
type ChannelDisplay string

const (
	DisplayConnected    ChannelDisplay = "connected"
	DisplayConnecting   ChannelDisplay = "connecting"
	DisplayDisconnected ChannelDisplay = "disconnected"
	DisplayStandby      ChannelDisplay = "standby"
	DisplayUnconfigured ChannelDisplay = "unconfigured"
)

// ChannelState is one upstream (USER or OB) connection view.
type ChannelState struct {
	Connected  bool
	Connecting bool
	Display    ChannelDisplay
	Required   bool
}
