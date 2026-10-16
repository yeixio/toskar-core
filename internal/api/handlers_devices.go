package api

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
)

// Connect a device (#216). The computer's own UI makes a 6-digit code; the
// phone sends it from the local network, without a key, and gets one.

func (s *Server) deviceRoutes(api *mux.Router) {
	api.HandleFunc("/devices/pairing", s.handleStartDevicePairing).Methods(http.MethodPost)
	api.HandleFunc("/devices/pairing", s.handleDevicePairingStatus).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/devices/pairing", s.handleCancelDevicePairing).Methods(http.MethodDelete)
	api.HandleFunc(pairDeviceRoute, s.handlePairDevice).Methods(http.MethodPost)
	api.HandleFunc("/me/devices", s.handleMyDevices).Methods(http.MethodGet)
	api.HandleFunc("/me/devices", s.handleAddCompanion).Methods(http.MethodPost)
	api.HandleFunc("/me/devices/{id}", s.handleDisconnectMyDevice).Methods(http.MethodDelete)
}

// pairDeviceRoute is the one control API route that needs no key.
const pairDeviceRoute = "/devices/pair"

// remoteIP is the address a request came from, without its port.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// pairingView is a code being shown, with where a phone reaches this
// computer.
type pairingView struct {
	auth.DevicePairing
	// Address is this computer's address on the local network, such as
	// 192.168.1.20:7331.
	Address string `json:"address,omitempty"`
	// Reachable is false while the API answers only on this computer: a
	// phone can't connect until local network access is on.
	Reachable bool `json:"reachable"`
	// TLSShort is the start of the API certificate's fingerprint, which
	// the phone shows too, so the person can see it reached this computer
	// (#213).
	TLSShort string `json:"tls_short,omitempty"`
}

func (s *Server) pairingView(p auth.DevicePairing) pairingView {
	v := pairingView{DevicePairing: p, TLSShort: s.TLS().Short}
	if s.deps.PhoneAddress != nil {
		v.Address, v.Reachable = s.deps.PhoneAddress()
	}
	return v
}

// handleStartDevicePairing shows a new code, replacing any other. With
// enable_lan it first turns on local network access, which a phone needs.
func (s *Server) handleStartDevicePairing(w http.ResponseWriter, r *http.Request) {
	if s.deps.Devices == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Connecting a device isn't available.", nil)
		return
	}
	var body struct {
		EnableLAN bool `json:"enable_lan"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body)
	// Turning on network access is an Admin's (#203); anyone may show a
	// code for their own device.
	if body.EnableLAN && !auth.PrincipalFrom(r.Context()).Person.Role.AtLeast(auth.RoleAdmin) {
		writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "turning on local network access needs the admin role", map[string]any{"role": string(auth.RoleAdmin)})
		return
	}
	if body.EnableLAN && s.deps.EnableLANForPhone != nil {
		if err := s.deps.EnableLANForPhone(r.Context()); err != nil {
			writeErrFrom(w, http.StatusInternalServerError, "LAN_ACCESS_FAILED", err)
			return
		}
	}
	p, err := s.deps.Devices.StartFor(auth.PrincipalFrom(r.Context()).Person)
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "PAIRING_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, s.pairingView(p))
}

// handleDevicePairingStatus says whether a phone has connected with the code.
func (s *Server) handleDevicePairingStatus(w http.ResponseWriter, r *http.Request) {
	if s.deps.Devices == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Connecting a device isn't available.", nil)
		return
	}
	writeJSON(w, http.StatusOK, s.pairingView(s.deps.Devices.StatusFor(auth.PersonID(r.Context()))))
}

// handleCancelDevicePairing stops showing the code.
func (s *Server) handleCancelDevicePairing(w http.ResponseWriter, r *http.Request) {
	if s.deps.Devices != nil {
		s.deps.Devices.CancelFor(auth.PersonID(r.Context()))
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePairDevice exchanges a code for the phone's own key. It needs no
// key, so it answers only on the local network.
func (s *Server) handlePairDevice(w http.ResponseWriter, r *http.Request) {
	if s.deps.Devices == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Connecting a device isn't available.", nil)
		return
	}
	if !auth.FromLocalNetwork(r) {
		writeErrFrom(w, http.StatusForbidden, "PAIRING_NOT_LOCAL", auth.ErrNotLocalNetwork)
		return
	}
	var body struct {
		Code       string `json:"code"`
		DeviceName string `json:"device_name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	rec, secret, err := s.deps.Devices.Pair(r.Context(), body.Code, body.DeviceName, remoteIP(r))
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, auth.ErrDeviceThrottled):
			status = http.StatusTooManyRequests
		case errors.Is(err, auth.ErrWrongDeviceCode):
			status = http.StatusUnauthorized
		case errors.Is(err, auth.ErrNoDeviceCode), errors.Is(err, auth.ErrDeviceCodeExpired):
			status = http.StatusGone
		}
		writeErrFrom(w, status, "PAIRING_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, s.pairedAnswer(rec, secret))
}

