package chat

import "encoding/json"

type accountStatus int

const (
	accountUnknown accountStatus = iota
	accountReady
	accountLoggedOut
)

// codexAccountStatus is the single interpretation of account/read used by
// message delivery and session status. A provider can explicitly require no
// OpenAI account. Missing/malformed answers and RPC failures prove no login
// state; they must not invent an authentication failure.
func codexAccountStatus(v codexLine) accountStatus {
	if v.Method != "" || v.ID == nil || *v.ID != codexAccountID ||
		(len(v.Error) > 0 && string(v.Error) != "null") {
		return accountUnknown
	}
	var r struct {
		Account            json.RawMessage `json:"account"`
		RequiresOpenaiAuth *bool           `json:"requiresOpenaiAuth"`
	}
	if json.Unmarshal(v.Result, &r) != nil {
		return accountUnknown
	}
	if r.RequiresOpenaiAuth != nil && !*r.RequiresOpenaiAuth {
		return accountReady
	}
	if len(r.Account) == 0 {
		return accountUnknown
	}
	if string(r.Account) == "null" {
		return accountLoggedOut
	}
	var account map[string]json.RawMessage
	if json.Unmarshal(r.Account, &account) != nil || account == nil {
		return accountUnknown
	}
	return accountReady
}
