package auth

import "strings"

// What each role may reach on the control API (#206, #203), by method and
// route template, such as "GET /api/v1/conversations/{id}/messages".
// Visitors chat; Members use Toskar for themselves: their own chats,
// memories, files, and automations, with the shared models, profiles,
// knowledge, and tools to read. Everything else, such as models, tools,
// knowledge sources, computers, keys, people, and settings, is for Admins,
// and erasing everything is the Owner's alone. A route not listed here
// needs an Admin, so a new one is closed until it is given a role.

// visitorRoutes are what everyone signed in may reach.
var visitorRoutes = map[string]bool{
	"GET /api/v1/remote-access/route":                     true,
	"GET /api/v1/health":                                  true,
	"GET /api/v1/version":                                 true,
	"GET /api/v1/me":                                      true,
	"PATCH /api/v1/me/preferences":                        true,
	"GET /api/v1/me/devices":                              true,
	"DELETE /api/v1/me/devices/{id}":                      true,
	"POST /api/v1/devices/pairing":                        true,
	"GET /api/v1/devices/pairing":                         true,
	"DELETE /api/v1/devices/pairing":                      true,
	"GET /api/v1/personalization":                         true,
	"PUT /api/v1/personalization":                         true,
	"POST /api/v1/session":                                true,
	"DELETE /api/v1/session":                              true,
	"GET /api/v1/invites/{token}":                         true,
	"POST /api/v1/invites/{token}":                        true,
	"GET /api/v1/oidc":                                    true,
	"GET /api/v1/portals/{slug}/page":                     true,
	"POST /api/v1/portals/{slug}/enter":                   true,
	"GET /api/v1/oidc/start":                              true,
	"GET /api/v1/oidc/callback":                           true,
	"GET /api/v1/settings":                                true,
	"GET /api/v1/tls":                                     true,
	"GET /api/v1/events":                                  true,
	"GET /api/v1/profiles":                                true,
	"GET /api/v1/profiles/{id}":                           true,
	"GET /api/v1/models":                                  true,
	"GET /api/v1/models/running":                          true,
	"GET /api/v1/nodes":                                   true,
	"POST /api/v1/chat":                                   true,
	"POST /api/v1/models/warm":                            true,
	"POST /api/v1/chat/stop":                              true,
	"POST /api/v1/tools/decide":                           true,
	"GET /api/v1/conversations":                           true,
	"POST /api/v1/conversations":                          true,
	"PATCH /api/v1/conversations/{id}":                    true,
	"DELETE /api/v1/conversations/{id}":                   true,
	"POST /api/v1/conversations/delete":                   true,
	"GET /api/v1/conversations/{id}/messages":             true,
	"PUT /api/v1/conversations/{id}/messages/{mid}/shown": true,
	"GET /api/v1/conversations/{id}/artifacts":            true,
	"POST /api/v1/artifacts":                              true,
	"GET /api/v1/artifacts/{id}":                          true,
	"GET /api/v1/artifacts/{id}/content":                  true,
	"DELETE /api/v1/artifacts/{id}":                       true,
	"POST /api/v1/speech":                                 true,
	"GET /api/v1/tools":                                   true,
	"GET /api/v1/training/deployed":                       true,
}

// memberRoutes are, besides the visitors', what Members may reach.
var memberRoutes = map[string]bool{
	"GET /api/v1/memory":         true,
	"POST /api/v1/memory":        true,
	"PATCH /api/v1/memory/{id}":  true,
	"DELETE /api/v1/memory/{id}": true,

	"GET /api/v1/automations":                          true,
	"POST /api/v1/automations":                         true,
	"POST /api/v1/automations/preview":                 true,
	"POST /api/v1/automations/parse":                   true,
	"GET /api/v1/automations/{id}":                     true,
	"PATCH /api/v1/automations/{id}":                   true,
	"DELETE /api/v1/automations/{id}":                  true,
	"POST /api/v1/automations/{id}/run":                true,
	"POST /api/v1/automations/{id}/pause":              true,
	"POST /api/v1/automations/{id}/resume":             true,
	"GET /api/v1/automations/{id}/runs":                true,
	"POST /api/v1/automations/{id}/runs/{run_id}/chat": true,
	"POST /api/v1/automations/{id}/hook":               true,

	"GET /api/v1/notifications":               true,
	"GET /api/v1/notifications/{id}":          true,
	"POST /api/v1/notifications/read":         true,
	"POST /api/v1/notifications/{id}/dismiss": true,

	"GET /api/v1/knowledge/sources":              true,
	"GET /api/v1/knowledge/sources/{id}":         true,
	"GET /api/v1/knowledge/sources/{id}/content": true,
	"POST /api/v1/knowledge/search":              true,

	"GET /api/v1/tools/{id}":      true,
	"GET /api/v1/tools/providers": true,
	"GET /api/v1/capabilities":    true,
	"GET /api/v1/hardware":        true,
}

// ownerRoutes are the Owner's alone.
var ownerRoutes = map[string]bool{
	"POST /api/v1/settings/reset": true,
}

// RoleNeeded is the least role that may make a request, by its method and
// route template.
func RoleNeeded(method, route string) Role {
	key := strings.ToUpper(method) + " " + route
	switch {
	case visitorRoutes[key]:
		return RoleVisitor
	case memberRoutes[key]:
		return RoleMember
	case ownerRoutes[key]:
		return RoleOwner
	}
	return RoleAdmin
}

// RoleRoutes lists the routes given a role below Admin or the Owner's, for
// a test that each is served.
func RoleRoutes() []string {
	var out []string
	for _, m := range []map[string]bool{visitorRoutes, memberRoutes, ownerRoutes} {
		for k := range m {
			out = append(out, k)
		}
	}
	return out
}
