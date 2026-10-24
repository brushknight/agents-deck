package link

import (
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// limit is one link.options entry: a string's buffer size (nanopb max_size,
// which counts the terminating NUL) and/or a list's max_count.
type limit struct{ size, count int }

// limits mirrors proto/agentsdeck/link/v1/link.options (TestLimitsMatchOptions
// keeps them in step): the deck clips what it sends to these, so firmware
// with fixed buffers never gets more than it can hold.
var limits = map[string]limit{
	"Hello.board_id": {size: 40}, "Hello.type": {size: 32}, "Hello.firmware": {size: 24}, "Hello.inputs": {count: 4},
	"Welcome.server_name": {size: 48}, "Welcome.server_version": {size: 24}, "Welcome.device_name": {size: 40}, "Welcome.cert_serial": {size: 40},
	"Server.name": {size: 48}, "Server.version": {size: 24},
	"AgentRemoved.id": {size: 12},
	"Agent.id":        {size: 12}, "Agent.title": {size: 64}, "Agent.ai_title": {size: 96}, "Agent.cwd": {size: 128},
	"Agent.folder": {size: 48}, "Agent.branch": {size: 48}, "Agent.model": {size: 40}, "Agent.model_label": {size: 24},
	"Agent.effort": {size: 12}, "Agent.app": {size: 12}, "Agent.last_prompt": {size: 160}, "Agent.error": {size: 128}, "Agent.subagents": {count: 6},
	"Activity.tool": {size: 24}, "Activity.detail": {size: 128},
	"Prompt.id": {size: 16}, "Prompt.title": {size: 96}, "Prompt.detail": {size: 512}, "Prompt.context": {size: 96}, "Prompt.options": {count: 6},
	"Option.key": {size: 8}, "Option.label": {size: 96},
	"Hat.shape": {size: 16}, "Hat.color": {size: 12},
	"Account.name": {size: 16}, "Account.color": {size: 12},
	"Subagent.id": {size: 12}, "Subagent.title": {size: 64}, "Subagent.type": {size: 24}, "Subagent.tool": {size: 24},
	"Answer.agent_id": {size: 12}, "Answer.prompt_id": {size: 16}, "Answer.key": {size: 8},
	"Focus.agent_id": {size: 12}, "Interrupt.agent_id": {size: 12}, "Dismiss.agent_id": {size: 12}, "Resume.agent_id": {size: 12},
	"Move.agent_id": {size: 12}, "Order.agent_ids": {size: 12, count: 32},
	"SetHat.agent_id": {size: 12}, "SetHat.folder": {size: 128}, "SetHat.shape": {size: 16}, "SetHat.color": {size: 12},
	"AddLive.session_id": {size: 40},
	"Handoff.agent_id":   {size: 12}, "Handoff.account": {size: 16},
	"Result.error":   {size: 96},
	"HatList.shapes": {size: 16, count: 16}, "HatList.colors": {size: 12, count: 12}, "HatList.overrides": {count: 8},
	"HatOverride.folder": {size: 128}, "HatOverride.shape": {size: 16}, "HatOverride.color": {size: 12},
	"LiveSessions.sessions":  {count: 8},
	"LiveSession.session_id": {size: 40}, "LiveSession.title": {size: 64}, "LiveSession.cwd": {size: 128}, "LiveSession.folder": {size: 48},
	"AccountList.accounts": {count: 4},
	"AccountInfo.name":     {size: 16}, "AccountInfo.color": {size: 12},
	"RestoreResult.results": {count: 16},
	"Restored.id":           {size: 12}, "Restored.title": {size: 64}, "Restored.action": {size: 12}, "Restored.error": {size: 96},
	// Pairing and Wi-Fi. For bytes, size is the byte count (no NUL): the deck
	// refuses to send more rather than clip a certificate.
	"CertRequest.csr": {size: 600},
	"CertIssued.cert": {size: 800}, "CertIssued.ca": {size: 600}, "CertIssued.hosts": {size: 64, count: 4},
	"CertIssued.server_fingerprint": {size: 32}, "CertIssued.device_name": {size: 40}, "CertIssued.cert_serial": {size: 40},
	"WifiConfig.ssid": {size: 33}, "WifiConfig.password": {size: 65},
	"NetStatus.ssid": {size: 33}, "NetStatus.ip": {size: 40},
}

// clipMessage cuts every string and list in m (recursively) to its limit.
func clipMessage(m protoreflect.Message) {
	name := string(m.Descriptor().Name())
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		lim := limits[name+"."+string(fd.Name())]
		switch {
		case fd.IsList():
			l := m.Mutable(fd).List()
			if lim.count > 0 && l.Len() > lim.count {
				l.Truncate(lim.count)
			}
			for i := range l.Len() {
				switch {
				case fd.Kind() == protoreflect.StringKind && lim.size > 0:
					l.Set(i, protoreflect.ValueOfString(clipString(l.Get(i).String(), lim.size)))
				case fd.Kind() == protoreflect.MessageKind:
					clipMessage(l.Get(i).Message())
				}
			}
		case fd.Kind() == protoreflect.MessageKind:
			clipMessage(m.Mutable(fd).Message())
		case fd.Kind() == protoreflect.StringKind && lim.size > 0:
			if s := v.String(); len(s) > lim.size-1 {
				m.Set(fd, protoreflect.ValueOfString(clipString(s, lim.size)))
			}
		}
		return true
	})
}

// clipString fits s in a buffer of size bytes (NUL included), cutting on a
// rune boundary and ending in "…".
func clipString(s string, size int) string {
	max := size - 1
	if len(s) <= max {
		return s
	}
	const ell = "…"
	cut := max - len(ell)
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " ") + ell
}