// pairedAnswer is what a newly connected device keeps: its key, the
// certificate's fingerprint to check from now on (#213; over plain HTTP the
// phone checks it on its next HTTPS connection), and the route secret for
// finding this computer away from home (#456).
func (s *Server) pairedAnswer(rec auth.APIKeyRecord, secret string) map[string]any {
	tlsInfo := s.TLS()
	out := map[string]any{"api_key": secret, "key": rec,
		"tls_fingerprint": tlsInfo.Fingerprint, "tls_short": tlsInfo.Short}
	if s.deps.RouteSecret != nil {
		if route, id, err := s.deps.RouteSecret(); err == nil {
			out["route_secret"], out["route_id"] = route, id
			if s.deps.RelayName != nil {
				out["relay"] = s.deps.RelayName()
			}
			s.addRelayToken(out)
		}
	}
	return out
}

// maxDevices is how many connected devices one person may have; a
// companion past it is refused, so a key can't make keys without end.
const maxDevices = 20

// handleAddCompanion connects a device that goes along with one already
// connected, such as a watch with its phone (yeixio/toskar-apps#213): it
// gets a key of its own, for the same person, so it shows in Devices and
// can be disconnected on its own. A connected device asks for it, from
// anywhere, so there's no code to type on a watch.
func (s *Server) handleAddCompanion(w http.ResponseWriter, r *http.Request) {
	if s.deps.Devices == nil || s.deps.Devices.CreateKey == nil || s.deps.ListAPIKeys == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Connecting a device isn't available.", nil)
		return
	}
	var body struct {
		DeviceName string `json:"device_name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	list, err := s.myDevices(r)
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "DEVICES_FAILED", err)
		return
	}
	live := 0
	for _, k := range list {
		if !k.Revoked {
			live++
		}
	}
	if live >= maxDevices {
		writeErr(w, http.StatusConflict, "DEVICE_LIMIT", "You have as many connected devices as you can. Disconnect one first.",
			map[string]any{"limit": maxDevices})
		return
	}
	rec, secret, err := s.deps.Devices.CreateKey(r.Context(), auth.CleanDeviceName(body.DeviceName))
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "PAIRING_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, s.pairedAnswer(rec, secret))
}

// myDevices are the request's person's connected devices (#206).
func (s *Server) myDevices(r *http.Request) ([]auth.APIKeyRecord, error) {
	all, err := s.deps.ListAPIKeys(r.Context())
	if err != nil {
		return nil, err
	}
	me := auth.PersonID(r.Context())
	out := []auth.APIKeyRecord{}
	for _, k := range all {
		if k.Kind == auth.KindDevice && k.PersonID == me {
			out = append(out, k)
		}
	}
	return out, nil
}

// handleMyDevices lists the devices connected as the request's person,
// whatever their role.
func (s *Server) handleMyDevices(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListAPIKeys == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Devices aren't available.", nil)
		return
	}
	list, err := s.myDevices(r)
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "DEVICES_FAILED", err)
		return
	}
	// Each says whether it takes pushes from the Toskar app (#460).
	type device struct {
		auth.APIKeyRecord
		Push bool `json:"push"`
	}
	pushed := map[string]bool{}
	if s.push != nil {
		if regs, err := s.push.ForPerson(r.Context(), auth.PersonID(r.Context())); err == nil {
			for _, reg := range regs {
				pushed[reg.KeyID] = true
			}
		}
	}
	out := make([]device, 0, len(list))
	for _, k := range list {
		out = append(out, device{APIKeyRecord: k, Push: pushed[k.ID]})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDisconnectMyDevice revokes one of the person's own devices' keys.
func (s *Server) handleDisconnectMyDevice(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListAPIKeys == nil || s.deps.RevokeAPIKey == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Devices aren't available.", nil)
		return
	}
	list, err := s.myDevices(r)
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "DEVICES_FAILED", err)
		return
	}
	id := mux.Vars(r)["id"]
	for _, k := range list {
		if k.ID == id {
			if err := s.deps.RevokeAPIKey(r.Context(), id); err != nil {
				writeErrFrom(w, http.StatusInternalServerError, "DEVICES_FAILED", err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeErr(w, http.StatusNotFound, "DEVICE_NOT_FOUND", "No device of yours has that id.", nil)
}
